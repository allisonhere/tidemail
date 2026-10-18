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

func tmplCompose(t *testing.T) ComposeModel {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c := NewCompose(config.AccountConfig{Name: "Allie"}, nil, nil)
	c.bodyInput.SetSize(60, 10)
	c.toInput.SetValue("Priya Natarajan <p@x.com>")
	c.openTemplates()
	return c
}

func TestTemplatePreviewShowsRawTagsAndLegend(t *testing.T) {
	c := tmplCompose(t)
	if out := c.templatePreview(); !strings.Contains(out, "{first_name}") {
		t.Errorf("preview should show the raw tag:\n%s", out)
	}
	panel := c.templatesPanel(120, managerChrome{})
	if !strings.Contains(panel, "{first_name}=Priya") || !strings.Contains(panel, "{my_name}=Allie") {
		t.Errorf("legend missing tag values:\n%s", panel)
	}
}

func TestSerializeParseRoundTrip(t *testing.T) {
	for _, in := range []mailTemplate{{body: "hi\n"}, {subject: "Re: x", body: "hi {name}\n"}} {
		if got := parseTemplate(serializeTemplate(in)); got != in {
			t.Errorf("round trip %+v -> %+v", in, got)
		}
	}
}

func TestTemplateEditSave(t *testing.T) {
	c := tmplCompose(t)
	name := c.tmplList[c.tmplIdx].name // a built-in
	c, _, _ = c.updateTemplates(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")}, DefaultKeys)
	if !c.tmplEditing {
		t.Fatal("e should start editing")
	}
	c, _, _ = c.updateTemplates(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Z")}, DefaultKeys)
	c, _, _ = c.updateTemplates(tea.KeyMsg{Type: tea.KeyCtrlS}, DefaultKeys)
	if c.tmplEditing || c.isErr {
		t.Fatalf("save failed: editing=%v status=%q", c.tmplEditing, c.statusMsg)
	}
	dir, _ := config.TemplatesDir()
	data, err := os.ReadFile(filepath.Join(dir, name+".md"))
	if err != nil || !strings.Contains(string(data), "Z") || !strings.Contains(string(data), "{first_name}") {
		t.Fatalf("saved file = %q, %v", data, err)
	}
	if got := c.tmplList[c.tmplIdx]; got.name != name || !strings.Contains(got.body, "Z") {
		t.Errorf("reloaded template = %+v; want edited %q selected", got, name)
	}
}

func TestTemplateEditCancelWritesNothing(t *testing.T) {
	c := tmplCompose(t)
	c, _, _ = c.updateTemplates(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")}, DefaultKeys)
	c, _, _ = c.updateTemplates(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Z")}, DefaultKeys)
	c, _, _ = c.updateTemplates(tea.KeyMsg{Type: tea.KeyEsc}, DefaultKeys)
	if c.tmplEditing || !c.tmplOpen || strings.Contains(c.templatePreview(), "Z") {
		t.Errorf("esc should discard and stay in the picker")
	}
	dir, _ := config.TemplatesDir()
	if _, err := os.Stat(dir); err == nil {
		t.Error("cancel must not create the templates folder")
	}
}

func TestTemplateEditOversizeRefused(t *testing.T) {
	c := tmplCompose(t)
	c.tmplEditing = true
	c.tmplEd.SetValue(strings.Repeat("x", maxTemplateBytes+1))
	c.saveTemplate()
	dir, _ := config.TemplatesDir()
	if !c.isErr {
		t.Error("oversize save should report an error")
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("oversize save must not write")
	}
}

func tmplKey(c ComposeModel, s string) ComposeModel {
	km := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	switch s {
	case "enter":
		km = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		km = tea.KeyMsg{Type: tea.KeyEsc}
	}
	c, _, _ = c.updateTemplates(km, DefaultKeys)
	return c
}

func tmplType(c ComposeModel, name string) ComposeModel {
	for _, r := range name {
		c = tmplKey(c, string(r))
	}
	return c
}

func TestCreateTemplateFromBody(t *testing.T) {
	c := tmplCompose(t)
	c.bodyInput.SetValue("Hi {first_name}, my standard answer.\n\nThanks")
	c.subjectInput.SetValue("Quick note")
	c = tmplKey(c, "n")
	if !c.tmplNaming {
		t.Fatal("n should open the name prompt")
	}
	c = tmplType(c, "My answer")
	c = tmplKey(c, "enter")
	if c.tmplNaming || c.isErr {
		t.Fatalf("save failed: naming=%v status=%q", c.tmplNaming, c.statusMsg)
	}
	dir, _ := config.TemplatesDir()
	data, err := os.ReadFile(filepath.Join(dir, "My answer.md"))
	if err != nil || !strings.Contains(string(data), "subject: Quick note") || !strings.Contains(string(data), "my standard answer.") {
		t.Fatalf("file = %q, %v", data, err)
	}
	if got := c.tmplList[c.tmplIdx]; got.name != "My answer" || got.subject != "Quick note" {
		t.Errorf("selected = %+v", got)
	}
}

func TestCreateTemplateSkipsQuotedOriginal(t *testing.T) {
	c := tmplCompose(t)
	c.bodyInput.SetValue("My reply\n\nOn Bob wrote:\n> old text\n")
	c = tmplKey(c, "n")
	c = tmplType(c, "Reply")
	c = tmplKey(c, "enter")
	dir, _ := config.TemplatesDir()
	data, _ := os.ReadFile(filepath.Join(dir, "Reply.md"))
	if strings.Contains(string(data), "old text") || !strings.Contains(string(data), "My reply") {
		t.Errorf("saved %q; want only the user's own text", data)
	}
}

func TestCreateTemplateRefusals(t *testing.T) {
	c := tmplCompose(t)
	c.bodyInput.SetValue("some text")
	dir, _ := config.TemplatesDir()
	for name, typed := range map[string]string{
		"empty":    "",
		"built-in": "thanks", // case-insensitive match of "Thanks"
		"dot":      ".hidden",
		"slash":    "a/b",
		"too long": strings.Repeat("x", 61),
	} {
		d := tmplKey(c, "n")
		d = tmplType(d, typed)
		d = tmplKey(d, "enter")
		if !d.isErr || !d.tmplNaming {
			t.Errorf("%s: want refusal with the prompt still open, status=%q", name, d.statusMsg)
		}
	}
	empty := tmplCompose(t)
	empty = tmplKey(empty, "n")
	empty = tmplType(empty, "Fine")
	empty = tmplKey(empty, "enter")
	if !empty.isErr {
		t.Error("an empty body should be refused")
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("refusals must not create the templates folder")
	}
}

func TestCreateTemplateEscWritesNothing(t *testing.T) {
	c := tmplCompose(t)
	c.bodyInput.SetValue("text")
	c = tmplKey(c, "n")
	c = tmplType(c, "Nope")
	c = tmplKey(c, "esc")
	dir, _ := config.TemplatesDir()
	if c.tmplNaming || !c.tmplOpen {
		t.Error("esc should return to browsing the picker")
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("esc must not write")
	}
}

func TestTemplatePanelShowsNamePrompt(t *testing.T) {
	c := tmplCompose(t)
	c = tmplKey(c, "n")
	c = tmplType(c, "Abc")
	if p := c.templatesPanel(120, managerChrome{}); !strings.Contains(p, "new template name") || !strings.Contains(p, "Abc") {
		t.Errorf("panel missing the prompt:\n%s", p)
	}
}
