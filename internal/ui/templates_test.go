package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/mailtemplate"
	"github.com/charmbracelet/x/ansi"
)

func TestInsertTemplate(t *testing.T) {
	c := NewCompose(config.AccountConfig{Name: "Allie"}, nil, nil)
	c.toInput.SetValue(`Sam Lee <sam@x.com>`)
	c.bodyInput.SetValue("AB")
	c.bodyInput.Focus()
	c.bodyInput, _ = c.bodyInput.Update(tea.KeyMsg{Type: tea.KeyCtrlHome})
	c.bodyInput, _ = c.bodyInput.Update(tea.KeyMsg{Type: tea.KeyRight})
	c.insertTemplate(mailtemplate.Template{Subject: "Hello {first_name}", Body: "[{first_name}]"})
	if got := c.bodyInput.Value(); got != "A[Sam]B" {
		t.Errorf("body = %q; want insertion at the caret", got)
	}
	if got := c.subjectInput.Value(); got != "Hello Sam" {
		t.Errorf("empty subject should be set, got %q", got)
	}
	c.insertTemplate(mailtemplate.Template{Subject: "Other", Body: ""})
	if got := c.subjectInput.Value(); got != "Hello Sam" {
		t.Errorf("a typed subject must not be replaced, got %q", got)
	}
}

func TestTemplatePickerKeys(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c := NewCompose(config.AccountConfig{Name: "Allie"}, nil, nil)
	c.openTemplates()
	if !c.tmplOpen || len(c.tmplList) != len(mailtemplate.Builtins) {
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
	want := c.tmplList[c.tmplIdx].Name
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

func TestTemplateEditSave(t *testing.T) {
	c := tmplCompose(t)
	name := c.tmplList[c.tmplIdx].Name // a built-in
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
	if got := c.tmplList[c.tmplIdx]; got.Name != name || !strings.Contains(got.Body, "Z") {
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
	c.tmplEd.SetValue(strings.Repeat("x", mailtemplate.MaxBytes+1))
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
	if got := c.tmplList[c.tmplIdx]; got.Name != "My answer" || got.Subject != "Quick note" {
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

func TestTemplateHintInEveryFieldAndPaletteEntry(t *testing.T) {
	c := tmplCompose(t)
	chrome := newManagerChrome(100, CatppuccinMocha, false)
	c.tmplOpen = false
	c.focusedField = composeFieldBody
	if h := ansi.Strip(c.composeHints(120, chrome)); !strings.Contains(h, "f6 templates") {
		t.Errorf("body footer should hint f6 templates:\n%s", h)
	}
	for _, f := range []composeField{composeFieldFrom, composeFieldSubject} {
		c.focusedField = f
		if h := ansi.Strip(c.composeHints(120, chrome)); !strings.Contains(h, "f6 templates") {
			t.Errorf("field %v footer should hint f6 templates:\n%s", f, h)
		}
	}
	m, _, _ := newIdentityModel(t, acct("Personal", "imap.example.com", "me@example.com", "pw"))
	found := false
	for _, it := range m.composeCommandItems() {
		if it.id == "compose-template" && it.enabled {
			found = true
		}
	}
	if !found {
		t.Error("compose palette should offer Insert template…")
	}
}
