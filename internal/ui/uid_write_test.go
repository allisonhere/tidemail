package ui

import (
	"context"
	"testing"

	"github.com/allisonhere/tidemail/internal/db"
)

func TestUIDWriteContextCarriesTheCachedUIDValidity(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	accountID, err := database.AddAccount("", "Personal", "")
	if err != nil {
		t.Fatal(err)
	}
	mailboxID, err := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "INBOX"})
	if err != nil {
		t.Fatal(err)
	}

	base := context.Background()
	// Never synced: nothing to compare against, so writes run unchecked as before.
	if ctx := uidWriteContext(base, database, mailboxID, "INBOX"); ctx != base {
		t.Fatal("a mailbox without a stored UIDVALIDITY must not add a check")
	}
	if err := database.SetMailboxUIDValidity(mailboxID, 42); err != nil {
		t.Fatal(err)
	}
	if ctx := uidWriteContext(base, database, mailboxID, "INBOX"); ctx == base {
		t.Fatal("a synced mailbox must carry its UIDVALIDITY to the write")
	}
	if ctx := uidWriteContext(base, nil, mailboxID, "INBOX"); ctx != base {
		t.Fatal("no database means no check")
	}
}
