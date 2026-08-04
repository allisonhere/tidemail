package db

import (
	"testing"
	"time"
)

func TestOldestMessageUIDTracksPagingCursor(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	accountID, err := database.AddAccount("Personal", "")
	if err != nil {
		t.Fatalf("AddAccount: %v", err)
	}
	mailboxID, err := database.UpsertMailbox(Mailbox{AccountID: accountID, Name: "INBOX"})
	if err != nil {
		t.Fatalf("UpsertMailbox: %v", err)
	}

	// An empty mailbox has no cursor yet — there is no history to page past.
	uid, err := database.OldestMessageUID(mailboxID)
	if err != nil {
		t.Fatalf("OldestMessageUID on empty mailbox: %v", err)
	}
	if uid != 0 {
		t.Fatalf("expected 0 for an empty mailbox, got %d", uid)
	}

	for _, u := range []uint32{40, 12, 91} {
		if err := database.UpsertMessage(Message{MailboxID: mailboxID, UID: u, Subject: "s", Date: time.Unix(1, 0)}); err != nil {
			t.Fatalf("UpsertMessage: %v", err)
		}
	}
	if uid, err = database.OldestMessageUID(mailboxID); err != nil {
		t.Fatalf("OldestMessageUID: %v", err)
	}
	if uid != 12 {
		t.Fatalf("expected the lowest cached UID (12), got %d", uid)
	}

	// The cursor is per-mailbox: another mailbox's lower UID must not leak in.
	otherID, err := database.UpsertMailbox(Mailbox{AccountID: accountID, Name: "Archive"})
	if err != nil {
		t.Fatalf("UpsertMailbox: %v", err)
	}
	if err := database.UpsertMessage(Message{MailboxID: otherID, UID: 3, Subject: "s", Date: time.Unix(1, 0)}); err != nil {
		t.Fatalf("UpsertMessage: %v", err)
	}
	if uid, err = database.OldestMessageUID(mailboxID); err != nil {
		t.Fatalf("OldestMessageUID: %v", err)
	}
	if uid != 12 {
		t.Fatalf("expected the cursor to stay scoped to its mailbox, got %d", uid)
	}
}

// Search runs on every keystroke, so a half-typed word must still match.
func TestSearchAllMessagesMatchesPartialWords(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	accountID, err := database.AddAccount("Personal", "")
	if err != nil {
		t.Fatalf("AddAccount: %v", err)
	}
	mailboxID, err := database.UpsertMailbox(Mailbox{AccountID: accountID, Name: "INBOX"})
	if err != nil {
		t.Fatalf("UpsertMailbox: %v", err)
	}
	if err := database.UpsertMessage(Message{
		MailboxID: mailboxID, UID: 1,
		Subject:  "Quarterly retrospective",
		From:     "alice@example.com",
		BodyText: "notes about deployment",
		Date:     time.Unix(1, 0),
	}); err != nil {
		t.Fatalf("UpsertMessage: %v", err)
	}

	for _, q := range []string{"retro", "retrospective", "ali", "deploy"} {
		found, err := database.SearchAllMessages(q, false)
		if err != nil {
			t.Fatalf("SearchAllMessages(%q): %v", q, err)
		}
		if len(found) != 1 {
			t.Fatalf("expected %q to match while typing, got %d results", q, len(found))
		}
	}

	// Earlier terms stay exact; only the trailing one is a prefix.
	found, err := database.SearchAllMessages("retrospective deploy", false)
	if err != nil {
		t.Fatalf("SearchAllMessages multi-term: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("expected multi-term search to match, got %d", len(found))
	}
	if found, err = database.SearchAllMessages("nomatch retro", false); err != nil || len(found) != 0 {
		t.Fatalf("expected an unmatched leading term to exclude the message, got %d, err %v", len(found), err)
	}
}
