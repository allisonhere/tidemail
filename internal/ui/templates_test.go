package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/allisonhere/tidemail/internal/config"
)

func templateNames(ts []mailTemplate) []string {
	var out []string
	for _, t := range ts {
		out = append(out, t.name)
	}
	return out
}

func find(ts []mailTemplate, name string) (mailTemplate, bool) {
	for _, t := range ts {
		if t.name == name {
			return t, true
		}
	}
	return mailTemplate{}, false
}

func TestLoadTemplates(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Invoice.md", "---\nsubject: Your invoice\n---\nHi {first_name}\n")
	write("plain.txt", "just text")
	write("Thanks.txt", "my own thanks")
	write(".hidden.md", "no")
	write("notes.pdf", "no")
	write("big.md", strings.Repeat("x", maxTemplateBytes+1))
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.md"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(dir, "escape.md")); err != nil {
		t.Fatal(err)
	}

	got := loadTemplates(dir)
	inv, ok := find(got, "Invoice")
	if !ok || inv.subject != "Your invoice" || inv.body != "Hi {first_name}\n" {
		t.Errorf("Invoice = %+v", inv)
	}
	if p, ok := find(got, "plain"); !ok || p.body != "just text" || p.subject != "" {
		t.Errorf("plain = %+v", p)
	}
	if th, _ := find(got, "Thanks"); th.body != "my own thanks" {
		t.Errorf("a file should replace the built-in of the same name, got %q", th.body)
	}
	for _, bad := range []string{".hidden", "notes", "big", "escape", "secret"} {
		if _, ok := find(got, bad); ok {
			t.Errorf("%q should have been skipped: %v", bad, templateNames(got))
		}
	}
	if len(got) != len(builtinTemplates)+2 { // Invoice and plain added, Thanks replaced
		t.Errorf("got %d templates: %v", len(got), templateNames(got))
	}
}

func TestLoadTemplatesMissingDir(t *testing.T) {
	got := loadTemplates(filepath.Join(t.TempDir(), "nope"))
	if len(got) != len(builtinTemplates) {
		t.Errorf("a missing folder should give just the built-ins, got %v", templateNames(got))
	}
}

func TestParseTemplate(t *testing.T) {
	for in, want := range map[string]mailTemplate{
		"body only":                        {body: "body only"},
		"---\nsubject: Hi\n---\nbody":      {subject: "Hi", body: "body"},
		"---\nsubject: Hi\r\n---\r\nbody":  {subject: "Hi", body: "body"},
		"---\nother: x\n---\nbody":         {body: "body"},
		"---\nsubject: unterminated\nbody": {body: "---\nsubject: unterminated\nbody"},
		"---\nsubject: Only\n---":          {subject: "Only"},
	} {
		if got := parseTemplate(in); got != want {
			t.Errorf("parseTemplate(%q) = %+v; want %+v", in, got, want)
		}
	}
}

func TestFillTemplate(t *testing.T) {
	now := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	v := templateVars(`"Priya Natarajan" <p@acme.com>`, "Allie", now)
	got := fillTemplate("Hi {first_name} ({name}), {date}, {my_name} {unknown} {Not}", v)
	want := "Hi Priya (Priya Natarajan), October 18, 2026, Allie {unknown} {Not}"
	if got != want {
		t.Errorf("got %q; want %q", got, want)
	}
	if v := templateVars("p@acme.com", "A", now); v["first_name"] != "there" || v["name"] != "" {
		t.Errorf("without a display name first_name should be 'there': %v", v)
	}
	if v := templateVars("", "A", now); v["first_name"] != "there" {
		t.Errorf("empty To: %v", v)
	}
}

func TestInsertTemplate(t *testing.T) {
	c := NewCompose(config.AccountConfig{Name: "Allie"}, nil, nil)
	c.toInput.SetValue(`Sam Lee <sam@x.com>`)
	c.bodyInput.SetValue("AB")
	c.bodyInput.Focus()
	c.bodyInput, _ = c.bodyInput.Update(tea.KeyMsg{Type: tea.KeyCtrlHome})
	c.bodyInput, _ = c.bodyInput.Update(tea.KeyMsg{Type: tea.KeyRight})
	c.insertTemplate(mailTemplate{subject: "Hello {first_name}", body: "[{first_name}]"})
	if got := c.bodyInput.Value(); got != "A[Sam]B" {
		t.Errorf("body = %q; want insertion at the caret", got)
	}
	if got := c.subjectInput.Value(); got != "Hello Sam" {
		t.Errorf("empty subject should be set, got %q", got)
	}
	c.insertTemplate(mailTemplate{subject: "Other", body: ""})
	if got := c.subjectInput.Value(); got != "Hello Sam" {
		t.Errorf("a typed subject must not be replaced, got %q", got)
	}
}

func TestTemplatePickerKeys(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c := NewCompose(config.AccountConfig{Name: "Allie"}, nil, nil)
	c.openTemplates()
	if !c.tmplOpen || len(c.tmplList) != len(builtinTemplates) {
		t.Fatalf("open = %v, %d templates", c.tmplOpen, len(c.tmplList))
	}
	c, _, _ = c.updateTemplates(tea.KeyMsg{Type: tea.KeyRight}, DefaultKeys)
	if c.tmplIdx != 1 {
		t.Errorf("right should move to the next template, idx = %d", c.tmplIdx)
	}
	c, _, _ = c.updateTemplates(tea.KeyMsg{Type: tea.KeyLeft}, DefaultKeys)
	c, _, _ = c.updateTemplates(tea.KeyMsg{Type: tea.KeyLeft}, DefaultKeys)
	if c.tmplIdx != len(c.tmplList)-1 {
		t.Errorf("left should wrap, idx = %d", c.tmplIdx)
	}
	want := c.tmplList[c.tmplIdx].name
	c, _, _ = c.updateTemplates(tea.KeyMsg{Type: tea.KeyEnter}, DefaultKeys)
	if c.tmplOpen || !strings.Contains(c.statusMsg, want) || c.bodyInput.Value() == "" {
		t.Errorf("enter should insert %q and close: open=%v status=%q", want, c.tmplOpen, c.statusMsg)
	}
	c.openTemplates()
	c, _, _ = c.updateTemplates(tea.KeyMsg{Type: tea.KeyEsc}, DefaultKeys)
	if c.tmplOpen {
		t.Error("esc should close the picker")
	}
}

func TestTemplatePreviewShowsFilledBody(t *testing.T) {
	c := NewCompose(config.AccountConfig{}, nil, nil)
	c.bodyInput.SetSize(60, 10)
	c.toInput.SetValue("Priya Natarajan <p@x.com>")
	c.tmplList = []mailTemplate{{name: "T", body: "Hi {first_name}, thanks."}}
	c.tmplOpen = true
	if out := c.templatePreview(); !strings.Contains(out, "Hi Priya, thanks.") {
		t.Errorf("preview missing filled body:\n%s", out)
	}
}
