package app

import (
	"testing"
	"time"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
)

func testService(t *testing.T, cfg config.Config) (*Service, *db.DB) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	service := New(database, cfg, nil)
	t.Cleanup(func() {
		service.Close()
		database.Close()
	})
	return service, database
}

func TestBootstrapImportsConfiguredAccountsAndInbox(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Accounts = []config.AccountConfig{{Name: "Personal", User: "me@example.com"}}
	service, _ := testService(t, cfg)

	got, err := service.Bootstrap()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Accounts) != 1 || got.Accounts[0].Name != "Personal" {
		t.Fatalf("accounts = %#v", got.Accounts)
	}
	if len(got.Mailboxes) != 1 || got.Mailboxes[0].Name != "INBOX" {
		t.Fatalf("mailboxes = %#v", got.Mailboxes)
	}
}

func TestListAndSearchMessages(t *testing.T) {
	service, database := testService(t, config.DefaultConfig())
	accountID, err := database.AddAccount("Personal", "")
	if err != nil {
		t.Fatal(err)
	}
	mailboxID, err := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "INBOX", Flags: []string{`\Inbox`}})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpsertMessage(db.Message{
		MailboxID: mailboxID, UID: 1, Subject: "Tide report", From: "Mira <mira@example.com>",
		BodyText: "The water is calm.", Date: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	page, err := service.ListMessages(mailboxID, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 1 || page.Messages[0].Subject != "Tide report" {
		t.Fatalf("messages = %#v", page.Messages)
	}
	results, err := service.Search("water")
	if err != nil {
		t.Fatal(err)
	}
	if len(results.Messages) != 1 || results.Messages[0].AccountName != "Personal" {
		t.Fatalf("search results = %#v", results.Messages)
	}
}

func TestSaveDraftEmitsChange(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	var event Event
	service := New(database, config.DefaultConfig(), func(got Event) { event = got })
	t.Cleanup(func() { service.Close(); database.Close() })

	id, err := service.SaveDraft(db.Draft{AccountName: "Personal", To: "mira@example.com", Subject: "Hello"})
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 || event.Name != "drafts.changed" {
		t.Fatalf("id = %d, event = %#v", id, event)
	}
}
