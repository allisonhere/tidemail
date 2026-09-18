package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
)

// openFixture builds a model over a temp DB holding one account, one INBOX and
// one message, and returns the model with the ids of both.
func openFixture(t *testing.T) (Model, int64, int64) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	_, mailboxes, err := database.SaveAccountWithMailboxes(0, "config-1", "work", "", []db.Mailbox{{Name: "INBOX"}})
	if err != nil {
		t.Fatalf("save account: %v", err)
	}
	if len(mailboxes) != 1 {
		t.Fatalf("fixture mailboxes = %v", mailboxes)
	}
	inbox := mailboxes[0]
	if err := database.UpsertMessage(db.Message{
		MailboxID: inbox.ID, UID: 1, MessageID: "<one@example.com>",
		Subject: "standup notes", From: "ana@example.com",
		Date: time.Now().Add(-5 * time.Minute),
	}); err != nil {
		t.Fatalf("save message: %v", err)
	}

	messages, err := database.ListMessages(inbox.ID)
	if err != nil || len(messages) != 1 {
		t.Fatalf("fixture messages = %v (%v)", messages, err)
	}

	cfg := config.DefaultConfig()
	cfg.Accounts = []config.AccountConfig{{ID: "config-1", Name: "work"}}
	return NewModel(database, cfg, "dev", false), inbox.ID, messages[0].ID
}

// --open asks the model to land on a message: the pending fields are the same
// ones the startup load already consumes, so the folder and the cursor follow
// without a second mechanism.
func TestOpenMessageAtStartupPendsTheMailboxAndTheMessage(t *testing.T) {
	m, mailboxID, messageID := openFixture(t)
	m.OpenMessageAtStartup(messageID)

	if m.pendingSelectMailboxID != mailboxID {
		t.Errorf("pending mailbox = %d, want %d", m.pendingSelectMailboxID, mailboxID)
	}
	if m.pendingSelectMessageID != messageID {
		t.Errorf("pending message = %d, want %d", m.pendingSelectMessageID, messageID)
	}
	// The reader asked for a message, so the keyboard starts in the list.
	if m.focused != paneMessages {
		t.Errorf("focused = %v, want paneMessages", m.focused)
	}
}

// An id this cache does not have must not stop the launch or leave a pending
// request behind that fires on some later load.
func TestOpenMessageAtStartupWithoutTheMessage(t *testing.T) {
	m, _, _ := openFixture(t)
	m.OpenMessageAtStartup(987654321)

	if m.pendingSelectMailboxID != 0 || m.pendingSelectMessageID != 0 {
		t.Errorf("pending = %d/%d, want nothing pending",
			m.pendingSelectMailboxID, m.pendingSelectMessageID)
	}
	if !strings.Contains(m.statusMsg, "987654321") {
		t.Errorf("status = %q, want it to name the message", m.statusMsg)
	}
}

// And the cursor actually lands: drive the startup load the way the program
// does, with the accounts the DB holds and the messages a folder load delivers.
func TestOpenMessageAtStartupLandsTheCursor(t *testing.T) {
	m, mailboxID, messageID := openFixture(t)
	// A second message, newer than the one asked for, so landing on the right
	// row is a real assertion rather than "the first row happens to be it".
	if err := m.db.UpsertMessage(db.Message{
		MailboxID: mailboxID, UID: 2, MessageID: "<two@example.com>",
		Subject: "later mail", From: "sam@example.com", Date: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	m.OpenMessageAtStartup(messageID)

	// The account list arrives: this is what consumes the pending mailbox and
	// decides which folder loads.
	accounts, _ := m.loadAccountsCmd()().(AccountsLoadedMsg)
	next, _ := m.Update(accounts)
	m = next.(Model)

	// Then the folder it selected loads, in the DB's own order.
	messages, err := m.db.ListMessages(mailboxID)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 {
		t.Fatalf("fixture messages = %d, want 2", len(messages))
	}
	next, _ = m.Update(MessagesLoadedMsg{MailboxID: mailboxID, Messages: messages})
	m = next.(Model)

	current := m.currentRowMessage()
	if current == nil || current.ID != messageID {
		t.Fatalf("cursor is on %v, want message %d", current, messageID)
	}
	if m.contentMessageID != messageID {
		t.Errorf("reading pane shows %d, want %d", m.contentMessageID, messageID)
	}
}
