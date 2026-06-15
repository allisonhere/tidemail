package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	tea "github.com/charmbracelet/bubbletea"
)

func TestCommandPaletteFiltersCommands(t *testing.T) {
	m := NewModel(nil, config.Config{
		Accounts: []config.AccountConfig{{Name: "Personal"}},
		Display:  config.DefaultConfig().Display,
	}, "dev", false)

	m.commandInput.SetValue("sync")
	items := m.filteredCommandItems()
	if len(items) != 2 {
		t.Fatalf("expected sync and sync-all commands, got %+v", items)
	}
	if items[0].id != "sync" || items[1].id != "sync-all" {
		t.Fatalf("unexpected filtered commands: %+v", items)
	}
}

func TestCommandPaletteOpensFromMainWithCtrlP(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = next.(Model)

	if m.overlay != overlayCommandPalette {
		t.Fatalf("expected command palette overlay, got %v", m.overlay)
	}
	if m.commandPaletteContext != commandPaletteMain {
		t.Fatalf("expected main context, got %v", m.commandPaletteContext)
	}
}

func TestCommandPaletteEnablesMessageActionsWhenMessageSelected(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.focused = paneMessages
	m.messages = []db.Message{{
		ID:        1,
		MailboxID: 10,
		UID:       20,
		Subject:   "Hello",
		Date:      time.Unix(1710000000, 0),
	}}
	m.filteredMessages = m.messages

	enabled := map[string]bool{}
	for _, item := range m.commandItems() {
		enabled[item.id] = item.enabled
	}
	for _, id := range []string{"reply", "archive", "move", "delete", "toggle-read"} {
		if !enabled[id] {
			t.Fatalf("expected %q to be enabled with a selected message; commands=%+v", id, m.commandItems())
		}
	}
}

func TestCommandPaletteOpensFromComposeWithComposeContext(t *testing.T) {
	m := NewModel(nil, config.Config{
		Accounts: []config.AccountConfig{{Name: "Personal", User: "me@example.com"}},
		Display:  config.DefaultConfig().Display,
	}, "dev", false)
	m.overlay = overlayCompose
	m.compose = NewCompose(m.cfg.Accounts[0], m.cfg.Accounts, nil)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	m = next.(Model)

	if m.overlay != overlayCommandPalette {
		t.Fatalf("expected command palette overlay, got %v", m.overlay)
	}
	if m.commandPaletteOrigin != overlayCompose {
		t.Fatalf("expected compose origin, got %v", m.commandPaletteOrigin)
	}
	if m.commandPaletteContext != commandPaletteCompose {
		t.Fatalf("expected compose context, got %v", m.commandPaletteContext)
	}
}

func TestCommandPaletteCancelRestoresComposeOverlay(t *testing.T) {
	m := NewModel(nil, config.Config{
		Accounts: []config.AccountConfig{{Name: "Personal", User: "me@example.com"}},
		Display:  config.DefaultConfig().Display,
	}, "dev", false)
	m.overlay = overlayCommandPalette
	m.commandPaletteOrigin = overlayCompose
	m.commandPaletteContext = commandPaletteCompose

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)

	if m.overlay != overlayCompose {
		t.Fatalf("expected compose overlay restored, got %v", m.overlay)
	}
}

func TestCommandPaletteShowsComposeContextCommands(t *testing.T) {
	m := NewModel(nil, config.Config{
		Accounts: []config.AccountConfig{
			{Name: "Personal", User: "me@example.com"},
			{Name: "Work", User: "me@work.com"},
		},
		Display: config.DefaultConfig().Display,
	}, "dev", false)
	m.commandPaletteContext = commandPaletteCompose
	m.compose = NewCompose(m.cfg.Accounts[0], m.cfg.Accounts, nil)
	m.compose.attachments = []attachmentFile{{Name: "notes.txt"}}

	got := map[string]bool{}
	for _, item := range m.commandItems() {
		got[item.id] = item.enabled
	}
	for _, id := range []string{"compose-send", "compose-grammar", "compose-attach", "compose-remove-attach", "compose-cycle-sender", "compose-close"} {
		if !got[id] {
			t.Fatalf("expected compose command %q enabled, got %+v", id, m.commandItems())
		}
	}
}

func TestCommandPaletteShowsSummaryContextCommands(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.commandPaletteContext = commandPaletteSummary
	m.summaryMessage = db.Message{Summary: "Short summary"}

	got := map[string]bool{}
	for _, item := range m.commandItems() {
		got[item.id] = item.enabled
	}
	for _, id := range []string{"summary-copy", "summary-save", "summary-close"} {
		if !got[id] {
			t.Fatalf("expected summary command %q enabled, got %+v", id, m.commandItems())
		}
	}
}

func TestCommandPaletteShowsSaveAttachContextCommands(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.commandPaletteContext = commandPaletteSaveAttach
	m.saveAttachPicker.currentDir = "/tmp"
	m.contentAttachments = []db.Attachment{{Filename: "a.txt", Data: []byte("a")}}

	got := map[string]bool{}
	for _, item := range m.commandItems() {
		got[item.id] = item.enabled
	}
	for _, id := range []string{"save-attach-save", "save-attach-up", "save-attach-cancel"} {
		if !got[id] {
			t.Fatalf("expected save-attach command %q enabled, got %+v", id, m.commandItems())
		}
	}
}

func TestCommandPaletteExecuteSummaryCloseRestoresNoOverlay(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.overlay = overlayCommandPalette
	m.commandPaletteOrigin = overlaySummary
	m.commandPaletteContext = commandPaletteSummary
	m.summaryMessage = db.Message{Summary: "Short summary"}

	next, _ := m.executeCommand("summary-close")
	m = next.(Model)

	if m.overlay != overlayNone {
		t.Fatalf("expected summary close to clear overlay, got %v", m.overlay)
	}
}

func TestCommandPaletteExecuteComposeCloseOpensDraftConfirmWhenDirty(t *testing.T) {
	m := NewModel(nil, config.Config{
		Accounts: []config.AccountConfig{{Name: "Personal", User: "me@example.com"}},
		Display:  config.DefaultConfig().Display,
	}, "dev", false)
	m.overlay = overlayCommandPalette
	m.commandPaletteOrigin = overlayCompose
	m.commandPaletteContext = commandPaletteCompose
	m.compose = NewCompose(m.cfg.Accounts[0], m.cfg.Accounts, nil)
	m.compose.bodyInput.SetValue("draft")

	next, _ := m.executeCommand("compose-close")
	m = next.(Model)

	if m.overlay != overlayDraftCloseConfirm {
		t.Fatalf("expected draft close confirm, got %v", m.overlay)
	}
}

func TestCommandPaletteExecuteSaveAttachSaveUsesCurrentFolder(t *testing.T) {
	dir := t.TempDir()
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.overlay = overlayCommandPalette
	m.commandPaletteOrigin = overlaySaveAttach
	m.commandPaletteContext = commandPaletteSaveAttach
	m.saveAttachPicker.currentDir = dir
	m.contentAttachments = []db.Attachment{{Filename: "a.txt", Data: []byte("hello")}}

	next, cmd := m.executeCommand("save-attach-save")
	m = next.(Model)

	if m.overlay != overlayNone {
		t.Fatalf("expected save-attach save to close overlay, got %v", m.overlay)
	}
	if cmd == nil {
		t.Fatal("expected save command")
	}
	msg := cmd()
	saved, ok := msg.(AttachmentsSavedMsg)
	if !ok {
		t.Fatalf("expected AttachmentsSavedMsg, got %T", msg)
	}
	if saved.Err != nil {
		t.Fatalf("save returned error: %v", saved.Err)
	}
	path := filepath.Join(dir, "a.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected saved file: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("unexpected saved content %q", data)
	}
}
