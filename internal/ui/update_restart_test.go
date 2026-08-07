package ui

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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
