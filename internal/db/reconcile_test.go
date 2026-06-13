package db

import (
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func openReconcileTestDB(t *testing.T) *DB {
	t.Helper()
	database, err := openSQLite(filepath.Join(t.TempDir(), "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.init(); err != nil {
		database.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func TestReconcileMailboxUIDsRemovesVanishedKeepsPresent(t *testing.T) {
	database := openReconcileTestDB(t)
	accountID, err := database.AddAccount("Acct", "")
	if err != nil {
		t.Fatal(err)
	}
	mailboxID, err := database.UpsertMailbox(Mailbox{AccountID: accountID, Name: "INBOX"})
	if err != nil {
		t.Fatal(err)
	}
	for _, uid := range []uint32{10, 20, 30} {
		if err := database.UpsertMessage(Message{MailboxID: mailboxID, UID: uid, Subject: "m"}); err != nil {
			t.Fatal(err)
		}
	}
	// A local-only draft-style row (uid 0) must never be reconciled away.
	if err := database.UpsertMessage(Message{MailboxID: mailboxID, UID: 0, MessageID: "<local@x>", Subject: "local"}); err != nil {
		t.Fatal(err)
	}
	// Attachment on a message that will vanish — must cascade-delete.
	if _, err := database.SaveAttachment(messageIDForUID(t, database, mailboxID, 20), Attachment{Filename: "a.txt", Data: []byte("x")}); err != nil {
		t.Fatal(err)
	}

	// Server now only has 10 and 30; 20 was archived elsewhere.
	removed, err := database.ReconcileMailboxUIDs(mailboxID, []uint32{10, 30})
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("expected 1 message removed, got %d", removed)
	}

	msgs, err := database.ListMessages(mailboxID)
	if err != nil {
		t.Fatal(err)
	}
	gotUIDs := map[uint32]bool{}
	for _, m := range msgs {
		gotUIDs[m.UID] = true
	}
	if gotUIDs[20] {
		t.Fatal("expected vanished UID 20 to be removed")
	}
	if !gotUIDs[10] || !gotUIDs[30] || !gotUIDs[0] {
		t.Fatalf("expected UIDs 10, 30 and local 0 to survive, got %v", gotUIDs)
	}

	var attCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM attachments`).Scan(&attCount); err != nil {
		t.Fatal(err)
	}
	if attCount != 0 {
		t.Fatalf("expected attachment of removed message to cascade-delete, got %d", attCount)
	}

	results, err := database.SearchAllMessages("local", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].UID != 0 {
		t.Fatalf("expected only local-only row to remain searchable, got %+v", results)
	}
}

func TestReconcileMailboxUIDsEmptyServerSetWipesUIDMessages(t *testing.T) {
	database := openReconcileTestDB(t)
	accountID, _ := database.AddAccount("Acct", "")
	mailboxID, _ := database.UpsertMailbox(Mailbox{AccountID: accountID, Name: "INBOX"})
	if err := database.UpsertMessage(Message{MailboxID: mailboxID, UID: 5, Subject: "gone"}); err != nil {
		t.Fatal(err)
	}
	removed, err := database.ReconcileMailboxUIDs(mailboxID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("expected the one UID message removed against an empty server set, got %d", removed)
	}
}

func TestApplyServerReadStatesAdoptsServerFlags(t *testing.T) {
	database := openReconcileTestDB(t)
	accountID, _ := database.AddAccount("Acct", "")
	mailboxID, _ := database.UpsertMailbox(Mailbox{AccountID: accountID, Name: "INBOX"})

	// uid1 unread locally, uid2 read locally, uid3 read locally (will stay read).
	if err := database.UpsertMessage(Message{MailboxID: mailboxID, UID: 1, Subject: "a", Read: false}); err != nil {
		t.Fatal(err)
	}
	if err := database.UpsertMessage(Message{MailboxID: mailboxID, UID: 2, Subject: "b", Read: true}); err != nil {
		t.Fatal(err)
	}
	if err := database.UpsertMessage(Message{MailboxID: mailboxID, UID: 3, Subject: "c", Read: true}); err != nil {
		t.Fatal(err)
	}
	// A local-only row must be ignored even if its (zero) UID appears.
	if err := database.UpsertMessage(Message{MailboxID: mailboxID, UID: 0, MessageID: "<local@x>", Read: false}); err != nil {
		t.Fatal(err)
	}

	// Server says: uid1 now read, uid2 now unread, uid3 still read.
	changed, err := database.ApplyServerReadStates(mailboxID, map[uint32]bool{1: true, 2: false, 3: true, 0: true})
	if err != nil {
		t.Fatal(err)
	}
	if changed != 2 {
		t.Fatalf("expected 2 rows changed (uid1, uid2), got %d", changed)
	}

	read := map[uint32]bool{}
	msgs, err := database.ListMessages(mailboxID)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range msgs {
		read[m.UID] = m.Read
	}
	if !read[1] {
		t.Fatal("expected uid1 to become read")
	}
	if read[2] {
		t.Fatal("expected uid2 to become unread")
	}
	if !read[3] {
		t.Fatal("expected uid3 to stay read")
	}
	if read[0] {
		t.Fatal("expected local-only uid 0 row to be untouched (stay unread)")
	}

	// Idempotent: applying the same states again changes nothing.
	changed, err = database.ApplyServerReadStates(mailboxID, map[uint32]bool{1: true, 2: false, 3: true})
	if err != nil {
		t.Fatal(err)
	}
	if changed != 0 {
		t.Fatalf("expected no changes on a second apply, got %d", changed)
	}
}

func TestUIDValidityRoundTripAndResetMailboxCache(t *testing.T) {
	database := openReconcileTestDB(t)
	accountID, _ := database.AddAccount("Acct", "")
	mailboxID, _ := database.UpsertMailbox(Mailbox{AccountID: accountID, Name: "INBOX"})

	if v, err := database.MailboxUIDValidity(mailboxID); err != nil || v != 0 {
		t.Fatalf("expected 0 initial uid_validity, got %d err=%v", v, err)
	}
	if err := database.SetMailboxUIDValidity(mailboxID, 4242); err != nil {
		t.Fatal(err)
	}
	if v, _ := database.MailboxUIDValidity(mailboxID); v != 4242 {
		t.Fatalf("expected 4242, got %d", v)
	}

	if err := database.UpsertMessage(Message{MailboxID: mailboxID, UID: 1, Subject: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := database.SetMailboxLastSynced(mailboxID, time.Unix(123456, 0)); err != nil {
		t.Fatal(err)
	}
	if err := database.ResetMailboxCache(mailboxID); err != nil {
		t.Fatal(err)
	}
	if n, _ := database.CountMessages(mailboxID); n != 0 {
		t.Fatalf("expected messages cleared, got %d", n)
	}
	mb, err := database.GetMailbox(mailboxID)
	if err != nil {
		t.Fatal(err)
	}
	if !mb.LastSynced.IsZero() {
		t.Fatalf("expected last_synced reset, got %v", mb.LastSynced)
	}
	results, err := database.SearchAllMessages("x", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("expected reset mailbox cache to clear FTS rows, got %+v", results)
	}
}

func messageIDForUID(t *testing.T, database *DB, mailboxID int64, uid uint32) int64 {
	t.Helper()
	var id int64
	if err := database.QueryRow(`SELECT id FROM messages WHERE mailbox_id = ? AND uid = ?`, mailboxID, uid).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
