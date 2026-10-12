package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/db"
)

func mailReadFixture(t *testing.T) int64 {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	account, err := database.AddAccount("account-stable", "Personal", "")
	if err != nil {
		t.Fatal(err)
	}
	mailbox, err := database.UpsertMailbox(db.Mailbox{AccountID: account, Name: "INBOX"})
	if err != nil {
		t.Fatal(err)
	}
	if err = database.UpsertMessage(db.Message{MailboxID: mailbox, UID: 42, Subject: "Invoice", From: "sender@example.com", BodyText: "private invoice details", BodyHTML: "<p>private</p>", Summary: "private summary", Date: time.Unix(100, 0)}); err != nil {
		t.Fatal(err)
	}
	id, err := database.MessageIDByUID(mailbox, 42)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func TestMailReadsMetadataAndExplicitBody(t *testing.T) {
	id := mailReadFixture(t)
	var out, diagnostics bytes.Buffer
	if code := RunMailRead("messages", []string{"list", "--unread", "--json"}, &out, &diagnostics); code != 0 {
		t.Fatalf("%d: %s", code, diagnostics.String())
	}
	if strings.Contains(out.String(), "private") || strings.Contains(out.String(), "body_text") || strings.Contains(out.String(), "\x1b") {
		t.Fatalf("unsafe metadata: %s", out.String())
	}
	var envelope struct {
		SchemaVersion int  `json:"schema_version"`
		OK            bool `json:"ok"`
		Data          struct {
			Items []struct {
				ID        string `json:"id"`
				AccountID string `json:"account_id"`
			}
		}
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.SchemaVersion != 1 || !envelope.OK || len(envelope.Data.Items) != 1 || envelope.Data.Items[0].AccountID != "account-stable" {
		t.Fatalf("unexpected output: %s", out.String())
	}
	out.Reset()
	if code := RunMailRead("messages", []string{"read", fmt.Sprintf("cache:%d", id), "--json"}, &out, &diagnostics); code != 0 || !strings.Contains(out.String(), "private invoice details") {
		t.Fatalf("explicit read: %d %s", code, out.String())
	}
	out.Reset()
	if code := RunMailRead("messages", []string{"read", fmt.Sprint(id), "--agent", "--json"}, &out, &diagnostics); code != 3 || !strings.Contains(out.String(), "permission_denied") || strings.Contains(out.String(), "private") {
		t.Fatalf("agent read: %d %s", code, out.String())
	}
}
func TestMailReadsSearchAndPagination(t *testing.T) {
	mailReadFixture(t)
	var out, diagnostics bytes.Buffer
	if code := RunMailRead("messages", []string{"search", "invoice", "--agent", "--json", "--offset", "1"}, &out, &diagnostics); code != 0 || !strings.Contains(out.String(), `"items":[]`) || !strings.Contains(out.String(), `"total":1`) {
		t.Fatalf("search: %d %s %s", code, out.String(), diagnostics.String())
	}
	out.Reset()
	if code := RunMailRead("messages", []string{"list", "--limit", "0", "--json"}, &out, &diagnostics); code != 2 || !strings.Contains(out.String(), "invalid_arguments") {
		t.Fatalf("invalid pagination: %d %s", code, out.String())
	}
}
func TestOutboxReadsHidePayloadAndBCC(t *testing.T) {
	mailReadFixture(t)
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	id, err := database.EnqueueOutbox(db.OutboxItem{AccountConfigID: "account-stable", Subject: "Queued", Recipients: "hidden@example.com", MessageJSON: []byte(`{"body":"private"}`), DraftJSON: []byte(`{}`), LastError: "private", MaxAttempts: 3})
	if err != nil {
		t.Fatal(err)
	}
	database.Close()
	var out, diagnostics bytes.Buffer
	if code := RunMailRead("outbox", []string{"status", fmt.Sprint(id), "--json"}, &out, &diagnostics); code != 0 || !strings.Contains(out.String(), `"state":"queued"`) || strings.Contains(out.String(), "private") || strings.Contains(out.String(), "hidden@example.com") {
		t.Fatalf("outbox: %d %s", code, out.String())
	}
}
func TestMailReadHelpAndUnknownSubcommand(t *testing.T) {
	var out, diagnostics bytes.Buffer
	if code := RunMailRead("accounts", []string{"--help"}, &out, &diagnostics); code != 0 || !strings.Contains(out.String(), "Usage:") {
		t.Fatalf("help: %d %s", code, out.String())
	}
	out.Reset()
	if code := RunMailRead("accounts", []string{"delete", "--json"}, &out, &diagnostics); code != 2 || !strings.Contains(out.String(), "invalid_arguments") {
		t.Fatalf("unknown: %d %s", code, out.String())
	}
}

func TestMailReadPlainOutputEscapesTerminalControls(t *testing.T) {
	var out bytes.Buffer
	code := readSuccess(&out, false, map[string]any{"items": []map[string]any{{"id": "cache:1", "subject": "Hello\x1b[2J\nforged row", "from": "sender\x1b]0;injected\a"}}})
	if code != 0 || strings.ContainsAny(out.String(), "\x1b\a") || strings.Count(out.String(), "\n") != 1 {
		t.Fatalf("unsafe terminal output: %q", out.String())
	}
}

func TestMailReadPagesKeepAccountIdentity(t *testing.T) {
	first := mailReadFixture(t)
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.AddAccount("second-stable", "Personal", "")
	if err != nil {
		t.Fatal(err)
	}
	mailbox, err := database.UpsertMailbox(db.Mailbox{AccountID: account, Name: "INBOX"})
	if err != nil {
		t.Fatal(err)
	}
	if err = database.UpsertMessage(db.Message{MailboxID: mailbox, UID: 42, Subject: "Invoice second", BodyText: "private", Date: time.Unix(200, 0)}); err != nil {
		t.Fatal(err)
	}
	second, err := database.MessageIDByUID(mailbox, 42)
	if err != nil {
		t.Fatal(err)
	}
	page, total, err := database.SearchAllMessagesPage("invoice", false, 1, 1)
	if err != nil || total != 2 || len(page) != 1 || page[0].ID != second {
		t.Fatalf("page %v total %d error %v", page, total, err)
	}
	database.Close()
	var out, diagnostics bytes.Buffer
	if code := RunMailRead("messages", []string{"read", fmt.Sprintf("cache:%d", second), "--json"}, &out, &diagnostics); code != 0 || !strings.Contains(out.String(), `"account_id":"second-stable"`) || strings.Contains(out.String(), `"account_id":"account-stable"`) {
		t.Fatalf("cross-account identity: %d %s", code, out.String())
	}
	out.Reset()
	if code := RunMailRead("messages", []string{"list", "--limit", "1", "--offset", "1", "--json"}, &out, &diagnostics); code != 0 || !strings.Contains(out.String(), `"total":2`) || !strings.Contains(out.String(), fmt.Sprintf(`"id":"cache:%d"`, first)) || strings.Contains(out.String(), `Invoice second`) {
		t.Fatalf("bounded page %d %s", code, out.String())
	}
}
