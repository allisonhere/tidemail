package db

import (
	"path/filepath"
	"testing"
)

func newConfigIDTestDB(t *testing.T) *DB {
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

func configIDOf(t *testing.T, database *DB, id int64) string {
	t.Helper()
	acc, err := database.GetAccount(id)
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	return acc.ConfigID
}

// Rows written before config_id existed are adopted by matching the display
// name, so an upgrade does not re-import every account as a duplicate.
func TestMigrateAccountConfigIDsAdoptsLegacyRows(t *testing.T) {
	database := newConfigIDTestDB(t)
	personal, err := database.AddAccount("", "Personal", "")
	if err != nil {
		t.Fatal(err)
	}
	work, err := database.AddAccount("", "Work", "")
	if err != nil {
		t.Fatal(err)
	}
	err = database.MigrateAccountConfigIDs([]AccountLink{
		{ConfigID: "cfg-personal", Name: "Personal"},
		{ConfigID: "cfg-work", Name: "Work"},
	})
	if err != nil {
		t.Fatalf("MigrateAccountConfigIDs: %v", err)
	}
	if got := configIDOf(t, database, personal); got != "cfg-personal" {
		t.Fatalf("Personal config_id = %q", got)
	}
	if got := configIDOf(t, database, work); got != "cfg-work" {
		t.Fatalf("Work config_id = %q", got)
	}
	// Re-running must not disturb an already-linked row.
	if err := database.MigrateAccountConfigIDs([]AccountLink{{ConfigID: "cfg-other", Name: "Personal"}}); err != nil {
		t.Fatalf("second migration: %v", err)
	}
	if got := configIDOf(t, database, personal); got != "cfg-personal" {
		t.Fatalf("an already-linked row was relinked to %q", got)
	}
}

// An ambiguous display name must be left unlinked rather than guessed: a wrong
// link would sync one account with another account's credentials, which is the
// exact failure this column exists to prevent.
func TestMigrateAccountConfigIDsLeavesAmbiguousNamesUnlinked(t *testing.T) {
	database := newConfigIDTestDB(t)
	first, err := database.AddAccount("", "David Blangstrup", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := database.AddAccount("", "David Blangstrup", "")
	if err != nil {
		t.Fatal(err)
	}
	err = database.MigrateAccountConfigIDs([]AccountLink{
		{ConfigID: "cfg-giga", Name: "David Blangstrup"},
		{ConfigID: "cfg-gmail", Name: "David Blangstrup"},
	})
	if err != nil {
		t.Fatalf("MigrateAccountConfigIDs: %v", err)
	}
	if got := configIDOf(t, database, first); got != "" {
		t.Fatalf("ambiguous row was linked to %q", got)
	}
	if got := configIDOf(t, database, second); got != "" {
		t.Fatalf("ambiguous row was linked to %q", got)
	}
}

// Drafts keep working across a rename once they carry the config ID, and drafts
// written before the column existed are still found by the (name, user) pair.
func TestDraftsFollowTheAccountAcrossARename(t *testing.T) {
	database := newConfigIDTestDB(t)
	accountID, err := database.AddAccount("cfg-personal", "Personal", "")
	if err != nil {
		t.Fatal(err)
	}
	mailboxID, err := database.UpsertMailbox(Mailbox{AccountID: accountID, Name: "Drafts"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.SaveDraft(Draft{
		AccountConfigID: "cfg-personal", AccountName: "Personal", AccountUser: "me@example.com",
		MailboxID: mailboxID, Subject: "hello",
	}); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	// The account is renamed; the draft must still be listed under its ID.
	drafts, err := database.ListDrafts("cfg-personal", "Renamed", "me@example.com")
	if err != nil {
		t.Fatalf("ListDrafts: %v", err)
	}
	if len(drafts) != 1 || drafts[0].Subject != "hello" {
		t.Fatalf("draft lost after rename: %#v", drafts)
	}

	// A pre-migration row (no config ID) is still reachable by name and user,
	// and the backfill then stamps it.
	if _, err := database.Exec(`INSERT INTO drafts
		(account_config_id, account_name, account_user, account_index, mailbox_id, subject, created_at, updated_at)
		VALUES ('', 'Personal', 'me@example.com', 0, ?, 'legacy', 1, 1)`, mailboxID); err != nil {
		t.Fatal(err)
	}
	legacy, err := database.ListDrafts("cfg-nothing", "Personal", "me@example.com")
	if err != nil {
		t.Fatalf("ListDrafts: %v", err)
	}
	if len(legacy) != 1 || legacy[0].Subject != "legacy" {
		t.Fatalf("legacy draft not reachable by name/user: %#v", legacy)
	}
	if err := database.MigrateDraftAccountConfigIDs([]DraftAccountLink{
		{ConfigID: "cfg-personal", Name: "Personal", User: "me@example.com"},
	}); err != nil {
		t.Fatalf("MigrateDraftAccountConfigIDs: %v", err)
	}
	after, err := database.ListDrafts("cfg-personal", "Renamed", "me@example.com")
	if err != nil {
		t.Fatalf("ListDrafts: %v", err)
	}
	if len(after) != 2 {
		t.Fatalf("expected both drafts after the backfill, got %#v", after)
	}
}
