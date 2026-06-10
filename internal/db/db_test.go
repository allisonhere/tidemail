package db

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestDBAccountsMailboxesAndMessages(t *testing.T) {
	tmp := t.TempDir()
	database, err := openSQLite(filepath.Join(tmp, "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if err := database.init(); err != nil {
		t.Fatal(err)
	}

	accountID, err := database.AddAccount("Personal", "#7aa2f7")
	if err != nil {
		t.Fatal(err)
	}
	mailboxID, err := database.UpsertMailbox(Mailbox{
		AccountID:   accountID,
		Name:        "INBOX",
		DisplayName: "Inbox",
		Delimiter:   "/",
		Flags:       []string{"\\HasNoChildren"},
		UnreadCount: 2,
		LastSynced:  time.Unix(1710000000, 0),
	})
	if err != nil {
		t.Fatal(err)
	}

	msg := Message{
		MailboxID:     mailboxID,
		UID:           42,
		MessageID:     "<hello@example.com>",
		Subject:       "Hello",
		From:          "Alice <alice@example.com>",
		To:            "Bob <bob@example.com>",
		Date:          time.Unix(1710000100, 0),
		BodyText:      "Body",
		Flags:         []string{"\\Seen"},
		Read:          true,
		HasAttachment: true,
	}
	if err := database.UpsertMessage(msg); err != nil {
		t.Fatal(err)
	}

	accounts, err := database.ListAccounts()
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 || accounts[0].Name != "Personal" {
		t.Fatalf("unexpected accounts: %+v", accounts)
	}

	mailboxes, err := database.ListMailboxes(accountID)
	if err != nil {
		t.Fatal(err)
	}
	if len(mailboxes) != 1 || mailboxes[0].Name != "INBOX" {
		t.Fatalf("unexpected mailboxes: %+v", mailboxes)
	}
	if got := mailboxes[0].Flags; len(got) != 1 || got[0] != "\\HasNoChildren" {
		t.Fatalf("unexpected mailbox flags: %+v", got)
	}

	messages, err := database.ListMessages(mailboxID)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected one message, got %d", len(messages))
	}
	if messages[0].Subject != "Hello" || !messages[0].Read || !messages[0].HasAttachment {
		t.Fatalf("unexpected message: %+v", messages[0])
	}
}

func TestDBUnreadAndSummaryUpdates(t *testing.T) {
	tmp := t.TempDir()
	database, err := openSQLite(filepath.Join(tmp, "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if err := database.init(); err != nil {
		t.Fatal(err)
	}

	accountID, err := database.AddAccount("Work", "")
	if err != nil {
		t.Fatal(err)
	}
	mailboxID, err := database.UpsertMailbox(Mailbox{AccountID: accountID, Name: "INBOX"})
	if err != nil {
		t.Fatal(err)
	}

	for _, msg := range []Message{
		{MailboxID: mailboxID, UID: 1, Subject: "Old unread", Date: time.Unix(1710000000, 0)},
		{MailboxID: mailboxID, UID: 2, Subject: "Read", Date: time.Unix(1710000100, 0), Read: true},
		{MailboxID: mailboxID, UID: 3, Subject: "New unread", Date: time.Unix(1710000200, 0)},
	} {
		if err := database.UpsertMessage(msg); err != nil {
			t.Fatal(err)
		}
	}

	unread, err := database.ListUnreadMessages(mailboxID)
	if err != nil {
		t.Fatal(err)
	}
	if len(unread) != 2 {
		t.Fatalf("expected two unread messages, got %d", len(unread))
	}
	if unread[0].Subject != "New unread" || unread[1].Subject != "Old unread" {
		t.Fatalf("unexpected unread order: %+v", unread)
	}

	if err := database.MarkRead(unread[0].ID, true); err != nil {
		t.Fatal(err)
	}
	count, err := database.CountUnread(mailboxID)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one unread message, got %d", count)
	}

	if err := database.SaveSummary(unread[1].ID, "summary"); err != nil {
		t.Fatal(err)
	}
	got, err := database.GetMessage(unread[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary != "summary" {
		t.Fatalf("expected saved summary, got %q", got.Summary)
	}
}

func TestDBListMessagesUnreadFirstGroupsUnreadBeforeRead(t *testing.T) {
	tmp := t.TempDir()
	database, err := openSQLite(filepath.Join(tmp, "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if err := database.init(); err != nil {
		t.Fatal(err)
	}

	accountID, err := database.AddAccount("Work", "")
	if err != nil {
		t.Fatal(err)
	}
	mailboxID, err := database.UpsertMailbox(Mailbox{AccountID: accountID, Name: "INBOX"})
	if err != nil {
		t.Fatal(err)
	}

	for _, msg := range []Message{
		{MailboxID: mailboxID, UID: 1, Subject: "Old unread", Date: time.Unix(1710000000, 0)},
		{MailboxID: mailboxID, UID: 2, Subject: "New read", Date: time.Unix(1710000300, 0), Read: true},
		{MailboxID: mailboxID, UID: 3, Subject: "New unread", Date: time.Unix(1710000200, 0)},
		{MailboxID: mailboxID, UID: 4, Subject: "Old read", Date: time.Unix(1710000100, 0), Read: true},
	} {
		if err := database.UpsertMessage(msg); err != nil {
			t.Fatal(err)
		}
	}

	msgs, err := database.ListMessagesUnreadFirst(mailboxID)
	if err != nil {
		t.Fatal(err)
	}
	got := subjectsOf(msgs)
	want := []string{"New unread", "Old unread", "New read", "Old read"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("unexpected unread-first order: got %v want %v", got, want)
	}
}

func TestDBListUnifiedInboxUsesInboxMailboxesAcrossAccounts(t *testing.T) {
	tmp := t.TempDir()
	database, err := openSQLite(filepath.Join(tmp, "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if err := database.init(); err != nil {
		t.Fatal(err)
	}

	personalID, err := database.AddAccount("Personal", "")
	if err != nil {
		t.Fatal(err)
	}
	workID, err := database.AddAccount("Work", "")
	if err != nil {
		t.Fatal(err)
	}
	personalInboxID, err := database.UpsertMailbox(Mailbox{AccountID: personalID, Name: "INBOX", Flags: []string{"\\HasNoChildren"}})
	if err != nil {
		t.Fatal(err)
	}
	workInboxID, err := database.UpsertMailbox(Mailbox{AccountID: workID, Name: "Inbox", Flags: []string{"\\Inbox"}})
	if err != nil {
		t.Fatal(err)
	}
	archiveID, err := database.UpsertMailbox(Mailbox{AccountID: personalID, Name: "Archive", Flags: []string{"\\Archive"}})
	if err != nil {
		t.Fatal(err)
	}

	for _, msg := range []Message{
		{MailboxID: personalInboxID, UID: 1, Subject: "Personal older", Date: time.Unix(1710000000, 0)},
		{MailboxID: workInboxID, UID: 2, Subject: "Work newest", Date: time.Unix(1710000200, 0)},
		{MailboxID: personalInboxID, UID: 3, Subject: "Personal read", Date: time.Unix(1710000300, 0), Read: true},
		{MailboxID: archiveID, UID: 4, Subject: "Archived", Date: time.Unix(1710000400, 0)},
	} {
		if err := database.UpsertMessage(msg); err != nil {
			t.Fatal(err)
		}
	}

	all, err := database.ListUnifiedInbox(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("expected three inbox messages, got %d: %+v", len(all), all)
	}
	if all[0].Subject != "Personal read" || all[1].Subject != "Work newest" || all[2].Subject != "Personal older" {
		t.Fatalf("unexpected unified inbox order: %+v", all)
	}

	unread, err := database.ListUnifiedInbox(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(unread) != 2 {
		t.Fatalf("expected two unread inbox messages, got %d: %+v", len(unread), unread)
	}
	if unread[0].Subject != "Work newest" || unread[1].Subject != "Personal older" {
		t.Fatalf("unexpected unread unified inbox order: %+v", unread)
	}
}

func TestDBListUnifiedInboxUnreadFirstGroupsUnreadBeforeRead(t *testing.T) {
	tmp := t.TempDir()
	database, err := openSQLite(filepath.Join(tmp, "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if err := database.init(); err != nil {
		t.Fatal(err)
	}

	personalID, err := database.AddAccount("Personal", "")
	if err != nil {
		t.Fatal(err)
	}
	workID, err := database.AddAccount("Work", "")
	if err != nil {
		t.Fatal(err)
	}
	personalInboxID, err := database.UpsertMailbox(Mailbox{AccountID: personalID, Name: "INBOX"})
	if err != nil {
		t.Fatal(err)
	}
	workInboxID, err := database.UpsertMailbox(Mailbox{AccountID: workID, Name: "Inbox", Flags: []string{"\\Inbox"}})
	if err != nil {
		t.Fatal(err)
	}

	for _, msg := range []Message{
		{MailboxID: personalInboxID, UID: 1, Subject: "Old unread", Date: time.Unix(1710000000, 0)},
		{MailboxID: personalInboxID, UID: 2, Subject: "Newest read", Date: time.Unix(1710000400, 0), Read: true},
		{MailboxID: workInboxID, UID: 3, Subject: "New unread", Date: time.Unix(1710000300, 0)},
		{MailboxID: workInboxID, UID: 4, Subject: "Old read", Date: time.Unix(1710000200, 0), Read: true},
	} {
		if err := database.UpsertMessage(msg); err != nil {
			t.Fatal(err)
		}
	}

	msgs, err := database.ListUnifiedInboxUnreadFirst(false)
	if err != nil {
		t.Fatal(err)
	}
	got := subjectsOf(msgs)
	want := []string{"New unread", "Old unread", "Newest read", "Old read"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("unexpected unified unread-first order: got %v want %v", got, want)
	}

	unread, err := database.ListUnifiedInboxUnreadFirst(true)
	if err != nil {
		t.Fatal(err)
	}
	got = subjectsOf(unread)
	want = []string{"New unread", "Old unread"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("unexpected unread-only unified order: got %v want %v", got, want)
	}
}

func subjectsOf(msgs []Message) []string {
	out := make([]string, 0, len(msgs))
	for _, msg := range msgs {
		out = append(out, msg.Subject)
	}
	return out
}

func TestDBMoveAndDeleteMessageUpdateLocalState(t *testing.T) {
	tmp := t.TempDir()
	database, err := openSQLite(filepath.Join(tmp, "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if err := database.init(); err != nil {
		t.Fatal(err)
	}

	accountID, err := database.AddAccount("Personal", "")
	if err != nil {
		t.Fatal(err)
	}
	inboxID, err := database.UpsertMailbox(Mailbox{AccountID: accountID, Name: "INBOX"})
	if err != nil {
		t.Fatal(err)
	}
	archiveID, err := database.UpsertMailbox(Mailbox{AccountID: accountID, Name: "Archive", Flags: []string{"\\Archive"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpsertMessage(Message{MailboxID: inboxID, UID: 44, Subject: "Move me", Date: time.Unix(1710000000, 0)}); err != nil {
		t.Fatal(err)
	}
	messages, err := database.ListMessages(inboxID)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected seeded message, got %+v", messages)
	}

	if err := database.MoveMessage(messages[0].ID, archiveID); err != nil {
		t.Fatal(err)
	}
	moved, err := database.GetMessage(messages[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if moved.MailboxID != archiveID {
		t.Fatalf("expected message mailbox %d, got %d", archiveID, moved.MailboxID)
	}

	if err := database.DeleteMessage(moved.ID); err != nil {
		t.Fatal(err)
	}
	remaining, err := database.ListMessages(archiveID)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Fatalf("expected deleted message to be gone, got %+v", remaining)
	}
	var tombstones int
	if err := database.QueryRow(`SELECT COUNT(*) FROM deleted_messages WHERE mailbox_id = ? AND uid = ?`, archiveID, uint32(44)).Scan(&tombstones); err != nil {
		t.Fatal(err)
	}
	if tombstones != 1 {
		t.Fatalf("expected deleted message tombstone, got %d", tombstones)
	}
}

func TestDBDeleteMessageMissingReturnsError(t *testing.T) {
	tmp := t.TempDir()
	database, err := openSQLite(filepath.Join(tmp, "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if err := database.DeleteMessage(999); err == nil {
		t.Fatal("expected missing message delete to fail")
	}
}

func TestMessageDeletedLocallyPrefersMessageIDOverUID(t *testing.T) {
	tmp := t.TempDir()
	database, err := openSQLite(filepath.Join(tmp, "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if err := database.init(); err != nil {
		t.Fatal(err)
	}
	accountID, err := database.AddAccount("Personal", "")
	if err != nil {
		t.Fatal(err)
	}
	mailboxID, err := database.UpsertMailbox(Mailbox{AccountID: accountID, Name: "INBOX"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := database.Exec(`
		INSERT INTO deleted_messages (mailbox_id, uid, message_id, deleted_at)
		VALUES (?, ?, ?, ?)`, mailboxID, 44, "<old@example.com>", time.Now().Unix()); err != nil {
		t.Fatal(err)
	}

	deleted, err := database.MessageDeletedLocally(mailboxID, 44, "<new@example.com>")
	if err != nil {
		t.Fatal(err)
	}
	if deleted {
		t.Fatal("same UID with different message-id should not be treated as locally deleted")
	}
	deleted, err = database.MessageDeletedLocally(mailboxID, 99, "<old@example.com>")
	if err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal("same message-id should be treated as locally deleted")
	}
}

func TestMessageDeletedLocallyFallsBackToUIDOnlyTombstones(t *testing.T) {
	tmp := t.TempDir()
	database, err := openSQLite(filepath.Join(tmp, "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if err := database.init(); err != nil {
		t.Fatal(err)
	}
	accountID, err := database.AddAccount("Personal", "")
	if err != nil {
		t.Fatal(err)
	}
	mailboxID, err := database.UpsertMailbox(Mailbox{AccountID: accountID, Name: "INBOX"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := database.Exec(`
		INSERT INTO deleted_messages (mailbox_id, uid, message_id, deleted_at)
		VALUES (?, ?, '', ?)`, mailboxID, 44, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}

	deleted, err := database.MessageDeletedLocally(mailboxID, 44, "")
	if err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal("UID-only tombstone should match UID-only fetched message")
	}
}

func TestPruneOldDeletedMessageTombstones(t *testing.T) {
	tmp := t.TempDir()
	database, err := openSQLite(filepath.Join(tmp, "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if err := database.init(); err != nil {
		t.Fatal(err)
	}
	accountID, err := database.AddAccount("Personal", "")
	if err != nil {
		t.Fatal(err)
	}
	mailboxID, err := database.UpsertMailbox(Mailbox{AccountID: accountID, Name: "INBOX"})
	if err != nil {
		t.Fatal(err)
	}

	old := time.Now().Add(-91 * 24 * time.Hour).Unix()
	recent := time.Now().Add(-89 * 24 * time.Hour).Unix()
	if _, err := database.Exec(`
		INSERT INTO deleted_messages (mailbox_id, uid, message_id, deleted_at)
		VALUES (?, 1, '<old@example.com>', ?), (?, 2, '<recent@example.com>', ?)`,
		mailboxID, old, mailboxID, recent); err != nil {
		t.Fatal(err)
	}

	if err := database.PruneDeletedMessageTombstones(); err != nil {
		t.Fatal(err)
	}
	var oldCount, recentCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM deleted_messages WHERE message_id = '<old@example.com>'`).Scan(&oldCount); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM deleted_messages WHERE message_id = '<recent@example.com>'`).Scan(&recentCount); err != nil {
		t.Fatal(err)
	}
	if oldCount != 0 || recentCount != 1 {
		t.Fatalf("expected old pruned and recent kept, got old=%d recent=%d", oldCount, recentCount)
	}
}

func TestDBFindArchiveMailboxPrefersSpecialUseThenCommonNames(t *testing.T) {
	tmp := t.TempDir()
	database, err := openSQLite(filepath.Join(tmp, "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if err := database.init(); err != nil {
		t.Fatal(err)
	}

	accountID, err := database.AddAccount("Personal", "")
	if err != nil {
		t.Fatal(err)
	}
	allMailID, err := database.UpsertMailbox(Mailbox{AccountID: accountID, Name: "[Gmail]/All Mail"})
	if err != nil {
		t.Fatal(err)
	}
	archiveID, err := database.UpsertMailbox(Mailbox{AccountID: accountID, Name: "Old Mail", Flags: []string{"\\Archive"}})
	if err != nil {
		t.Fatal(err)
	}

	got, err := database.FindArchiveMailbox(accountID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != archiveID {
		t.Fatalf("expected special-use archive %d, got %+v", archiveID, got)
	}

	if err := database.DeleteMailbox(archiveID); err != nil {
		t.Fatal(err)
	}
	got, err = database.FindArchiveMailbox(accountID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != allMailID {
		t.Fatalf("expected common all-mail archive %d, got %+v", allMailID, got)
	}
}

func TestDBFindArchiveMailboxErrorsWhenUnavailable(t *testing.T) {
	tmp := t.TempDir()
	database, err := openSQLite(filepath.Join(tmp, "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if err := database.init(); err != nil {
		t.Fatal(err)
	}
	accountID, err := database.AddAccount("Personal", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.UpsertMailbox(Mailbox{AccountID: accountID, Name: "INBOX"}); err != nil {
		t.Fatal(err)
	}

	if _, err := database.FindArchiveMailbox(accountID); err == nil {
		t.Fatal("expected missing archive mailbox error")
	}
}

func TestDBFindTrashMailboxPrefersSpecialUseThenCommonNames(t *testing.T) {
	tmp := t.TempDir()
	database, err := openSQLite(filepath.Join(tmp, "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if err := database.init(); err != nil {
		t.Fatal(err)
	}

	accountID, err := database.AddAccount("Personal", "")
	if err != nil {
		t.Fatal(err)
	}
	gmailTrashID, err := database.UpsertMailbox(Mailbox{AccountID: accountID, Name: "[Gmail]/Trash"})
	if err != nil {
		t.Fatal(err)
	}
	trashID, err := database.UpsertMailbox(Mailbox{AccountID: accountID, Name: "Deleted Items", Flags: []string{"\\Trash"}})
	if err != nil {
		t.Fatal(err)
	}

	got, err := database.FindTrashMailbox(accountID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != trashID {
		t.Fatalf("expected special-use trash %d, got %+v", trashID, got)
	}

	if err := database.DeleteMailbox(trashID); err != nil {
		t.Fatal(err)
	}
	got, err = database.FindTrashMailbox(accountID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != gmailTrashID {
		t.Fatalf("expected common Gmail trash %d, got %+v", gmailTrashID, got)
	}
}

func TestDBFindTrashMailboxErrorsWhenUnavailable(t *testing.T) {
	tmp := t.TempDir()
	database, err := openSQLite(filepath.Join(tmp, "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if err := database.init(); err != nil {
		t.Fatal(err)
	}
	accountID, err := database.AddAccount("Personal", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.UpsertMailbox(Mailbox{AccountID: accountID, Name: "INBOX"}); err != nil {
		t.Fatal(err)
	}

	if _, err := database.FindTrashMailbox(accountID); err == nil {
		t.Fatal("expected missing trash mailbox error")
	}
}

func openSQLite(path string) (*DB, error) {
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(1)
	return &DB{conn}, nil
}
