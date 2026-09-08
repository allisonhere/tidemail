package db

import (
	"bytes"
	"testing"
)

func TestAttachmentContentIDRoundTripAndMigration(t *testing.T) {
	d := newTestDB(t)
	account, err := d.AddAccount("Images", "")
	if err != nil {
		t.Fatal(err)
	}
	mailbox, err := d.UpsertMailbox(Mailbox{AccountID: account, Name: "INBOX"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.UpsertMessage(Message{MailboxID: mailbox, UID: 1, Subject: "Image"}); err != nil {
		t.Fatal(err)
	}
	var message int64
	if err := d.QueryRow(`SELECT id FROM messages WHERE mailbox_id=?`, mailbox).Scan(&message); err != nil {
		t.Fatal(err)
	}
	// Recreate the previous schema with a real cached attachment, then migrate.
	if _, err := d.Exec(`ALTER TABLE attachments DROP COLUMN content_id`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO attachments(message_id,filename,content_type,data,size) VALUES(?, 'old.png','image/png',?,3)`, message, []byte("old")); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := d.migrateAttachmentContentID(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.SaveAttachment(message, Attachment{Filename: "new.png", ContentType: "image/png", ContentID: "chart@mail", Data: []byte("new")}); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetAttachments(message)
	if err != nil || len(got) != 2 {
		t.Fatalf("attachments: %+v %v", got, err)
	}
	if got[0].ContentID != "" || !bytes.Equal(got[0].Data, []byte("old")) || got[1].ContentID != "chart@mail" || got[1].Size != 3 {
		t.Fatalf("migration/roundtrip: %+v", got)
	}
}
