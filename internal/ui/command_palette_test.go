package ui

import (
	"testing"
	"time"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
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
	for _, id := range []string{"reply", "archive", "delete", "toggle-read"} {
		if !enabled[id] {
			t.Fatalf("expected %q to be enabled with a selected message; commands=%+v", id, m.commandItems())
		}
	}
}
