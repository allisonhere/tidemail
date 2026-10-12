package cli

import (
	"bytes"
	"encoding/json"
	"github.com/allisonhere/tidemail/internal/db"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mailFixture(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	dir := filepath.Join(home, "config", "tidemail")
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	text := "[[account]]\nid = 'stable'\nname = 'Work'\nuser = 'me@example.com'\nfrom = 'me@example.com'\nsmtp_host = 'localhost'\nsmtp_port = 587\n[display]\nsend_delay_seconds = 60\n"
	if e := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(text), 0600); e != nil {
		t.Fatal(e)
	}
}
func TestHeadlessSendDraftAndQueue(t *testing.T) {
	mailFixture(t)
	for _, tc := range []struct {
		name  string
		args  []string
		state string
	}{{"draft", []string{"send", "--to", "you@example.com", "--subject", "Hi", "--json"}, "drafted"}, {"queue", []string{"send", "--to", "you@example.com", "--subject", "Hi", "--yes", "--json"}, "queued"}, {"dry", []string{"send", "--to", "you@example.com", "--dry-run", "--json"}, "validated"}} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errs bytes.Buffer
			code := RunHeadless(tc.args, strings.NewReader("hello from pipe"), &out, &errs)
			if code != 0 {
				t.Fatalf("%d %s %s", code, out.String(), errs.String())
			}
			var v struct {
				OK   bool `json:"ok"`
				Data struct {
					State string `json:"state"`
				}
			}
			if e := json.Unmarshal(out.Bytes(), &v); e != nil || !v.OK || v.Data.State != tc.state {
				t.Fatal(out.String(), e)
			}
			if strings.Contains(out.String(), "\x1b") {
				t.Fatal("terminal pollution")
			}
		})
	}
	database, e := db.Open()
	if e != nil {
		t.Fatal(e)
	}
	defer database.Close()
	drafts, e := database.ListDrafts("stable", "Work", "me@example.com")
	if e != nil || len(drafts) != 1 || drafts[0].BodyText != "hello from pipe" {
		t.Fatal(drafts, e)
	}
	items, e := database.ListOutbox()
	if e != nil || len(items) != 1 || items[0].State != "queued" {
		t.Fatal(items, e)
	}
}
func TestHeadlessApprovalAndParsing(t *testing.T) {
	mailFixture(t)
	for _, args := range [][]string{{"send", "--to", "you@example.com", "--agent", "--yes", "--json"}, {"send", "--to", "you@example.com", "--body", "a", "--file", "b", "--json"}, {"send", "--to", "bad", "--json"}, {"draft", "create", "--to", "you@example.com", "--yes", "--json"}} {
		var out, errs bytes.Buffer
		if c := RunHeadless(args, strings.NewReader("body"), &out, &errs); c == 0 {
			t.Fatal(args, out.String())
		}
		if !json.Valid(out.Bytes()) {
			t.Fatal(out.String())
		}
	}
}
func TestHeadlessHelpWithoutConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, args := range [][]string{{"--help"}, {"send", "--help"}, {"draft", "--help"}, {"outbox", "process", "--help"}} {
		var out, errs bytes.Buffer
		if c := RunHeadless(args, strings.NewReader(""), &out, &errs); c != 0 || !strings.Contains(out.String(), "Usage:") || strings.Contains(out.String(), "panic") || errs.Len() != 0 {
			t.Fatal(args, c, out.String(), errs.String())
		}
	}
}

func TestHeadlessMarkdownAttachmentAndCancellation(t *testing.T) {
	mailFixture(t)
	bodyPath := filepath.Join(t.TempDir(), "body.md")
	attachmentPath := filepath.Join(t.TempDir(), "report.txt")
	if e := os.WriteFile(bodyPath, []byte("# Report\n\nComplete."), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(attachmentPath, []byte("attachment contents"), 0600); e != nil {
		t.Fatal(e)
	}
	var out, errs bytes.Buffer
	args := []string{"send", "--to", "you@example.com", "--subject", "Report", "--markdown", bodyPath, "--attach", attachmentPath, "--yes", "--json"}
	if code := RunHeadless(args, strings.NewReader("ignored pipe"), &out, &errs); code != 0 {
		t.Fatal(code, out.String(), errs.String())
	}
	database, e := db.Open()
	if e != nil {
		t.Fatal(e)
	}
	defer database.Close()
	items, e := database.ListOutbox()
	if e != nil || len(items) != 1 {
		t.Fatal(items, e)
	}
	item, e := database.GetOutbox(items[0].ID)
	if e != nil {
		t.Fatal(e)
	}
	var payload struct {
		HTMLBody    string
		Attachments []struct {
			Name string
			Data []byte
		}
	}
	if e = json.Unmarshal(item.MessageJSON, &payload); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(payload.HTMLBody, "<h1>Report</h1>") || len(payload.Attachments) != 1 || string(payload.Attachments[0].Data) != "attachment contents" {
		t.Fatal(payload)
	}
	draft, e := database.EditOutbox(item.ID)
	if e != nil {
		t.Fatal(e)
	}
	if draft.AccountConfigID != "stable" || len(draft.Attachments) != 1 || draft.BodyText != "# Report\n\nComplete." {
		t.Fatal(draft)
	}
}

func TestInteractiveFlagsPreserveHeadlessUsage(t *testing.T) {
	mailFixture(t)
	for _, args := range [][]string{{"send", "--to", "you@example.com", "--subject", "Hi", "--body", "Hello", "--no-interactive", "--json"}, {"accounts", "list", "--no-interactive", "--json"}, {"messages", "list", "--no-interactive", "--json"}, {"outbox", "list", "--no-interactive", "--json"}} {
		var out, errs bytes.Buffer
		if code := RunHeadless(args, strings.NewReader(""), &out, &errs); code != 0 {
			t.Fatal(args, code, out.String(), errs.String())
		}
		if !json.Valid(out.Bytes()) || strings.Contains(out.String(), "\x1b") {
			t.Fatal(args, out.String())
		}
	}
	var out, errs bytes.Buffer
	if code := RunHeadless([]string{"send", "--interactive", "--no-interactive", "--json"}, strings.NewReader(""), &out, &errs); code != 2 || !json.Valid(out.Bytes()) {
		t.Fatal(code, out.String())
	}
}
func TestInteractiveMenuHelpIsHeadless(t *testing.T) {
	var out, errs bytes.Buffer
	if code := RunHeadless([]string{"cli", "--help"}, strings.NewReader(""), &out, &errs); code != 0 || !strings.Contains(out.String(), "Usage: tidemail cli") {
		t.Fatal(code, out.String())
	}
}
func TestJSONParseErrorsDoNotPolluteStdout(t *testing.T) {
	var out, errs bytes.Buffer
	if code := RunHeadless([]string{"messages", "list", "--unknown", "--json"}, strings.NewReader(""), &out, &errs); code != 2 || !json.Valid(out.Bytes()) {
		t.Fatal(code, out.String(), errs.String())
	}
}
