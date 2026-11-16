package db

import "testing"

func TestDecodeFilename(t *testing.T) {
	for in, want := range map[string]string{
		"=?UTF-8?B?5oql5ZGKLnBkZg==?=": "报告.pdf",
		"=?GB2312?B?sqnQxy5wZGY=?=":    "博星.pdf",
		"report.pdf":                   "report.pdf",
		"报告.pdf":                       "报告.pdf",
		"=?bogus":                      "=?bogus",
	} {
		if got := DecodeFilename(in); got != want {
			t.Errorf("DecodeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCachedAttachmentFilenamesDecodeOnRead(t *testing.T) {
	d := newTestDB(t)
	acct, err := d.AddAccount("", "A", "")
	if err != nil {
		t.Fatal(err)
	}
	box, err := d.UpsertMailbox(Mailbox{AccountID: acct, Name: "INBOX"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.UpsertMessage(Message{MailboxID: box, UID: 1, Subject: "old", BodyText: "b", HasAttachment: true}); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := d.QueryRow(`SELECT id FROM messages WHERE mailbox_id = ? AND uid = 1`, box).Scan(&id); err != nil {
		t.Fatal(err)
	}
	raw := "=?UTF-8?B?5oql5ZGKLnBkZg==?="
	data := []byte("cached image")
	// Simulate a filename stored by an earlier parser, without changing the schema.
	if _, err := d.SaveAttachment(id, Attachment{Filename: raw, ContentType: "image/png", Data: data}); err != nil {
		t.Fatal(err)
	}
	for name, read := range map[string]func(int64) ([]Attachment, error){
		"metadata":    d.GetAttachmentsMeta,
		"full":        d.GetAttachments,
		"image parts": d.GetImageParts,
	} {
		t.Run(name, func(t *testing.T) {
			atts, err := read(id)
			if err != nil {
				t.Fatal(err)
			}
			if len(atts) != 1 || atts[0].Filename != "报告.pdf" {
				t.Fatalf("attachments = %#v", atts)
			}
			if name != "metadata" && string(atts[0].Data) != string(data) {
				t.Fatal("attachment bytes changed")
			}
		})
	}
	var stored string
	if err := d.QueryRow(`SELECT filename FROM attachments WHERE message_id = ?`, id).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != raw {
		t.Fatal("reading should not rewrite cached data")
	}
}
