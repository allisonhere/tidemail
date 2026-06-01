package db

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	database, err := openSQLite(filepath.Join(t.TempDir(), "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := database.init(); err != nil {
		t.Fatal(err)
	}
	return database
}

func TestAddContactUpsertAndNormalize(t *testing.T) {
	d := newTestDB(t)
	id, err := d.AddContact("Bob <BOB@Example.com>", "", "manual")
	if err != nil || id == 0 {
		t.Fatalf("AddContact: id=%d err=%v", id, err)
	}
	got, err := d.ListContacts()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Addr != "bob@example.com" || got[0].DisplayName != "Bob" || got[0].Source != "manual" {
		t.Fatalf("expected normalized bob, got %+v", got)
	}

	again, err := d.AddContact("bob@example.com", "Bobby", "vcard")
	if err != nil {
		t.Fatal(err)
	}
	if again != id {
		t.Fatalf("expected upsert to return same id %d, got %d", id, again)
	}
	got, _ = d.ListContacts()
	if len(got) != 1 || got[0].DisplayName != "Bobby" || got[0].Source != "vcard" {
		t.Fatalf("expected single updated row, got %+v", got)
	}
}

func TestListContactsOrderAndContactAddressesFormat(t *testing.T) {
	d := newTestDB(t)
	_, _ = d.AddContact("zed@example.com", "", "manual")
	_, _ = d.AddContact("Amy <amy@example.com>", "", "manual")
	_, _ = d.AddContact("bob@example.com", "Bob", "manual")

	got, err := d.ListContacts()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 contacts, got %+v", got)
	}
	if got[0].Addr != "amy@example.com" || got[1].Addr != "bob@example.com" || got[2].Addr != "zed@example.com" {
		t.Fatalf("unexpected order: %+v", got)
	}

	addrs, err := d.ContactAddresses()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Amy <amy@example.com>", "Bob <bob@example.com>", "zed@example.com"}
	for i := range want {
		if addrs[i] != want[i] {
			t.Fatalf("addresses[%d] = %q, want %q; all=%v", i, addrs[i], want[i], addrs)
		}
	}
}

func TestUpdateAndDeleteContact(t *testing.T) {
	d := newTestDB(t)
	id, _ := d.AddContact("temp@example.com", "Temp", "manual")
	if err := d.UpdateContact(id, "Perm <PERM@example.com>", ""); err != nil {
		t.Fatal(err)
	}
	got, _ := d.ListContacts()
	if len(got) != 1 || got[0].Addr != "perm@example.com" || got[0].DisplayName != "Perm" {
		t.Fatalf("update failed: %+v", got)
	}
	if err := d.DeleteContact(id); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.ListContacts(); len(got) != 0 {
		t.Fatalf("delete failed: %+v", got)
	}
}

func TestContactMetadataPersistsThroughAddAndUpdate(t *testing.T) {
	d := newTestDB(t)
	id, err := d.AddContactWithMetadata("alex@example.com", "Alex", "manual", ContactMetadata{
		Phone:        "555-0100",
		Organization: "Example Co",
		Title:        "Engineer",
		Note:         "Met at conf",
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := d.ListContacts()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Phone != "555-0100" || got[0].Organization != "Example Co" || got[0].Title != "Engineer" || got[0].Note != "Met at conf" {
		t.Fatalf("metadata not saved: %+v", got)
	}

	if err := d.UpdateContactWithMetadata(id, "Alex <alex@example.com>", "Alex Updated", ContactMetadata{
		Phone:        "555-0101",
		Organization: "New Co",
		Title:        "Lead",
		Note:         "Updated note",
	}); err != nil {
		t.Fatal(err)
	}
	got, _ = d.ListContacts()
	if len(got) != 1 || got[0].DisplayName != "Alex Updated" || got[0].Phone != "555-0101" || got[0].Organization != "New Co" || got[0].Title != "Lead" || got[0].Note != "Updated note" {
		t.Fatalf("metadata not updated: %+v", got)
	}
}

func TestSeenAddressesDedupesAndExcludesContacts(t *testing.T) {
	d := newTestDB(t)
	accountID, _ := d.AddAccount("Acct", "")
	mailboxID, _ := d.UpsertMailbox(Mailbox{AccountID: accountID, Name: "INBOX"})
	_ = d.UpsertMessage(Message{
		MailboxID: mailboxID,
		UID:       1,
		From:      "Carol <CAROL@example.com>",
		To:        "Alice <alice@example.com>, bob@example.com",
		CC:        "carol@example.com",
	})
	_ = d.UpsertMessage(Message{
		MailboxID: mailboxID,
		UID:       2,
		From:      "Bob <BOB@example.com>",
		To:        "dave@example.com",
	})
	_, _ = d.AddContact("alice@example.com", "Alice", "manual")

	seen, err := d.SeenAddresses()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range seen {
		got = append(got, c.Addr+"|"+c.DisplayName)
	}
	want := map[string]bool{
		"bob@example.com|Bob":     true,
		"carol@example.com|Carol": true,
		"dave@example.com|":       true,
	}
	if len(got) != len(want) {
		t.Fatalf("unexpected seen addresses: %v", got)
	}
	for _, item := range got {
		if !want[item] {
			t.Fatalf("unexpected seen address %q in %v", item, got)
		}
	}
}

func TestInitMigratesLegacyAutoContactsToCuratedSchema(t *testing.T) {
	database, err := openSQLite(filepath.Join(t.TempDir(), "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })

	if _, err := database.Exec(`
		CREATE TABLE contacts (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			addr         TEXT    NOT NULL UNIQUE,
			display_name TEXT    NOT NULL DEFAULT '',
			status       TEXT    NOT NULL DEFAULT 'pending',
			source       TEXT    NOT NULL DEFAULT 'auto',
			use_count    INTEGER NOT NULL DEFAULT 0,
			last_used    INTEGER NOT NULL DEFAULT 0,
			created_at   INTEGER NOT NULL DEFAULT 0
		);
		INSERT INTO contacts (addr, display_name, status, source, created_at) VALUES
			('auto@example.com', 'Auto', 'pending', 'auto', 100),
			('manual@example.com', 'Manual', 'approved', 'manual', 200),
			('vcard@example.com', 'VCard', 'approved', 'vcard', 300);
	`); err != nil {
		t.Fatal(err)
	}

	if err := database.init(); err != nil {
		t.Fatal(err)
	}

	cols := contactTestColumns(t, database)
	for _, legacy := range []string{"status", "use_count", "last_used"} {
		if cols[legacy] {
			t.Fatalf("legacy column %q still exists after migration: %v", legacy, cols)
		}
	}
	for _, added := range []string{"phone", "organization", "title", "note"} {
		if !cols[added] {
			t.Fatalf("metadata column %q missing after migration: %v", added, cols)
		}
	}

	got, err := database.ListContacts()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected only curated contacts, got %+v", got)
	}
	for _, c := range got {
		if c.Addr == "auto@example.com" {
			t.Fatalf("auto-captured contact survived migration: %+v", got)
		}
	}
}

func contactTestColumns(t *testing.T, database *DB) map[string]bool {
	t.Helper()
	rows, err := database.Query(`PRAGMA table_info(contacts)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatal(err)
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return cols
}
