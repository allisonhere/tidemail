package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func logViewerModel(t *testing.T, entries []logEntry) Model {
	t.Helper()
	m, _, _ := newIdentityModel(t, acct("Personal", "imap.example.com", "me@example.com", "pw"))
	t.Setenv("HOME", t.TempDir())
	m.width, m.height = 100, 40
	m.logBuffer = entries
	m.overlay = overlayLogViewer
	return m
}

func TestLogBufferPlainTextIsChronologicalWithErrorMarks(t *testing.T) {
	t1 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	entries := []logEntry{
		{Time: t1, Message: "synced INBOX"},
		{Time: t1.Add(time.Minute), Message: "send failed", IsError: true},
	}
	text := logBufferPlainText(entries)
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %v", lines)
	}
	if !strings.Contains(lines[0], "synced INBOX") || strings.Contains(lines[0], "!") {
		t.Errorf("first line = %q", lines[0])
	}
	if !strings.Contains(lines[1], "send failed") || !strings.Contains(lines[1], "!") {
		t.Errorf("second line = %q", lines[1])
	}
	if lines[0] >= lines[1] {
		t.Errorf("expected chronological (oldest first) order: %v", lines)
	}
}

func TestLogBufferPlainTextEmpty(t *testing.T) {
	if got := logBufferPlainText(nil); !strings.Contains(got, "no log entries") {
		t.Errorf("empty buffer text = %q", got)
	}
}

func TestLogViewerCopyUsesClipboard(t *testing.T) {
	m := logViewerModel(t, []logEntry{{Time: time.Now(), Message: "hello"}})
	var copied string
	orig := clipboardWriteCmd
	clipboardWriteCmd = func(s string) tea.Cmd { copied = s; return func() tea.Msg { return nil } }
	defer func() { clipboardWriteCmd = orig }()

	_, cmd := m.handleUpdate(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if cmd == nil {
		t.Fatal("c should start a copy")
	}
	cmd()
	if !strings.Contains(copied, "hello") {
		t.Errorf("copied = %q", copied)
	}
}

// Without a Downloads folder to open the picker on, s falls straight back to
// the fixed destination so a save still succeeds.
func TestLogViewerSaveFallsBackWithoutDownloadsFolder(t *testing.T) {
	m := logViewerModel(t, []logEntry{{Time: time.Now(), Message: "hello"}})
	_, cmd := m.handleUpdate(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if cmd == nil {
		t.Fatal("s should start a save")
	}
	msg := cmd().(logsSavedMsg)
	if msg.Err != nil {
		t.Fatal(msg.Err)
	}
	dir := filepath.Join(os.Getenv("HOME"), "Downloads", "tidemail-logs")
	if filepath.Dir(msg.Path) != dir || !strings.HasSuffix(msg.Path, ".txt") {
		t.Errorf("path = %q; want a file directly in %q", msg.Path, dir)
	}
	info, err := os.Stat(msg.Path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("stat = %v, %v", info, err)
	}
	data, err := os.ReadFile(msg.Path)
	if err != nil || !strings.Contains(string(data), "hello") {
		t.Errorf("file contents = %q, %v", data, err)
	}
}

// With a Downloads folder present, s opens the same folder browser attachments
// use, so the destination can be chosen instead of always writing to a fixed
// path.
func TestLogViewerSaveOpensSharedFolderPicker(t *testing.T) {
	m := logViewerModel(t, []logEntry{{Time: time.Now(), Message: "hello"}})
	downloads := filepath.Join(os.Getenv("HOME"), "Downloads")
	if err := os.MkdirAll(downloads, 0o755); err != nil {
		t.Fatal(err)
	}

	next, cmd := m.handleUpdate(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = next.(Model)
	if cmd != nil {
		t.Fatal("opening the picker should not itself start a save")
	}
	if m.overlay != overlaySaveAttach || m.saveAttachPickerKind != saveAttachKindLogs {
		t.Fatalf("overlay=%v kind=%v", m.overlay, m.saveAttachPickerKind)
	}
	if m.saveAttachPicker.currentDir != downloads {
		t.Fatalf("picker opened on %q, want %q", m.saveAttachPicker.currentDir, downloads)
	}

	// Cursor starts on "select this folder"; confirming saves into Downloads
	// directly, matching where the attachment picker saves.
	next, cmd = m.handleSaveAttachPicker(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("confirming should start the save")
	}
	if m.overlay != overlayNone {
		t.Fatalf("overlay after confirm = %v, want overlayNone", m.overlay)
	}
	msg := cmd().(logsSavedMsg)
	if msg.Err != nil {
		t.Fatal(msg.Err)
	}
	if filepath.Dir(msg.Path) != downloads {
		t.Errorf("saved to %q, want directly in %q", msg.Path, downloads)
	}
	data, err := os.ReadFile(msg.Path)
	if err != nil || !strings.Contains(string(data), "hello") {
		t.Errorf("file contents = %q, %v", data, err)
	}
}

// Canceling the picker while saving logs must not fall back to a quick save;
// the shared picker's own Cancel closes everything, same as it does for
// attachments.
func TestLogViewerSavePickerCancelDoesNotSave(t *testing.T) {
	m := logViewerModel(t, []logEntry{{Time: time.Now(), Message: "hello"}})
	downloads := filepath.Join(os.Getenv("HOME"), "Downloads")
	if err := os.MkdirAll(downloads, 0o755); err != nil {
		t.Fatal(err)
	}
	next, _ := m.handleUpdate(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = next.(Model)

	next, cmd := m.handleSaveAttachPicker(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if cmd != nil {
		t.Fatal("cancel should not start a save")
	}
	if m.overlay != overlayNone {
		t.Fatalf("overlay after cancel = %v, want overlayNone", m.overlay)
	}
	entries, err := os.ReadDir(downloads)
	if err != nil || len(entries) != 0 {
		t.Fatalf("cancel wrote something to Downloads: %v %v", entries, err)
	}
}

func TestLogViewerCancelClosesWithoutCopyOrSave(t *testing.T) {
	m := logViewerModel(t, nil)
	next, cmd := m.handleUpdate(tea.KeyMsg{Type: tea.KeyEsc})
	nm := next.(Model)
	if nm.overlay != overlaySettings {
		t.Fatalf("esc should return to Settings, got overlay %v", nm.overlay)
	}
	if cmd != nil {
		t.Fatal("esc should not start a copy or save")
	}
}

// The shared folder picker's title says what it is for.
func TestSaveAttachPickerTitleReflectsKind(t *testing.T) {
	dir := t.TempDir()
	m := Model{keys: DefaultKeys}
	m.width, m.height = 100, 40
	if err := m.openSaveAttachPicker(dir); err != nil {
		t.Fatal(err)
	}
	m.overlay = overlaySaveAttach

	m.saveAttachPickerKind = saveAttachKindAttachments
	if view := m.renderOverlay("base"); !strings.Contains(view, "save attachments") {
		t.Errorf("attachments title missing:\n%s", view)
	}

	m.saveAttachPickerKind = saveAttachKindLogs
	if view := m.renderOverlay("base"); !strings.Contains(view, "save logs") || strings.Contains(view, "save attachments") {
		t.Errorf("logs title missing or wrong:\n%s", view)
	}
}
