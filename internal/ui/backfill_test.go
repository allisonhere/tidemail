package ui

import (
	"errors"
	"testing"
	"time"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
)

// Search results come from the FTS index, which covers sender and body as well
// as subject. Combining a search with the unread-only toggle used to fall into
// a subject-only substring filter, silently dropping every result that matched
// on body or sender.
func TestApplyFilterKeepsNonSubjectSearchHitsWhenUnreadOnly(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "dev", false)
	m.searchMode = true
	m.searchQuery = "retrospective" // matched the body, not the subject
	m.messages = []db.Message{
		{ID: 1, Subject: "Weekly notes", BodyText: "retrospective agenda", Read: false},
		{ID: 2, Subject: "Weekly notes", BodyText: "retrospective agenda", Read: true},
	}

	m.showUnreadOnly = false
	m.applyFilter()
	if len(m.filteredMessages) != 2 {
		t.Fatalf("expected both search hits without the unread filter, got %d", len(m.filteredMessages))
	}

	m.showUnreadOnly = true
	m.applyFilter()
	if len(m.filteredMessages) != 1 {
		t.Fatalf("expected the unread body-match to survive the unread filter, got %d", len(m.filteredMessages))
	}
	if m.filteredMessages[0].ID != 1 {
		t.Fatalf("expected the unread message, got ID %d", m.filteredMessages[0].ID)
	}
}

func TestLoadOlderMessagesCmdGuards(t *testing.T) {
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
	mailboxID, err := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "INBOX"})
	if err != nil {
		t.Fatalf("UpsertMailbox: %v", err)
	}

	m := NewModel(database, config.DefaultConfig(), "dev", false)

	// An empty cache has no paging cursor, so there is nothing older to ask for.
	cmd := m.loadOlderMessagesCmd(mailboxID)
	if cmd == nil {
		t.Fatal("expected a command reporting exhaustion for an empty mailbox")
	}
	msg, ok := cmd().(OlderMessagesLoadedMsg)
	if !ok {
		t.Fatalf("expected OlderMessagesLoadedMsg, got %T", cmd())
	}
	if !msg.Exhausted || msg.Err != nil {
		t.Fatalf("expected a clean exhausted result, got %+v", msg)
	}

	if err := database.UpsertMessage(db.Message{MailboxID: mailboxID, UID: 50, Subject: "s", Date: time.Unix(1, 0)}); err != nil {
		t.Fatalf("UpsertMessage: %v", err)
	}

	// A mailbox already known to be fully paged must not hit the network again.
	m.olderExhausted[mailboxID] = true
	if cmd := m.loadOlderMessagesCmd(mailboxID); cmd != nil {
		t.Fatal("expected no command once the mailbox is exhausted")
	}
	delete(m.olderExhausted, mailboxID)

	// Neither must a mailbox with server work already in flight — back-fill and
	// sync write to the same rows.
	m.syncing[mailboxID] = true
	if cmd := m.loadOlderMessagesCmd(mailboxID); cmd != nil {
		t.Fatal("expected no command while a sync is in flight")
	}
}

func TestOlderMessagesLoadedMsgUpdatesState(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "dev", false)
	m.syncing[7] = true

	next, _ := m.Update(OlderMessagesLoadedMsg{MailboxID: 7, Count: 100})
	m = next.(Model)
	if m.syncing[7] {
		t.Fatal("expected the in-flight marker to be cleared")
	}
	if m.olderExhausted[7] {
		t.Fatal("expected a full page to leave more history available")
	}

	// A short page means the server had nothing more to give.
	next, _ = m.Update(OlderMessagesLoadedMsg{MailboxID: 7, Count: 12, Exhausted: true})
	m = next.(Model)
	if !m.olderExhausted[7] {
		t.Fatal("expected a short page to mark the mailbox exhausted")
	}

	// Errors surface rather than being swallowed, and still clear the marker.
	m.syncing[8] = true
	next, _ = m.Update(OlderMessagesLoadedMsg{MailboxID: 8, Err: errors.New("connection reset")})
	m = next.(Model)
	if m.syncing[8] {
		t.Fatal("expected a failed back-fill to clear the in-flight marker")
	}
	if m.statusMsg == "" || !m.statusErr {
		t.Fatalf("expected a back-fill failure to surface as an error status, got %q", m.statusMsg)
	}
}
