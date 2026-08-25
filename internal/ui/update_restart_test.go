package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/update"
)

// Restarting after an in-app update must NOT spawn a second process from inside
// the live TUI (that leaves two processes fighting over the terminal). Instead
// the model records the binary to exec and quits, so main can hand the restored
// terminal to the new version. This test pins that contract.
func TestRestartAfterUpdateRecordsExecPathAndQuits(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "dev", false)
	m.updateInstall = update.InstallResult{
		Version:        "v9.9.9",
		ExecutablePath: "/tmp/tide-new",
		Restartable:    true,
	}
	// Queue the action the Updates settings row sets when the user confirms restart.
	m.settings.action = settingsActionRestartAfterUpdate

	next, cmd := m.handleSettings(tea.WindowSizeMsg{Width: 80, Height: 24})
	nm := next.(Model)

	if got := nm.RestartExecPath(); got != "/tmp/tide-new" {
		t.Fatalf("expected restart exec path recorded, got %q", got)
	}
	if nm.QuitActivated() {
		t.Fatal("update restart must not be reported as a normal quit")
	}
	if cmd == nil {
		t.Fatal("expected a quit command, got nil")
	}
	if msg := cmd(); fmt.Sprintf("%T", msg) != "tea.QuitMsg" {
		t.Fatalf("expected tea.QuitMsg from restart, got %T (%v)", msg, msg)
	}
}

// A non-restartable install must not record an exec path or quit.
func TestRestartAfterUpdateNoopWhenNotRestartable(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "dev", false)
	m.updateInstall = update.InstallResult{Version: "v9.9.9", Restartable: false}
	m.settings.action = settingsActionRestartAfterUpdate

	next, _ := m.handleSettings(tea.WindowSizeMsg{Width: 80, Height: 24})
	if got := next.(Model).RestartExecPath(); got != "" {
		t.Fatalf("expected no restart exec path when not restartable, got %q", got)
	}
}

func TestUpdateConfirmStartsProgressOverlay(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "v1.2.2", false)
	m.overlay = overlayUpdateConfirm
	m.updateState = updateStateAvailable
	m.updateInfo = update.ReleaseInfo{
		Version:     "v1.2.3",
		AssetName:   "tidemail-linux-x86_64",
		DownloadURL: "https://github.com/o/r/releases/download/archive.tar.gz",
	}

	next, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(Model)
	if got.overlay != overlayUpdateConfirm {
		t.Fatalf("overlay = %v, want update confirm overlay to stay open", got.overlay)
	}
	if got.updateState != updateStateDownloading {
		t.Fatalf("updateState = %v, want downloading", got.updateState)
	}
	if got.updateProgress != 0 {
		t.Fatalf("updateProgress = %d, want 0", got.updateProgress)
	}
	if cmd == nil {
		t.Fatal("expected batched download and progress commands")
	}
}

func TestUpdateOverlayIgnoresKeysWhileProgressIsRunning(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "v1.2.2", false)
	m.overlay = overlayUpdateConfirm
	m.updateState = updateStateInstalling
	m.updateProgress = 40

	next, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	got := next.(Model)
	if got.overlay != overlayUpdateConfirm || got.updateState != updateStateInstalling || got.updateProgress != 40 {
		t.Fatalf("progress state changed: overlay=%v state=%v progress=%d", got.overlay, got.updateState, got.updateProgress)
	}
	if cmd != nil {
		t.Fatal("expected no command while update progress is running")
	}
}

func TestPreviewUpdateProgressOverlayCanClose(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "v1.2.2", true)
	m.ApplyUpdateProgressPreview()

	next, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	got := next.(Model)
	if got.overlay != overlayNone {
		t.Fatalf("overlay = %v, want closed preview overlay", got.overlay)
	}
	if got.updateState != updateStateIdle {
		t.Fatalf("updateState = %v, want idle after closing preview", got.updateState)
	}
	if cmd != nil {
		t.Fatal("expected no command when closing preview overlay")
	}
}

func TestUpdateInstalledWaitsForProgressBeforeRestartPrompt(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "v1.2.2", false)
	m.overlay = overlayUpdateConfirm
	m.updateState = updateStateInstalling
	m.updateProgress = 40

	next, cmd := m.Update(UpdateInstalledMsg{Result: update.InstallResult{
		Version:        "v1.2.3",
		ExecutablePath: "/tmp/tidemail",
		Restartable:    true,
	}})
	got := next.(Model)
	if got.updateState != updateStateInstalling || !got.updateInstallReady {
		t.Fatalf("expected install result held while progress catches up, state=%v ready=%v", got.updateState, got.updateInstallReady)
	}
	if got.RestartExecPath() != "" {
		t.Fatalf("restart path set before progress finished: %q", got.RestartExecPath())
	}
	if cmd != nil {
		t.Fatal("expected no command until progress tick catches up")
	}

	for got.updateProgress < 100 {
		next, _ = got.Update(UpdateProgressTickMsg{})
		got = next.(Model)
	}
	if got.updateState != updateStateInstalled {
		t.Fatalf("updateState = %v, want installed", got.updateState)
	}
	if got.overlay != overlayUpdateConfirm {
		t.Fatalf("overlay = %v, want update overlay showing restart prompt", got.overlay)
	}
	if got.RestartExecPath() != "/tmp/tidemail" {
		t.Fatalf("RestartExecPath = %q, want /tmp/tidemail", got.RestartExecPath())
	}
}

func TestUpdateInstalledFailureClosesProgressOverlay(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "v1.2.2", false)
	m.overlay = overlayUpdateConfirm
	m.updateState = updateStateInstalling
	m.updateProgress = 40

	next, cmd := m.Update(UpdateInstalledMsg{Err: errors.New("disk full")})
	got := next.(Model)
	if got.overlay != overlayNone {
		t.Fatalf("overlay = %v, want closed on failure", got.overlay)
	}
	if got.updateState != updateStateError || !got.statusErr || !strings.Contains(got.statusMsg, "disk full") {
		t.Fatalf("failure not surfaced: state=%v status=%q err=%v", got.updateState, got.statusMsg, got.statusErr)
	}
	if cmd == nil {
		t.Fatal("expected status clear command")
	}
}

func TestUpdateOverlayRendersProgressBar(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "v1.2.2", true)
	m.updateState = updateStateInstalling
	m.updateInfo = update.ReleaseInfo{Version: "v1.2.3"}
	m.updateProgress = 40
	chrome := newManagerChrome(60, CatppuccinMocha, true)

	got := ansi.Strip(m.renderUpdateConfirmOverlay(60, chrome))
	if !strings.Contains(got, "Installing TideMail v1.2.3... 40%") {
		t.Fatalf("expected install progress label, got %q", got)
	}
	if !strings.Contains(got, "████") || !strings.Contains(got, "░░░░") {
		t.Fatalf("expected progress bar in overlay, got %q", got)
	}
}

func TestUpdateOverlayRendersShadowedBinaryCleanup(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "v1.2.2", false)
	m.updateState = updateStateInstalled
	m.updateInstall = update.InstallResult{
		Version:         "v1.2.3",
		ShadowedPath:    "/usr/local/bin/tidemail",
		ShadowedCommand: `rm -f "/usr/local/bin/tidemail"`,
	}
	chrome := newManagerChrome(72, CatppuccinMocha, true)

	got := ansi.Strip(m.renderUpdateConfirmOverlay(72, chrome))
	if !strings.Contains(got, "/usr/local/bin/tidemail is still earlier on PATH") {
		t.Fatalf("expected shadowed binary warning, got %q", got)
	}
	if !strings.Contains(got, `rm -f "/usr/local/bin/tidemail"`) {
		t.Fatalf("expected cleanup command, got %q", got)
	}
}

func TestApplyUpdateProgressPreviewOpensProgressOverlay(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", true)
	m.ApplyUpdateProgressPreview()

	if m.overlay != overlayUpdateConfirm {
		t.Fatalf("overlay = %v, want update overlay", m.overlay)
	}
	if m.updateState != updateStateInstalling {
		t.Fatalf("updateState = %v, want installing", m.updateState)
	}
	if m.updateProgress <= 0 || m.updateProgress >= 100 {
		t.Fatalf("updateProgress = %d, want an in-progress value", m.updateProgress)
	}
}
