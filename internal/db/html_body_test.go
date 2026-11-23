package db

import (
	"context"
	"path/filepath"
	"testing"
)

func TestHTMLFallbackIndexAndMigration(t *testing.T) {
	d, err := openSQLite(filepath.Join(t.TempDir(), "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err = d.init(); err != nil {
		t.Fatal(err)
	}
	a, err := d.AddAccount("test", "Test", "")
	if err != nil {
		t.Fatal(err)
	}
	mb, err := d.UpsertMailbox(Mailbox{AccountID: a, Name: "INBOX"})
	if err != nil {
		t.Fatal(err)
	}
	if err = d.UpsertMessage(Message{MailboxID: mb, UID: 1, BodyText: " \u00a0", BodyHTML: "<p>htmlneedle <a href='https://destinationneedle.invalid'>labelneedle</a></p><style>stylenoise</style>"}); err != nil {
		t.Fatal(err)
	}
	if err = d.UpsertMessage(Message{MailboxID: mb, UID: 2, BodyText: "plainneedle", BodyHTML: "<p>ignoredneedle</p>"}); err != nil {
		t.Fatal(err)
	}
	check := func() {
		t.Helper()
		for term, want := range map[string]int{"htmlneedle": 1, "labelneedle": 1, "destinationneedle": 0, "stylenoise": 0, "plainneedle": 1, "ignoredneedle": 0} {
			_, total, e := d.SearchAllMessagesPage(term, false, 10, 0)
			if e != nil || total != want {
				t.Fatalf("%s: total %d want %d err %v", term, total, want, e)
			}
		}
	}
	check()
	// Simulate an old index; migration must populate fallback and preserve bodies.
	if _, err = d.Exec("UPDATE messages_fts SET body_text=''"); err != nil {
		t.Fatal(err)
	}
	if err = d.SetSetting("fts_schema_version", "1"); err != nil {
		t.Fatal(err)
	}
	if err = d.migrateMessageFTS(); err != nil {
		t.Fatal(err)
	}
	check()
	var plain string
	if err = d.QueryRow("SELECT body_text FROM messages WHERE uid=1").Scan(&plain); err != nil || plain != " \u00a0" {
		t.Fatalf("stored plain changed: %q %v", plain, err)
	}
	if err = d.migrateMessageFTS(); err != nil {
		t.Fatal(err)
	}
	check()
}

func TestLargeCacheIndexRebuildRunsInBackgroundAndResumes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mail.db")
	d, err := OpenPath(path)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := d.AddAccount("test", "Test", "")
	mb, _ := d.UpsertMailbox(Mailbox{AccountID: a, Name: "INBOX"})
	const total = ftsSyncLimit + ftsBatchSize + 10
	tx, err := d.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= total; i++ {
		if _, err = tx.Exec(`INSERT INTO messages(mailbox_id, uid, subject, body_html) VALUES(?, ?, 's', '<p>bgneedle</p>')`, mb, i); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = d.Exec("DELETE FROM messages_fts"); err != nil {
		t.Fatal(err)
	}
	if err = d.SetSetting("fts_schema_version", "1"); err != nil {
		t.Fatal(err)
	}
	d.Close()

	d, err = OpenPath(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if d.stopFTS == nil {
		t.Fatal("large cache must be rebuilt in the background, not while opening")
	}
	d.stopFTS()
	<-d.ftsDone
	d.stopFTS, d.ftsDone = nil, nil
	// A cancelled run keeps its progress; finishing it completes the index.
	if err = d.rebuildMessageFTS(context.Background()); err != nil {
		t.Fatal(err)
	}
	if v, _ := d.GetSetting("fts_schema_version"); v != ftsSchemaVersion {
		t.Fatalf("token %q after rebuild", v)
	}
	if _, n, err := d.SearchAllMessagesPage("bgneedle", false, 10, 0); err != nil || n != total {
		t.Fatalf("total %d err %v", n, err)
	}
	if v, _ := d.GetSetting(ftsCursorKey); v != "" {
		t.Fatalf("cursor left behind: %q", v)
	}
}
