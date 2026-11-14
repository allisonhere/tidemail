package db

import (
	"testing"
	"time"
)

// A failure while replacing attachments must roll the whole upsert back: the
// message keeps its old subject, FTS row and attachments, so the next sync can
// retry instead of finding a stored UID with half its attachments missing.
func TestUpsertMessageIsAtomicWithAttachments(t *testing.T) {
	database := newTestDB(t)
	accountID, _ := database.AddAccount("", "Work", "")
	mailboxID, _ := database.UpsertMailbox(Mailbox{AccountID: accountID, Name: "INBOX"})
	first := Message{
		MailboxID: mailboxID, UID: 1, Subject: "original", BodyText: "alpha", Date: time.Unix(1, 0),
		HasAttachment:  true,
		AttachmentData: []Attachment{{Filename: "keep.txt", ContentType: "text/plain", Data: []byte("x")}},
	}
	if err := database.UpsertMessage(first); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		CREATE TRIGGER fail_attachment BEFORE INSERT ON attachments
		WHEN NEW.filename = 'boom.txt'
		BEGIN SELECT RAISE(ABORT, 'boom'); END`); err != nil {
		t.Fatal(err)
	}

	second := first
	second.Subject = "changed"
	second.AttachmentData = []Attachment{
		{Filename: "new.txt", ContentType: "text/plain", Data: []byte("y")},
		{Filename: "boom.txt", ContentType: "text/plain", Data: []byte("z")},
	}
	if err := database.UpsertMessage(second); err == nil {
		t.Fatal("expected the failing attachment to fail the upsert")
	}

	var subject string
	if err := database.QueryRow(`SELECT subject FROM messages WHERE mailbox_id = ? AND uid = 1`, mailboxID).Scan(&subject); err != nil {
		t.Fatal(err)
	}
	if subject != "original" {
		t.Errorf("message row changed despite the failure: subject %q", subject)
	}
	var id int64
	if err := database.QueryRow(`SELECT id FROM messages WHERE mailbox_id = ? AND uid = 1`, mailboxID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	atts, err := database.GetAttachmentsMeta(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(atts) != 1 || atts[0].Filename != "keep.txt" {
		t.Errorf("old attachments were not preserved: %+v", atts)
	}
	var ftsHits int
	if err := database.QueryRow(`SELECT COUNT(*) FROM messages_fts WHERE messages_fts MATCH 'changed'`).Scan(&ftsHits); err != nil {
		t.Fatal(err)
	}
	if ftsHits != 0 {
		t.Errorf("FTS row was updated despite the failure")
	}
}
