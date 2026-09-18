package ui

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/allisonhere/tidemail/internal/update"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	updateProgressStep     = 5
	updateProgressInterval = 120 * time.Millisecond
)

func tickUpdateProgress() tea.Cmd {
	return tea.Tick(updateProgressInterval, func(time.Time) tea.Msg { return UpdateProgressTickMsg{} })
}

func (m *Model) maybeCheckForUpdatesCmd(manual bool) tea.Cmd {
	if manual {
		return m.checkForUpdatesCmd(true)
	}
	if !m.cfg.Updates.CheckOnStartup {
		return nil
	}
	// Always run the startup check when enabled: the banner is check-first and
	// does not trust cached results, so skipping here would leave the user
	// without any update signal for up to CheckIntervalHours. -allie
	return m.checkForUpdatesCmd(false)
}

func (m *Model) checkForUpdatesCmd(manual bool) tea.Cmd {
	m.updateState = updateStateChecking
	updater := m.updater
	currentVersion := m.currentVersion
	check := func() tea.Msg {
		result, err := updater.Check(currentVersion)
		return UpdateCheckedMsg{Result: result, Manual: manual, Err: err}
	}
	// Start the spinner animation for this update check (no-op if already running).
	return tea.Batch(check, m.ensureSpinner())
}

func (m *Model) downloadUpdateCmd(info update.ReleaseInfo) tea.Cmd {
	updater := m.updater
	return func() tea.Msg {
		asset, err := updater.Download(info)
		return UpdateDownloadedMsg{Asset: asset, Err: err}
	}
}

func (m *Model) installUpdateCmd(asset update.DownloadedAsset) tea.Cmd {
	updater := m.updater
	currentExec, _ := os.Executable()
	return func() tea.Msg {
		result, err := updater.Install(asset, currentExec)
		return UpdateInstalledMsg{Result: result, Err: err}
	}
}

func (m *Model) beginUpdateInstall() tea.Cmd {
	m.overlay = overlayUpdateConfirm
	m.updateState = updateStateDownloading
	m.updateErr = ""
	m.updateProgress = 0
	m.updateInstallReady = false
	m.updateInstallErr = nil
	m.updateInstall = update.InstallResult{}
	m.syncSettingsUpdateState()
	return tea.Batch(m.downloadUpdateCmd(m.updateInfo), tickUpdateProgress())
}

func (m Model) updateInProgress() bool {
	return m.updateState == updateStateDownloading || m.updateState == updateStateInstalling
}

func (m Model) finalizeUpdateInstall() (Model, tea.Cmd) {
	installErr := m.updateInstallErr
	m.updateInstallReady = false
	m.updateInstallErr = nil
	m.downloadedUpdate = nil

	if installErr != nil {
		m.updateState = updateStateError
		m.updateErr = installErr.Error()
		m.overlay = overlayNone
		m.syncSettingsUpdateState()
		m.setStatus("update failed: "+installErr.Error(), true)
		return m, m.clearStatusCmd()
	}
	if m.updateInstall.RequiresManual {
		m.updateState = updateStateNeedsElevation
		m.syncSettingsUpdateState()
		m.updateDismissed = false
		m.cfg.Updates.DismissedVersion = ""
		_ = m.saveConfig()
		return m, nil
	}
	m.updateState = updateStateInstalled
	m.updateDismissed = false
	m.cfg.Updates.DismissedVersion = ""
	m.clearCachedAvailableUpdate()
	_ = m.saveConfig()
	m.syncSettingsUpdateState()
	if m.updateInstall.Restartable && m.updateInstall.ExecutablePath != "" {
		m.restartExecPath = m.updateInstall.ExecutablePath
	}
	return m, nil
}

func (m Model) openBrowserCmd(url string) tea.Cmd {
	browser := m.cfg.Display.Browser
	return func() tea.Msg {
		var cmd *exec.Cmd
		if browser != "" {
			cmd = exec.Command(browser, url)
		} else {
			switch runtime.GOOS {
			case "darwin":
				cmd = exec.Command("open", url)
			default:
				cmd = exec.Command("xdg-open", url)
			}
		}
		_ = cmd.Start()
		return nil
	}
}

func (m Model) effectiveManualCommand() string {
	if s := strings.TrimSpace(m.updateInstall.ManualCommand); s != "" {
		return s
	}
	if m.updateDismissed {
		return ""
	}
	v := strings.TrimSpace(m.updateInfo.Version)
	if v == "" {
		return ""
	}
	if !update.IsNewerVersion(v, m.currentVersion) {
		return ""
	}
	ok, err := update.InstallDestinationWritable()
	if err != nil || ok {
		return ""
	}
	return update.ManualUpdateCommand()
}

// updateManualCommand is the command this install has to run outside TideMail.
// effectiveManualCommand already prefers whatever the installer reported when it
// could not write; the script is the last resort for an install TideMail cannot
// otherwise place.
func (m Model) updateManualCommand() string {
	if cmd := strings.TrimSpace(m.effectiveManualCommand()); cmd != "" {
		return cmd
	}
	return update.SuggestedManualInstallScript
}

func (m Model) settingsUpdateState() settingsUpdateState {
	lastChecked := time.Time{}
	if m.cfg.Updates.LastCheckedUnix > 0 {
		lastChecked = time.Unix(m.cfg.Updates.LastCheckedUnix, 0)
	}
	return settingsUpdateState{
		currentVersion:   m.currentVersion,
		state:            m.updateState,
		latestVersion:    m.updateInfo.Version,
		latestIsFresh:    m.updateInfoFresh,
		publishedAt:      m.updateInfo.PublishedAt,
		summary:          m.updateInfo.Summary,
		lastChecked:      lastChecked,
		err:              m.updateErr,
		dismissed:        m.updateDismissed,
		manualCommand:    m.effectiveManualCommand(),
		restartable:      m.updateInstall.Restartable,
		installedVersion: m.updateInstall.Version,
	}
}

func (m *Model) syncSettingsUpdateState() {
	m.settings.setUpdateState(m.settingsUpdateState())
}

// applyManualUpdatePreview stages the update a package-managed install cannot
// apply itself, and opens the window it gets instead. That window is otherwise
// only reachable on a machine where a distro package really does own the
// binary, which makes it the one piece of the update UI nobody can check while
// working on it. The state it leaves behind is the real thing, so closing the
// window and opening Settings > Updates previews that side too.
func (m *Model) applyManualUpdatePreview() {
	pub := time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC)
	now := time.Now()
	m.currentVersion = "v0.0.38"
	m.updateState = updateStateAvailable
	m.updateInfo = update.ReleaseInfo{
		Version:     "v0.0.39",
		PublishedAt: pub,
		Summary:     "Accounts keep their own settings and credentials.",
		AssetName:   "tidemail-linux-x86_64",
	}
	m.updateInfoFresh = true
	m.updateErr = ""
	m.updateDismissed = false
	m.pendingUpdateInstall = false
	m.downloadedUpdate = nil
	m.updateInstall = update.InstallResult{
		RequiresManual: true,
		ManualCommand:  "yay -S tidemail-bin",
	}
	// Name a package the preview machine almost certainly does not have, so the
	// wording an AUR user sees is what renders rather than the generic
	// "cannot write to its install location" fallback.
	m.previewPackageOwner = &update.PackageInstall{
		Manager: "pacman",
		Package: "tidemail-bin",
		Foreign: true,
	}
	m.cfg.Updates.LastCheckedUnix = now.Unix()
	m.settings = newSettings(m.cfg, m.settingsUpdateState())
	m.settings.setFocusedPane(settingsPaneDetail)
	m.settings.setActiveSection(ssUpdates)
	m.settings.setFocusedField(sfUpdateManualCommand)
	m.overlay = overlayUpdateConfirm
}

func (m *Model) ApplyUpdateProgressPreview() {
	m.currentVersion = "v0.0.38"
	m.updateState = updateStateInstalling
	m.updateInfo = update.ReleaseInfo{
		Version:   "v0.0.39",
		Summary:   "Preview update flow.",
		AssetName: "tidemail-linux-x86_64",
	}
	m.updateInfoFresh = true
	m.updateErr = ""
	m.updateDismissed = false
	m.pendingUpdateInstall = false
	m.downloadedUpdate = nil
	m.updateInstall = update.InstallResult{}
	m.updateProgress = 25
	m.updateInstallReady = false
	m.updateInstallErr = nil
	m.overlay = overlayUpdateConfirm
}

func (m *Model) dismissAvailableUpdate() tea.Cmd {
	if m.previewManualUpdateUI {
		m.setStatus("preview: dismiss ignored (not saved)", false)
		return m.clearStatusCmd()
	}
	if m.updateInfo.Version == "" {
		return nil
	}
	m.cfg.Updates.DismissedVersion = m.updateInfo.Version
	_ = m.saveConfig()
	m.updateDismissed = true
	m.syncSettingsUpdateState()
	m.setStatus("Tide update "+m.updateInfo.Version+" dismissed", false)
	return m.clearStatusCmd()
}

func (m *Model) restoreCachedUpdateState() {
	// Banner is check-first: we never surface an update banner from cached config
	// alone, only after a live GitHub check in this session. Clear any stale
	// cached "available_*" values so they cannot resurface. -allie
	m.clearCachedAvailableUpdate()
}

func (m *Model) clearCachedAvailableUpdate() {
	m.cfg.Updates.AvailableVersion = ""
	m.cfg.Updates.AvailableSummary = ""
	m.cfg.Updates.AvailablePublished = 0
}
