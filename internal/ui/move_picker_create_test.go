package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
)

func TestMovePickerCreateFolder(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	accountID, err := database.AddAccount("Personal", "")
	if err != nil {
		t.Fatalf("AddAccount: %v", err)
	}
	inboxID, err := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "INBOX", Delimiter: "/"})
	if err != nil {
		t.Fatalf("UpsertMailbox: %v", err)
	}
	// A second selectable folder so the picker has entries to open.
	if _, err := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "Archive", Delimiter: "/"}); err != nil {
		t.Fatalf("UpsertMailbox: %v", err)
	}

	cfg := config.DefaultConfig()
	// No IMAPHost configured, so createFolderCmd persists locally without a network call.
	cfg.Accounts = []config.AccountConfig{{Name: "Personal"}}
	m := NewModel(database, cfg, "dev", false)
	next, _ := m.Update(AccountsLoadedMsg{
		Accounts:  []db.Account{{ID: accountID, Name: "Personal"}},
		Mailboxes: mustListMailboxes(t, database, accountID),
	})
	m = next.(Model)

	msgToMove := db.Message{ID: 1, MailboxID: inboxID}
	m.openMovePicker([]db.Message{msgToMove})
	if m.overlay != overlayMoveMessage {
		t.Fatalf("expected move overlay open, got %v", m.overlay)
	}

	// n -> enter create mode
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = next.(Model)
	if !m.movePicker.creating {
		t.Fatalf("expected picker to be in creating mode after 'n'")
	}

	// type the folder name
	for _, r := range "Receipts" {
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(Model)
	}

	// enter -> returns the create command
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil {
		t.Fatalf("expected a create-folder command on enter")
	}
	if m.movePicker.creating {
		t.Fatalf("expected creating mode to end after submit")
	}

	// run the command and feed the result back
	created, ok := cmd().(FolderCreatedMsg)
	if !ok {
		t.Fatalf("expected FolderCreatedMsg, got %T", cmd())
	}
	if created.Err != nil {
		t.Fatalf("create folder errored: %v", created.Err)
	}
	if created.Name != "Receipts" {
		t.Fatalf("created folder name = %q, want Receipts", created.Name)
	}

	next, _ = m.Update(created)
	m = next.(Model)

	if m.mailboxByID(created.MailboxID) == nil {
		t.Fatalf("expected new folder in m.mailboxes")
	}
	if m.movePicker.currentPath != "Receipts" {
		t.Fatalf("expected picker to navigate into new folder, path = %q", m.movePicker.currentPath)
	}
	if len(m.movePicker.entries) == 0 || !m.movePicker.entries[m.movePicker.cursor].isConfirm {
		t.Fatalf("expected cursor on a 'move here' entry, entries = %+v", m.movePicker.entries)
	}
}

func mustListMailboxes(t *testing.T, database *db.DB, accountID int64) []db.Mailbox {
	t.Helper()
	mbs, err := database.ListMailboxes(accountID)
	if err != nil {
		t.Fatalf("ListMailboxes: %v", err)
	}
	return mbs
}
