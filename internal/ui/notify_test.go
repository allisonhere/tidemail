package ui

import (
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/db"
)

// A re-fetch of an already-stored UID (IMAP SINCE is date-granular) must not be
// reported as new again — otherwise every poll re-notifies for the same mail.
func TestStoreFetchedMessagesDeduplicatesByUID(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

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

	msg := db.Message{UID: 7, From: "a@b.com", Subject: "Hi"}

	first, err := storeFetchedMessages(database, mailboxID, []db.Message{msg})
	if err != nil {
		t.Fatalf("first store: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("first fetch: expected 1 new, got %d", len(first))
	}

	// Same UID re-fetched: the upsert updates the row but it is not new mail.
	second, err := storeFetchedMessages(database, mailboxID, []db.Message{msg})
	if err != nil {
		t.Fatalf("second store: %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("re-fetch: expected 0 new, got %d", len(second))
	}
}

// Already-read mail (e.g. read on another device) is stored but is not a new arrival.
func TestStoreFetchedMessagesIgnoresRead(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	accountID, _ := database.AddAccount("Personal", "")
	mailboxID, _ := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "INBOX"})

	newMsgs, err := storeFetchedMessages(database, mailboxID, []db.Message{{UID: 9, Read: true, Subject: "Seen"}})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if len(newMsgs) != 0 {
		t.Fatalf("expected read mail to count as 0 new, got %d", len(newMsgs))
	}
}

func TestStoreFetchedMessagesSkipsLocallyDeletedUID(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	accountID, _ := database.AddAccount("Personal", "")
	mailboxID, _ := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "INBOX"})
	msg := db.Message{UID: 11, MessageID: "<deleted@example.com>", Subject: "Delete me"}
	if err := database.UpsertMessage(db.Message{MailboxID: mailboxID, UID: msg.UID, MessageID: msg.MessageID, Subject: msg.Subject}); err != nil {
		t.Fatalf("seed message: %v", err)
	}
	stored, err := database.ListMessages(mailboxID)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(stored) != 1 {
		t.Fatalf("expected seeded message, got %d", len(stored))
	}
	if err := database.DeleteMessage(stored[0].ID); err != nil {
		t.Fatalf("delete message: %v", err)
	}

	newMsgs, err := storeFetchedMessages(database, mailboxID, []db.Message{msg})
	if err != nil {
		t.Fatalf("store refetched deleted message: %v", err)
	}
	if len(newMsgs) != 0 {
		t.Fatalf("expected deleted refetch to count as 0 new, got %d", len(newMsgs))
	}
	remaining, err := database.ListMessages(mailboxID)
	if err != nil {
		t.Fatalf("list remaining: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("expected deleted refetch to stay hidden, got %+v", remaining)
	}
}

func TestComposeNotificationSingle(t *testing.T) {
	title, body := composeNotification("Work", []db.Message{
		{From: "Jane Doe <jane@example.com>", Subject: "Lunch?"},
	})
	if title != "Jane Doe · Work" {
		t.Fatalf("title = %q", title)
	}
	if body != "Lunch?" {
		t.Fatalf("body = %q", body)
	}
}

func TestComposeNotificationSingleFallbacks(t *testing.T) {
	// No display name -> bare address; empty subject -> placeholder; no account -> no suffix.
	title, body := composeNotification("", []db.Message{
		{From: "noreply@example.com", Subject: "   "},
	})
	if title != "noreply@example.com" {
		t.Fatalf("title = %q", title)
	}
	if body != "(no subject)" {
		t.Fatalf("body = %q", body)
	}
}

func TestComposeNotificationBatchCapsAndEscapes(t *testing.T) {
	msgs := make([]db.Message, 7)
	for i := range msgs {
		msgs[i] = db.Message{From: "x@y.com", Subject: "Sub"}
	}
	// One sender carries markup-significant chars that must be escaped.
	msgs[0] = db.Message{From: "A & B <ab@y.com>", Subject: "<tag>"}

	title, body := composeNotification("Acct", msgs)
	if !strings.HasPrefix(title, "7 new messages") {
		t.Fatalf("title = %q", title)
	}
	if strings.Contains(body, "<tag>") || strings.Contains(body, "A & B") {
		t.Fatalf("body not escaped: %q", body)
	}
	if !strings.Contains(body, "&amp;") || !strings.Contains(body, "&lt;tag&gt;") {
		t.Fatalf("expected escaped entities in body: %q", body)
	}
	if !strings.Contains(body, "…and 2 more") {
		t.Fatalf("expected overflow line, got: %q", body)
	}
	// 5 capped lines + 1 overflow line.
	if got := strings.Count(body, "\n") + 1; got != 6 {
		t.Fatalf("expected 6 body lines, got %d", got)
	}
}
