package ui

import (
	"testing"

	"github.com/allisonhere/tidemail/internal/db"
)

func TestQualifyFolderName(t *testing.T) {
	dovecot := []db.Mailbox{
		{Name: "INBOX", Delimiter: "."},
		{Name: "INBOX.Sent", Delimiter: "."},
		{Name: "INBOX.Archive", Delimiter: "."},
	}
	gmail := []db.Mailbox{
		{Name: "INBOX", Delimiter: "/"},
		{Name: "Receipts", Delimiter: "/"},
		{Name: "[Gmail]/All Mail", Delimiter: "/"},
	}

	tests := []struct {
		name      string
		mailboxes []db.Mailbox
		input     string
		wantName  string
		wantDelim string
	}{
		{"dovecot qualifies", dovecot, "Newsletters", "INBOX.Newsletters", "."},
		{"dovecot leaves qualified names alone", dovecot, "INBOX.Newsletters", "INBOX.Newsletters", "."},
		{"gmail stays top level", gmail, "Newsletters", "Newsletters", "/"},
		{"inbox only stays top level", []db.Mailbox{{Name: "INBOX", Delimiter: "/"}}, "Newsletters", "Newsletters", "/"},
		{"no mailboxes defaults to slash", nil, "Newsletters", "Newsletters", "/"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotName, gotDelim := qualifyFolderName(tc.input, tc.mailboxes)
			if gotName != tc.wantName || gotDelim != tc.wantDelim {
				t.Fatalf("qualifyFolderName(%q) = %q, %q; want %q, %q",
					tc.input, gotName, gotDelim, tc.wantName, tc.wantDelim)
			}
		})
	}
}

func TestAccountMailboxes(t *testing.T) {
	all := []db.Mailbox{
		{AccountID: 1, Name: "INBOX"},
		{AccountID: 2, Name: "INBOX"},
		{AccountID: 1, Name: "INBOX.Sent"},
	}
	got := accountMailboxes(all, 1)
	if len(got) != 2 || got[1].Name != "INBOX.Sent" {
		t.Fatalf("accountMailboxes(1) = %+v", got)
	}
}
