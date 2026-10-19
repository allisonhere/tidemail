package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
)

func TestTemplatesList(t *testing.T) {
	mailFixture(t)
	dir, _ := config.TemplatesDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Mine.md"), []byte("---\nsubject: Hello\n---\nhi"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	if code := RunHeadless([]string{"templates", "list"}, strings.NewReader(""), &out, &errs); code != 0 {
		t.Fatalf("%d %s", code, errs.String())
	}
	if !strings.Contains(out.String(), "Thanks") || !strings.Contains(out.String(), "Mine\tHello") {
		t.Errorf("list = %q", out.String())
	}
	out.Reset()
	if code := RunHeadless([]string{"templates", "list", "--json"}, strings.NewReader(""), &out, &errs); code != 0 || !strings.Contains(out.String(), `"name":"Mine"`) {
		t.Errorf("json list = %d %q", code, out.String())
	}
	if code := RunHeadless([]string{"templates", "bogus"}, strings.NewReader(""), &out, &errs); code != 2 {
		t.Errorf("bad subcommand should be a usage error, got %d", code)
	}
}

func TestSendWithTemplateFillsBodyAndSubject(t *testing.T) {
	mailFixture(t)
	dir, _ := config.TemplatesDir()
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(filepath.Join(dir, "Greet.md"), []byte("---\nsubject: Hello {first_name}\n---\nHi {first_name}, from {my_name}."), 0o600)

	var out, errs bytes.Buffer
	args := []string{"send", "--template", "greet", "--to", "Priya Natarajan <p@example.com>", "--json"}
	if code := RunHeadless(args, strings.NewReader("ignored stdin"), &out, &errs); code != 0 {
		t.Fatalf("%d %s %s", code, out.String(), errs.String())
	}
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	drafts, err := database.ListDrafts("stable", "Work", "me@example.com")
	if err != nil || len(drafts) != 1 {
		t.Fatal(drafts, err)
	}
	if drafts[0].BodyText != "Hi Priya, from Work." || drafts[0].Subject != "Hello Priya" {
		t.Errorf("draft = %q / %q", drafts[0].Subject, drafts[0].BodyText)
	}
}

func TestSendTemplateSubjectFlagWinsAndConflicts(t *testing.T) {
	mailFixture(t)
	var out, errs bytes.Buffer
	if code := RunHeadless([]string{"send", "--template", "Thanks", "--to", "a@b.co", "--subject", "Mine", "--dry-run", "--json"}, strings.NewReader(""), &out, &errs); code != 0 {
		t.Fatalf("dry run with template failed: %d %s", code, out.String())
	}
	for name, args := range map[string][]string{
		"body":    {"send", "--template", "Thanks", "--body", "x", "--to", "a@b.co", "--json"},
		"file":    {"send", "--template", "Thanks", "--file", "x.txt", "--to", "a@b.co", "--json"},
		"missing": {"send", "--template", "No such one", "--to", "a@b.co", "--json"},
	} {
		out.Reset()
		code := RunHeadless(args, strings.NewReader(""), &out, &errs)
		if code != 2 {
			t.Errorf("%s: want usage error 2, got %d %s", name, code, out.String())
		}
		if name == "missing" && !strings.Contains(out.String(), "template_not_found") {
			t.Errorf("missing template should say so: %s", out.String())
		}
	}
}
