package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
)

func TestImportRemoteDraftsCmdMirrorsAndDedupes(t *testing.T) {
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	accountID, err := database.AddAccount("Acct", "")
	if err != nil {
		t.Fatal(err)
	}
	mailboxID, err := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "Drafts", Flags: []string{"\\Drafts"}})
	if err != nil {
		t.Fatal(err)
	}
	date := time.Unix(1710000000, 0)
	if err := database.UpsertMessage(db.Message{
		MailboxID: mailboxID, UID: 1, MessageID: "<plain@x>",
		To: "bob@x", Subject: "Plain draft", BodyText: "plain body", Date: date,
	}); err != nil {
		t.Fatal(err)
	}
	if err := database.UpsertMessage(db.Message{
		MailboxID: mailboxID, UID: 2, MessageID: "<html@x>",
		Subject: "HTML draft", BodyHTML: "<p>Hello <b>world</b></p>", Date: date,
	}); err != nil {
		t.Fatal(err)
	}

	m := NewModel(database, config.DefaultConfig(), "dev", false)
	m.accounts = []db.Account{{ID: accountID, Name: "Acct"}}
	m.mailboxes = []db.Mailbox{{ID: mailboxID, AccountID: accountID, Name: "Drafts", Flags: []string{"\\Drafts"}}}

	result := m.importRemoteDraftsCmd(mailboxID)()
	loaded, ok := result.(DraftsLoadedMsg)
	if !ok {
		t.Fatalf("expected DraftsLoadedMsg, got %T", result)
	}
	if loaded.Err != nil {
		t.Fatal(loaded.Err)
	}
	if len(loaded.Drafts) != 2 {
		t.Fatalf("expected 2 mirrored drafts, got %d", len(loaded.Drafts))
	}
	byUID := map[uint32]db.Draft{}
	for _, d := range loaded.Drafts {
		byUID[d.RemoteUID] = d
	}
	plain := byUID[1]
	if plain.To != "bob@x" || plain.Subject != "Plain draft" || plain.BodyText != "plain body" {
		t.Fatalf("plain draft not mirrored faithfully: %+v", plain)
	}
	if plain.RemoteMessageID != "<plain@x>" || plain.MailboxID != mailboxID {
		t.Fatalf("plain draft missing remote linkage: %+v", plain)
	}
	html := byUID[2]
	if !strings.Contains(html.BodyText, "Hello") || !strings.Contains(html.BodyText, "**world**") {
		t.Fatalf("expected HTML-only draft converted to editable text, got %q", html.BodyText)
	}

	// Second import must not duplicate the mirrored drafts.
	result = m.importRemoteDraftsCmd(mailboxID)()
	loaded = result.(DraftsLoadedMsg)
	if loaded.Err != nil {
		t.Fatal(loaded.Err)
	}
	if len(loaded.Drafts) != 2 {
		t.Fatalf("expected import to be idempotent, got %d drafts", len(loaded.Drafts))
	}

	// Sidebar count: 2 mirrored locals, 0 unmirrored remote — not 4.
	if n := m.draftsSidebarCount(m.mailboxes[0]); n != 2 {
		t.Fatalf("expected sidebar count 2, got %d", n)
	}
}
