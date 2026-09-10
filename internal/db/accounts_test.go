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
	accountID, err := database.AddAccount("Personal", "")
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
