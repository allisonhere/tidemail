package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
)

func TestSaveAttachMissingDownloadsUsesDefaultDestination(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	t.Cleanup(m.CloseSessions)
	m.focused = paneContent
	m.contentMessageID = 1
	m.contentAttachments = []db.Attachment{{Filename: "report.txt", Data: []byte("report")}}

	next, cmd := m.handleMainKey(tea.KeyMsg{Type: tea.KeyCtrlD})
	m = next.(Model)
	if m.overlay == overlaySaveAttach || cmd == nil {
		t.Fatalf("missing Downloads should start fallback save, overlay=%v cmd=%v", m.overlay, cmd != nil)
	}
	result := cmd().(AttachmentsSavedMsg)
	if result.Err != nil || result.Count != 1 {
		t.Fatalf("fallback save: %+v", result)
	}
	if data, err := os.ReadFile(filepath.Join(result.Path, "report.txt")); err != nil || string(data) != "report" {
		t.Fatalf("saved attachment = %q, %v", data, err)
	}
}

func TestSaveAttachPickerFailedNavigationRetainsListingAndInvalidCursorDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	m := Model{keys: DefaultKeys}
	if err := m.openSaveAttachPicker(dir); err != nil {
		t.Fatal(err)
	}
	m.overlay = overlaySaveAttach
	before := m.saveAttachPicker.currentDir
	if err := m.openSaveAttachPicker(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("expected unreadable directory error")
	}
	if m.saveAttachPicker.currentDir != before || len(m.saveAttachPicker.entries) == 0 {
		t.Fatal("failed navigation changed the working picker")
	}
	m.saveAttachPicker.entries = nil
	next, cmd := m.handleSaveAttachPicker(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd != nil || m.overlay != overlayNone || m.saveAttachPicker.active {
		t.Fatalf("invalid picker should close safely, overlay=%v active=%v", m.overlay, m.saveAttachPicker.active)
	}
	m.saveAttachPickerEnterDir()
}

func TestSaveAttachPickerStartsInConfiguredDownloadDir(t *testing.T) {
	chosen := filepath.Join(t.TempDir(), "mail-in")
	if err := os.MkdirAll(chosen, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Display.DownloadDir = chosen
	m := NewModel(nil, cfg, "dev", false)
	t.Cleanup(m.CloseSessions)
	m.focused = paneContent
	m.contentMessageID = 1
	m.contentAttachments = []db.Attachment{{Filename: "report.txt", Data: []byte("report")}}

	next, _ := m.handleMainKey(tea.KeyMsg{Type: tea.KeyCtrlD})
	m = next.(Model)
	if m.overlay != overlaySaveAttach || m.saveAttachPicker.currentDir != chosen {
		t.Fatalf("picker should open in %q, overlay=%v dir=%q", chosen, m.overlay, m.saveAttachPicker.currentDir)
	}
}

func TestSaveAttachFallbackFollowsConfiguredDownloadDir(t *testing.T) {
	chosen := filepath.Join(t.TempDir(), "not-created-yet")
	cfg := config.DefaultConfig()
	cfg.Display.DownloadDir = chosen
	m := NewModel(nil, cfg, "dev", false)
	t.Cleanup(m.CloseSessions)
	m.focused = paneContent
	m.contentMessageID = 1
	m.contentAttachments = []db.Attachment{{Filename: "report.txt", Data: []byte("report")}}

	// The folder does not exist, so the picker cannot open and the fixed
	// fallback saves under the configured folder instead of ~/Downloads.
	next, cmd := m.handleMainKey(tea.KeyMsg{Type: tea.KeyCtrlD})
	m = next.(Model)
	if m.overlay == overlaySaveAttach || cmd == nil {
		t.Fatalf("a missing folder should start the fallback save, overlay=%v cmd=%v", m.overlay, cmd != nil)
	}
	result := cmd().(AttachmentsSavedMsg)
	if result.Err != nil || result.Path != filepath.Join(chosen, "tidemail-attachments") {
		t.Fatalf("fallback save: %+v, want it under %q", result, chosen)
	}
}

func TestSaveAttachDefaultsToXDGDownloadDir(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DOWNLOAD_DIR", xdg)
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	t.Cleanup(m.CloseSessions)
	if got := m.downloadDir(); got != xdg {
		t.Fatalf("downloadDir = %q, want %q", got, xdg)
	}
}
