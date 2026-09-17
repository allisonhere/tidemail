package db

import (
	"path/filepath"
	"testing"
)

func newSentTestDB(t *testing.T) (*DB, int64) {
	t.Helper()
	database, err := openSQLite(filepath.Join(t.TempDir(), "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := database.init(); err != nil {
		t.Fatal(err)
	}
	accountID, err := database.AddAccount("Personal", "")
	if err != nil {
		t.Fatal(err)
	}
	return database, accountID
}

// The \Sent special-use flag is authoritative, whatever the folder is called.
func TestFindSentMailboxPrefersSpecialUseFlag(t *testing.T) {
	database, accountID := newSentTestDB(t)
	for _, mb := range []Mailbox{
		{AccountID: accountID, Name: "INBOX"},
		{AccountID: accountID, Name: "Postausgang", Flags: []string{`\Sent`}},
		{AccountID: accountID, Name: "Sent"},
	} {
		if _, err := database.UpsertMailbox(mb); err != nil {
			t.Fatal(err)
		}
	}

	got, err := database.FindSentMailbox(accountID)
	if err != nil {
		t.Fatalf("FindSentMailbox: %v", err)
	}
	if got.Name != "Postausgang" {
		t.Fatalf("got %q, want the \\Sent-flagged mailbox", got.Name)
	}
}

// Servers that advertise no special-use still have to be matched by name, and
// the two hierarchy delimiters produce very different strings.
func TestFindSentMailboxMatchesCommonNames(t *testing.T) {
	for _, name := range []string{
		"Sent", "sent", "Sent Items", "Sent Mail", "Sent Messages",
		"[Gmail]/Sent Mail", "INBOX.Sent", "INBOX.Sent Items",
	} {
		t.Run(name, func(t *testing.T) {
			database, accountID := newSentTestDB(t)
			if _, err := database.UpsertMailbox(Mailbox{AccountID: accountID, Name: "INBOX"}); err != nil {
				t.Fatal(err)
			}
			if _, err := database.UpsertMailbox(Mailbox{AccountID: accountID, Name: name}); err != nil {
				t.Fatal(err)
			}

			got, err := database.FindSentMailbox(accountID)
			if err != nil {
				t.Fatalf("FindSentMailbox(%q): %v", name, err)
			}
			if got.Name != name {
				t.Fatalf("got %q, want %q", got.Name, name)
			}
		})
	}
}

// A folder that merely contains the word must not be mistaken for Sent.
func TestFindSentMailboxIgnoresUnrelatedNames(t *testing.T) {
	database, accountID := newSentTestDB(t)
	for _, name := range []string{"INBOX", "Unsent drafts", "Sentry alerts", "Presentations"} {
		if _, err := database.UpsertMailbox(Mailbox{AccountID: accountID, Name: name}); err != nil {
			t.Fatal(err)
		}
	}

	if got, err := database.FindSentMailbox(accountID); err == nil {
		t.Fatalf("expected no Sent mailbox, got %q", got.Name)
	}
}

// A mailbox stored before special-use flags were captured must be able to learn
// them later, or an account can never discover where Sent is.
func TestUpsertMailboxRefreshesFlags(t *testing.T) {
	database, accountID := newSentTestDB(t)
	if _, err := database.UpsertMailbox(Mailbox{AccountID: accountID, Name: "INBOX.Sent"}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.FindSentMailbox(accountID); err != nil {
		t.Fatalf("name match should already work: %v", err)
	}

	if _, err := database.UpsertMailbox(Mailbox{
		AccountID: accountID, Name: "INBOX.Sent", Delimiter: ".", Flags: []string{`\Sent`},
	}); err != nil {
		t.Fatal(err)
	}

	mailboxes, err := database.ListMailboxes(accountID)
	if err != nil {
		t.Fatal(err)
	}
	for _, mb := range mailboxes {
		if mb.Name != "INBOX.Sent" {
			continue
		}
		if len(mb.Flags) != 1 || mb.Flags[0] != `\Sent` {
			t.Fatalf("flags = %v, want [\\Sent] after re-upsert", mb.Flags)
		}
		if mb.Delimiter != "." {
			t.Fatalf("delimiter = %q, want .", mb.Delimiter)
		}
		return
	}
	t.Fatal("INBOX.Sent missing after upsert")
}

func TestAccountIDByName(t *testing.T) {
	database, accountID := newSentTestDB(t)
	got, err := database.AccountIDByName("Personal")
	if err != nil {
		t.Fatalf("AccountIDByName: %v", err)
	}
	if got != accountID {
		t.Fatalf("got %d, want %d", got, accountID)
	}
	if _, err := database.AccountIDByName("Nope"); err == nil {
		t.Fatal("expected an error for an unknown account")
	}
}
