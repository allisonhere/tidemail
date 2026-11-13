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
