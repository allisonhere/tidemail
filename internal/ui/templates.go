package ui

import (
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/allisonhere/tidemail/internal/config"
)

// Mail templates (F6 in compose). Built-ins are embedded; any .md or .txt file
// in the templates folder next to config.toml adds one, and a file named like
// a built-in replaces it. The filename (minus extension) is the template's
// name. A file may start with a "---" block holding "subject: ..."; the rest is
// the body. Templates are plain text with a few {placeholders}.

const maxTemplateBytes = 64 << 10

type mailTemplate struct {
	name    string
	subject string
	body    string
}

var builtinTemplates = []mailTemplate{
	{name: "Thanks", body: "Hi {first_name},\n\nThanks so much, I really appreciate it.\n\nBest,\n{my_name}\n"},
	{name: "Got it", body: "Hi {first_name},\n\nGot it, thanks. I'll get back to you properly soon.\n\nBest,\n{my_name}\n"},
	{name: "Out of office", body: "Hi {first_name},\n\nThanks for your message. I'm away from my inbox and will reply when I'm back.\n\nBest,\n{my_name}\n"},
	{name: "Propose a meeting", body: "Hi {first_name},\n\nWould you be free for a short call this week? Let me know a couple of times that suit you and I'll make one work.\n\nBest,\n{my_name}\n"},
	{name: "Polite decline", body: "Hi {first_name},\n\nThank you for thinking of me. Unfortunately I can't take this on right now, but I appreciate the invitation.\n\nBest,\n{my_name}\n"},
	{name: "Follow up", body: "Hi {first_name},\n\nJust following up on my earlier message. Do you have any update?\n\nBest,\n{my_name}\n"},
}

// loadTemplates returns the built-ins plus the template files in dir, sorted by
// name. A missing or unreadable dir yields just the built-ins; unreadable,
// oversized, hidden and symlinked-out-of-dir files are skipped.
func loadTemplates(dir string) []mailTemplate {
	byName := map[string]mailTemplate{}
	for _, t := range builtinTemplates {
		byName[strings.ToLower(t.name)] = t
	}
	root, err := filepath.EvalSymlinks(dir)
	if err == nil {
		entries, _ := os.ReadDir(root)
		for _, e := range entries {
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if strings.HasPrefix(e.Name(), ".") || (ext != ".md" && ext != ".txt") {
				continue
			}
			real, err := filepath.EvalSymlinks(filepath.Join(root, e.Name()))
			if err != nil || filepath.Dir(real) != root {
				continue
			}
			info, err := os.Stat(real)
			if err != nil || !info.Mode().IsRegular() || info.Size() > maxTemplateBytes {
				continue
			}
			data, err := os.ReadFile(real)
			if err != nil || !utf8.Valid(data) {
				continue
			}
			t := parseTemplate(string(data))
			t.name = strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
			byName[strings.ToLower(t.name)] = t
		}
	}
	out := make([]mailTemplate, 0, len(byName))
	for _, t := range byName {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].name) < strings.ToLower(out[j].name)
	})
	return out
}

// parseTemplate splits an optional leading "---" block (only "subject:" is
// read) from the body.
func parseTemplate(text string) mailTemplate {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	head, rest, ok := strings.Cut(text, "\n")
	if !ok || strings.TrimSpace(head) != "---" {
		return mailTemplate{body: text}
	}
	block, body, found := strings.Cut(rest, "\n---\n")
	if !found {
		if b, ok := strings.CutSuffix(rest, "\n---"); ok {
			block, body, found = b, "", true
		}
	}
	if !found {
		return mailTemplate{body: text}
	}
	t := mailTemplate{body: body}
	for _, line := range strings.Split(block, "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "subject") {
			t.subject = strings.TrimSpace(v)
		}
	}
	return t
}

var placeholderRe = regexp.MustCompile(`\{[a-z_]+\}`)

// fillTemplate replaces the known {placeholders}; unknown ones stay as written.
func fillTemplate(s string, vars map[string]string) string {
	return placeholderRe.ReplaceAllStringFunc(s, func(m string) string {
		if v, ok := vars[m[1:len(m)-1]]; ok {
			return v
		}
		return m
	})
}

// templateVars builds the placeholder values from the To field's first
// recipient and the sending account's name.
func templateVars(to, myName string, now time.Time) map[string]string {
	name, first := "", ""
	if addrs, err := mail.ParseAddressList(to); err == nil && len(addrs) > 0 {
		name = addrs[0].Name
	}
	if fields := strings.Fields(name); len(fields) > 0 {
		first = fields[0]
	}
	if first == "" {
		first = "there"
	}
	return map[string]string{
		"name":       name,
		"first_name": first,
		"my_name":    myName,
		"date":       now.Format("January 2, 2006"),
	}
}

func (c *ComposeModel) openTemplates() {
	dir, _ := config.TemplatesDir()
	c.tmplList = loadTemplates(dir)
	c.tmplIdx = 0
	c.tmplOpen = true
	c.tmplEditing = false
	c.tmplNaming = false
	c.tmplName = newComposeInput("name for the new template")
	c.tmplEd = newEditorArea()
	c.syncTemplatePreview()
}

// serializeTemplate is the file form of t: a "subject:" block when there is a
// subject, then the body. It is the inverse of parseTemplate.
func serializeTemplate(t mailTemplate) string {
	if t.subject == "" {
		return t.body
	}
	return "---\nsubject: " + t.subject + "\n---\n" + t.body
}

// syncTemplatePreview loads the highlighted template, raw so its {tags} show,
// into tmplEd: as the file form when editing, otherwise as a read-only preview
// with the subject as a first line.
func (c *ComposeModel) syncTemplatePreview() {
	t := c.tmplList[c.tmplIdx]
	text := serializeTemplate(t)
	if !c.tmplEditing && t.subject != "" {
		text = "Subject: " + t.subject + "\n\n" + t.body
	}
	c.tmplEd.SetSize(c.bodyInput.w, c.bodyInput.h)
	c.tmplEd.SetValue(text)
	if c.tmplEditing {
		c.tmplEd.Focus()
	} else {
		c.tmplEd.Blur()
	}
}

func (c ComposeModel) updateTemplates(km tea.KeyMsg, keys KeyMap) (ComposeModel, tea.Cmd, bool) {
	if c.tmplNaming {
		switch {
		case keyMatches(km, keys.Cancel):
			c.tmplNaming = false
			c.tmplName.Blur()
		case keyMatches(km, keys.Confirm), keyMatches(km, keys.Enter):
			c.createTemplate()
		default:
			var cmd tea.Cmd
			c.tmplName, cmd = c.tmplName.Update(km)
			return c, cmd, false
		}
		return c, nil, false
	}
	if c.tmplEditing {
		switch {
		case keyMatches(km, keys.Save):
			c.saveTemplate()
		case keyMatches(km, keys.Cancel) && c.tmplEd.Mode() != "INSERT" && c.tmplEd.Mode() != "VISUAL":
			c.tmplEditing = false
			c.syncTemplatePreview()
			c.statusMsg, c.isErr = "edit discarded", false
		default:
			var cmd tea.Cmd
			c.tmplEd, cmd = c.tmplEd.Update(km)
			return c, cmd, false
		}
		return c, nil, false
	}
	n := len(c.tmplList)
	switch {
	case keyMatches(km, keys.Cancel):
		c.tmplOpen = false
	case keyMatches(km, keys.Left), keyMatches(km, keys.Up):
		c.tmplIdx = (c.tmplIdx + n - 1) % n
		c.syncTemplatePreview()
	case keyMatches(km, keys.Right), keyMatches(km, keys.Down), keyMatches(km, keys.Tab):
		c.tmplIdx = (c.tmplIdx + 1) % n
		c.syncTemplatePreview()
	case km.String() == "e":
		c.tmplEditing = true
		c.syncTemplatePreview()
	case km.String() == "n":
		c.tmplNaming = true
		c.tmplName.SetValue("")
		c.tmplName.Focus()
		c.statusMsg, c.isErr = "", false
	case keyMatches(km, keys.Confirm), keyMatches(km, keys.Enter):
		c.insertTemplate(c.tmplList[c.tmplIdx])
		c.tmplOpen = false
	}
	return c, nil, false
}

// writeTemplate writes text to <templates dir>/<name>.md, creating the folder.
func writeTemplate(name, text string) error {
	if len(text) > maxTemplateBytes {
		return fmt.Errorf("template too large to save")
	}
	dir, err := config.TemplatesDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name+".md"), []byte(text), 0o600)
}

// reloadTemplates re-reads the list and selects the template called name.
func (c *ComposeModel) reloadTemplates(name string) {
	dir, _ := config.TemplatesDir()
	c.tmplList = loadTemplates(dir)
	for i, t := range c.tmplList {
		if strings.EqualFold(t.name, name) {
			c.tmplIdx = i
		}
	}
	c.syncTemplatePreview()
}

// saveTemplate writes the edited text over the highlighted template. Editing a
// built-in writes a same-named file, which overrides it on the next load.
func (c *ComposeModel) saveTemplate() {
	name := c.tmplList[c.tmplIdx].name
	if err := writeTemplate(name, c.tmplEd.Value()); err != nil {
		c.statusMsg, c.isErr = "couldn't save template: "+err.Error(), true
		return
	}
	c.tmplEditing = false
	c.reloadTemplates(name)
	c.statusMsg, c.isErr = "saved template: "+name, false
}

// cleanTemplateName turns what was typed into a template name that is safe as
// a filename and survives a round trip through loadTemplates.
func cleanTemplateName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("type a name for the template")
	}
	if strings.HasPrefix(s, ".") || strings.ContainsAny(s, `/\:`) || strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return "", fmt.Errorf(`names can't start with "." or contain / \ : or control characters`)
	}
	if utf8.RuneCountInString(s) > 60 {
		return "", fmt.Errorf("name is too long (60 characters at most)")
	}
	return s, nil
}

// createTemplate saves what the user wrote in the message (not the quoted
// original) as a new template under the typed name.
func (c *ComposeModel) createTemplate() {
	fail := func(msg string) { c.statusMsg, c.isErr = msg, true }
	name, err := cleanTemplateName(c.tmplName.Value())
	if err != nil {
		fail(err.Error())
		return
	}
	for _, t := range c.tmplList {
		if strings.EqualFold(t.name, name) {
			fail(`"` + t.name + `" already exists; esc, then e to edit it`)
			return
		}
	}
	body, _ := splitReplyAndQuote(c.bodyInput.Value())
	body = strings.TrimSpace(body)
	if body == "" {
		fail("nothing to save: write your message first")
		return
	}
	text := serializeTemplate(mailTemplate{subject: strings.TrimSpace(c.subjectInput.Value()), body: body + "\n"})
	if err := writeTemplate(name, text); err != nil {
		fail("couldn't save template: " + err.Error())
		return
	}
	c.tmplNaming = false
	c.tmplName.Blur()
	c.reloadTemplates(name)
	c.statusMsg, c.isErr = "saved new template: "+name, false
}

// insertTemplate puts t's body at the caret (one undo step) and sets the
// subject only when it is still empty.
func (c *ComposeModel) insertTemplate(t mailTemplate) {
	vars := templateVars(c.toInput.Value(), c.accountCfg.Name, time.Now())
	c.bodyInput.InsertText(fillTemplate(t.body, vars))
	if strings.TrimSpace(c.subjectInput.Value()) == "" && t.subject != "" {
		c.subjectInput.SetValue(fillTemplate(t.subject, vars))
	}
	c.dirty = true
	c.statusMsg = "inserted template: " + t.name
	c.isErr = false
}

// templatePreview renders tmplEd: the raw template, or the editor on it.
func (c ComposeModel) templatePreview() string { return c.tmplEd.View() }

// templatesPanel renders the footer shown while choosing a template.
func (c ComposeModel) templatesPanel(width int, chrome managerChrome) string {
	base := lipgloss.NewStyle().Background(chrome.baseBg)
	t := c.tmplList[c.tmplIdx]
	preview := strings.Join(strings.Fields(t.body), " ")
	head := base.Foreground(chrome.muted).Render(fmt.Sprintf("  template %d/%d  ", c.tmplIdx+1, len(c.tmplList))) +
		base.Foreground(chrome.accent).Bold(true).Render(t.name) +
		base.Foreground(chrome.muted).Render("  "+truncateRunes(preview, max(10, width-len(t.name)-24)))
	keyStyle := base.Foreground(chrome.text)
	lblStyle := base.Foreground(chrome.muted)
	pairs := []string{"enter", "insert", "e", "edit", "n", "new from message", "←→", "choose", "esc", "close"}
	if c.tmplEditing {
		pairs = []string{"ctrl+s", "save", "esc", "cancel edit"}
	}
	if c.tmplNaming {
		pairs = []string{"enter", "save", "esc", "back"}
		head = base.Foreground(chrome.muted).Render("  new template name: ") +
			renderTextInput(c.tmplName, max(10, width-26), true, false, chrome)
	}
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, keyStyle.Render(pairs[i])+lblStyle.Render(" "+pairs[i+1]))
	}
	hints := base.Render("  ") + strings.Join(parts, base.Render("   "))
	if dir, err := config.TemplatesDir(); err == nil {
		hints += lblStyle.Render("   files in " + dir)
	}
	vars := templateVars(c.toInput.Value(), c.accountCfg.Name, time.Now())
	var legend []string
	seen := map[string]bool{}
	for _, m := range placeholderRe.FindAllString(serializeTemplate(t), -1) {
		if v, ok := vars[m[1:len(m)-1]]; ok && !seen[m] {
			seen[m] = true
			legend = append(legend, m+"="+v)
		}
	}
	legendLine := base.Foreground(chrome.muted).Render("  tags: " + strings.Join(legend, "  "))
	if len(legend) == 0 {
		legendLine = base.Foreground(chrome.muted).Render("  no {tags} in this template")
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		padStyled(head, width, chrome.baseBg),
		padStyled(legendLine, width, chrome.baseBg),
		padStyled(hints, width, chrome.baseBg))
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:max(0, n-1)]) + "…"
}
