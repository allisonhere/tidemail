package ui

import (
	"fmt"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
)

// newMailboxListModel builds a model parked on one mailbox holding count
// messages, newest first, the way the list queries order them.
func newMailboxListModel(mailboxID int64, count int) (Model, []db.Message) {
	cfg := config.DefaultConfig()
	m := NewModel(nil, cfg, "dev", false)
	m.width = 100
	m.height = 30
	m.focused = paneMessages
	m.accounts = []db.Account{{ID: 1, Name: "Personal"}}
	m.mailboxes = []db.Mailbox{{ID: mailboxID, AccountID: 1, Name: "INBOX"}}
	m.rebuildSidebar()
	for i, row := range m.sidebarRows {
		if row.kind == rowKindMailbox && row.mailboxID == mailboxID {
			m.sidebarCursor = i
			break
		}
	}

	msgs := make([]db.Message, count)
	for i := range msgs {
		msgs[i] = db.Message{ID: int64(i + 1), MailboxID: mailboxID, Subject: fmt.Sprintf("msg %d", i+1), Date: time.Unix(int64(1000-i), 0)}
	}
	m.messages = msgs
	m.applyFilter()
	return m, msgs
}

// arrival is a message newer than everything newMailboxListModel builds, so it
// sorts to index 0 the way genuinely new mail does.
func arrival(mailboxID int64) db.Message {
	return db.Message{ID: 999, MailboxID: mailboxID, Subject: "new mail", Date: time.Unix(2000, 0)}
}

// TestMailboxReloadAtTopKeepsNewMailInView covers the case the scroll-position
// preservation used to get wrong: sitting at the top of the list, mail arriving
// above the focused row was pushed straight off the top edge, so it was never
// seen. The offset must stay at 0 so the arrival is on screen.
func TestMailboxReloadAtTopKeepsNewMailInView(t *testing.T) {
	const mailboxID = int64(7)
	m, msgs := newMailboxListModel(mailboxID, 30)

	// User is at the very top of the list.
	m.messageCursor = 0
	m.listOffset = 0
	focusedID := m.filteredMessages[m.messageCursor].ID

	reloaded := append([]db.Message{arrival(mailboxID)}, msgs...)
	next, _ := m.Update(MessagesLoadedMsg{MailboxID: mailboxID, Messages: reloaded})
	m = next.(Model)

	if m.listOffset != 0 {
		t.Fatalf("expected the list to stay at the top so new mail is visible, got offset %d", m.listOffset)
	}
	if m.messageCursor != 1 {
		t.Fatalf("expected the focused row to shift down to index 1, got %d", m.messageCursor)
	}
	if got := m.filteredMessages[m.messageCursor].ID; got != focusedID {
		t.Fatalf("expected the selection to stay on message %d, landed on %d", focusedID, got)
	}
	if got := m.filteredMessages[0].ID; got != 999 {
		t.Fatalf("expected the arrival at index 0, got message %d", got)
	}
}

// TestMailboxReloadWhenScrolledStillHoldsScreenLine guards the behavior the
// top-of-list case carves out from: anywhere else in the list, the focused row
// keeps its screen line and the offset shifts with it.
func TestMailboxReloadWhenScrolledStillHoldsScreenLine(t *testing.T) {
	const mailboxID = int64(7)
	m, msgs := newMailboxListModel(mailboxID, 30)

	// User has scrolled down and parked mid-list.
	m.messageCursor = 20
	m.listOffset = 14
	focusedID := m.filteredMessages[m.messageCursor].ID

	reloaded := append([]db.Message{arrival(mailboxID)}, msgs...)
	next, _ := m.Update(MessagesLoadedMsg{MailboxID: mailboxID, Messages: reloaded})
	m = next.(Model)

	if m.messageCursor != 21 {
		t.Fatalf("expected cursor index to shift to 21, got %d", m.messageCursor)
	}
	if m.listOffset != 15 {
		t.Fatalf("expected offset to follow the cursor to 15, got %d", m.listOffset)
	}
	if got := m.filteredMessages[m.messageCursor].ID; got != focusedID {
		t.Fatalf("expected the selection to stay on message %d, landed on %d", focusedID, got)
	}
	if before, after := 20-14, m.messageCursor-m.listOffset; before != after {
		t.Fatalf("expected the focused row to hold screen line %d, moved to %d", before, after)
	}
}
