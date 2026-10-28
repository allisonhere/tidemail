package mailapp

import (
	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/smtp"
	"strings"
	"testing"
)

func TestResolveAccount(t *testing.T) {
	cfg := config.Config{Accounts: []config.AccountConfig{{ID: "a", Name: "work", User: "a@example.com"}, {ID: "b", Name: "work", User: "b@example.com"}}}
	if _, e := ResolveAccount(cfg, "work"); e == nil {
		t.Fatal("ambiguous accepted")
	}
	a, e := ResolveAccount(cfg, "b")
	if e != nil || a.ID != "b" {
		t.Fatal(a, e)
	}
	if _, e = ResolveAccount(cfg, "missing"); e == nil {
		t.Fatal("missing accepted")
	}
}
func TestPrepare(t *testing.T) {
	a := config.AccountConfig{User: "me@example.com", Signature: "First\nSecond"}
	d := db.Draft{To: "You <you@example.com>", BCC: "secret@example.com", Subject: "hi", BodyText: "# Report", Attachments: []db.DraftAttachment{{Filename: "report.txt", Data: []byte("hello")}}}
	m, e := Prepare(a, d, true)
	if e != nil {
		t.Fatal(e)
	}
	raw := string(smtp.BuildRaw(a, m))
	if !strings.Contains(m.HTMLBody, "<h1>Report</h1>") || !strings.Contains(m.Body, "-- \nFirst\nSecond") || len(m.Attachments) != 1 {
		t.Fatal(m)
	}
	if strings.Contains(raw, "secret@example.com") {
		t.Fatal("BCC exposed in MIME")
	}
	d.To = "bad"
	if _, e = Prepare(a, d, true); e == nil {
		t.Fatal("invalid accepted")
	}
	d.To = "you@example.com"
	d.Subject = "hi\r\nBcc:evil@example.com"
	if _, e = Prepare(a, d, true); e == nil {
		t.Fatal("injection accepted")
	}
}
func TestAgentDefaults(t *testing.T) {
	cfg := config.Config{}
	if !Allowed(cfg, "drafts.create") || Allowed(cfg, "mail.send") || Allowed(cfg, "messages.read") {
		t.Fatal("unsafe defaults")
	}
	cfg.AgentPermissions = []string{}
	if Allowed(cfg, "drafts.create") {
		t.Fatal("empty should deny")
	}
}

func TestPrepareSignatureDisabledInOutgoingMIME(t *testing.T) {
	account := config.AccountConfig{User: "sender@example.com", Signature: "Unique signature\nSecond line"}
	draft := db.Draft{To: "recipient@example.com", BodyText: "Only body"}
	outgoing, err := Prepare(account, draft, false)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(smtp.BuildRaw(account, outgoing))
	if !strings.Contains(raw, "Only body") || strings.Contains(raw, "Unique signature") || strings.Contains(raw, "Second line") {
		t.Fatal("explicitly disabled signature appeared in outgoing MIME")
	}
}
