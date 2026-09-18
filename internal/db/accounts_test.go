package db

import (
	"path/filepath"
	"testing"
)

func TestUpsertMailboxReturnsExistingID(t *testing.T) {
	database, err := openSQLite(filepath.Join(t.TempDir(), "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.init(); err != nil {
		t.Fatal(err)
	}
	accountID, err := database.AddAccount("", "Personal", "")
	if err != nil {
		t.Fatal(err)
	}
	inbox := Mailbox{AccountID: accountID, Name: "INBOX"}
	inboxID, err := database.UpsertMailbox(inbox)
	if err != nil {
		t.Fatal(err)
	}
	archiveID, err := database.UpsertMailbox(Mailbox{AccountID: accountID, Name: "Archive"})
	if err != nil {
		t.Fatal(err)
	}
	if inboxID == archiveID {
		t.Fatal("different mailboxes returned the same ID")
	}
	inbox.DisplayName = "Updated inbox"
	got, err := database.UpsertMailbox(inbox)
	if err != nil {
		t.Fatal(err)
	}
	if got != inboxID {
		t.Fatalf("updated INBOX ID = %d, want %d (Archive ID = %d)", got, inboxID, archiveID)
	}
	saved, err := database.GetMailbox(got)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Name != inbox.Name || saved.DisplayName != inbox.DisplayName {
		t.Fatalf("returned mailbox does not contain the update: %+v", saved)
	}
}

func TestSaveAccountWithMailboxesRollsBackPartialEdit(t *testing.T) {
	database, err := openSQLite(filepath.Join(t.TempDir(), "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.init(); err != nil {
		t.Fatal(err)
	}
	accountID, err := database.AddAccount("cfg-old", "Before", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TRIGGER fail_broken_mailbox BEFORE INSERT ON mailboxes WHEN NEW.name = 'Broken' BEGIN SELECT RAISE(FAIL, 'forced mailbox failure'); END`); err != nil {
		t.Fatal(err)
	}

	_, _, err = database.SaveAccountWithMailboxes(accountID, "cfg-new", "After", "#fff", []Mailbox{{Name: "Good"}, {Name: "Broken"}})
	if err == nil {
		t.Fatal("expected forced mailbox failure")
	}
	account, err := database.GetAccount(accountID)
	if err != nil {
		t.Fatal(err)
	}
	if account.Name != "Before" || account.ConfigID != "cfg-old" {
		t.Fatalf("account edit escaped rolled-back transaction: %#v", account)
	}
	mailboxes, err := database.ListMailboxes(accountID)
	if err != nil {
		t.Fatal(err)
	}
	if len(mailboxes) != 0 {
		t.Fatalf("partial mailbox set escaped rolled-back transaction: %#v", mailboxes)
	}
}
