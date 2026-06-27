package ui

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/allisonhere/tide/internal/update"
	tea "github.com/charmbracelet/bubbletea"
)

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

func (m *Model) applyManualUpdatePreview() {
	pub := time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC)
	now := time.Now()
	m.currentVersion = "v0.0.38"
	m.updateState = updateStateNeedsElevation
	m.updateInfo = update.ReleaseInfo{
		Version:     "v0.0.39",
		PublishedAt: pub,
		Summary:     "## Tide v0.0.39",
	}
	m.updateInfoFresh = true
	m.updateErr = ""
	m.updateDismissed = false
	m.pendingUpdateInstall = false
	m.downloadedUpdate = nil
	m.updateInstall = update.InstallResult{
		RequiresManual: true,
		ManualCommand:  update.SuggestedManualInstallScript,
	}
	m.cfg.Updates.LastCheckedUnix = now.Unix()
	m.settings = newSettings(m.cfg, m.settingsUpdateState())
	m.settings.setFocusedPane(settingsPaneDetail)
	m.settings.setActiveSection(ssUpdates)
	m.settings.setFocusedField(sfUpdateManualCommand)
	m.overlay = overlaySettings
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
	m.saveConfig()
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
