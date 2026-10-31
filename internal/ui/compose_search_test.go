package ui

import (
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestComposeSearchOwnsEscTabAndReplacement(t *testing.T) {
	previous := editorSearchRegex
	setEditorSearchRegex(false)
	t.Cleanup(func() { setEditorSearchRegex(previous) })
	c := NewCompose(config.AccountConfig{}, nil, nil)
	c.focusedField = composeFieldBody
	c.bodyInput.Focus()
	c.bodyInput.SetValue("cat cat")
	c.bodyInput.ed.SetSize(40, 6)
	press := func(msg tea.KeyMsg) {
		t.Helper()
		next, _, done := c.Update(msg, DefaultKeys)
		c = next
		if done {
			t.Fatal("search key closed compose")
		}
	}
	press(tea.KeyMsg{Type: tea.KeyCtrlF})
	if !c.bodyInput.ed.SearchActive() {
		t.Fatal("find did not open")
	}
	c.bodyInput.ed.SetSearchQuery("cat")
	press(tea.KeyMsg{Type: tea.KeyCtrlR})
	press(tea.KeyMsg{Type: tea.KeyTab})
	press(tea.KeyMsg{Type: tea.KeyShiftTab})
	if c.focusedField != composeFieldBody {
		t.Fatal("tab left body")
	}
	press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("dog")})
	press(tea.KeyMsg{Type: tea.KeyEnter})
	if c.bodyInput.Value() != "dog cat" {
		t.Fatalf("replacement edited wrong field: %q", c.bodyInput.Value())
	}
	press(tea.KeyMsg{Type: tea.KeyEsc})
	if c.bodyInput.ed.SearchActive() {
		t.Fatal("esc did not close search")
	}
	if c.bodyInput.Value() != "dog cat" {
		t.Fatal("esc undid replacement")
	}
}
func TestComposeSearchRegexAvailabilityAndSize(t *testing.T) {
	previous := editorSearchRegex
	t.Cleanup(func() { setEditorSearchRegex(previous) })
	setEditorSearchRegex(false)
	c := NewCompose(config.AccountConfig{}, nil, nil)
	if c.bodyInput.ed.Search().RegexAvailable {
		t.Fatal("regex must default off")
	}
	setEditorSearchRegex(true)
	c.focusedField = composeFieldBody
	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyCtrlF}, DefaultKeys)
	if !c.bodyInput.ed.Search().RegexAvailable || c.bodyInput.ed.Search().Regex {
		t.Fatal("availability must not enable regex")
	}
	c.bodyInput.ed.SetValue("猫 cat cat")
	c.bodyInput.ed.OpenReplace()
	c.bodyInput.ed.SetSearchQuery("cat")
	for _, size := range [][2]int{{40, 18}, {60, 24}, {100, 36}} {
		view := c.View(size[0], size[1], BuildStyles(CatppuccinMocha, "compact", "square"))
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatalf("search overflows %v", size)
			}
		}
		if !strings.Contains(ansi.Strip(view), "Find") {
			t.Fatalf("find missing at %v", size)
		}
	}
	setEditorSearchRegex(false)
	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyTab}, DefaultKeys)
	if c.bodyInput.ed.Search().RegexAvailable {
		t.Fatal("config update did not apply")
	}
}
func TestComposeSearchKeepsExplicitSendShortcut(t *testing.T) {
	c := NewCompose(config.AccountConfig{}, nil, nil)
	c.focusedField = composeFieldBody
	c.bodyInput.ed.OpenSearch()
	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyCtrlS}, DefaultKeys)
	if !c.isErr || c.statusMsg == "" {
		t.Fatal("send should validate missing recipient even while search is open")
	}
}
func TestSettingsSearchRegexRoundTrip(t *testing.T) {
	cfg := config.DefaultConfig()
	s := newSettings(cfg, settingsUpdateState{})
	if s.composeSearchRegex {
		t.Fatal("regex default must be off")
	}
	s.composeSearchRegex = true
	next := s.ApplyTo(cfg)
	if !next.Display.ComposeSearchRegex || !newSettings(next, settingsUpdateState{}).composeSearchRegex {
		t.Fatal("regex setting lost")
	}
}

func TestComposeSearchRootRoutingOwnsGrammarAndPaste(t *testing.T) {
	m, _ := newMailboxListModel(7, 1)
	m.overlay = overlayCompose
	m.compose = NewCompose(config.AccountConfig{}, nil, nil)
	m.compose.focusedField = composeFieldBody
	m.compose.bodyInput.SetValue("cat cat")
	m.compose.bodyInput.ed.OpenSearch()
	m.compose.bodyInput.ed.SetSearchQuery("cat")
	next, _ := m.handleCompose(tea.KeyMsg{Type: tea.KeyCtrlG})
	m = next.(Model)
	if m.compose.statusMsg != "" || m.compose.busy {
		t.Fatal("search triggered grammar check")
	}
	// No clipboard configured: search paste must stay with Ripple, not the root
	// application's unrelated clipboard command.
	m.compose.bodyInput.ed.SetClipboard(nil)
	next, cmd := m.handleCompose(tea.KeyMsg{Type: tea.KeyCtrlV})
	m = next.(Model)
	if cmd != nil {
		t.Fatal("root intercepted paste while search was active")
	}
	if m.compose.bodyInput.Value() != "cat cat" {
		t.Fatal("search changed the draft")
	}
}

func TestTemplateSearchEscStaysInEditor(t *testing.T) {
	c := NewCompose(config.AccountConfig{}, nil, nil)
	c.tmplOpen = true
	c.tmplEditing = true
	c.tmplEd = newEditorArea()
	c.tmplEd.SetValue("template text")
	c.tmplEd.ed.OpenSearch()
	next, _, done := c.Update(tea.KeyMsg{Type: tea.KeyEsc}, DefaultKeys)
	if done || !next.tmplOpen || !next.tmplEditing || next.tmplEd.ed.SearchActive() {
		t.Fatal("Esc must close template search before leaving template editing")
	}
}

type composeSearchClipboard struct{}

func (composeSearchClipboard) Read() (string, error) { return "cat", nil }
func (composeSearchClipboard) Write(string) error    { return nil }

func TestComposeSearchRootClipboardRoundTrip(t *testing.T) {
	m, _ := newMailboxListModel(7, 1)
	m.overlay = overlayCompose
	m.compose = NewCompose(config.AccountConfig{}, nil, nil)
	m.compose.focusedField = composeFieldBody
	m.compose.bodyInput.SetValue("cat dog")
	m.compose.bodyInput.ed.OpenSearch()
	m.compose.bodyInput.ed.SetSearchQuery("")
	m.compose.bodyInput.ed.SetClipboard(composeSearchClipboard{})
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("paste command missing")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	if m.compose.bodyInput.ed.Search().Query != "cat" || m.compose.bodyInput.Value() != "cat dog" {
		t.Fatal("clipboard result failed to reach search input")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = next.(Model)
	if m.overlay != overlayCompose {
		t.Fatal("palette stole search input")
	}
}

func TestComposeRegexQuotesAndReplacementErrors(t *testing.T) {
	previous := editorSearchRegex
	setEditorSearchRegex(true)
	t.Cleanup(func() { setEditorSearchRegex(previous) })
	c := NewCompose(config.AccountConfig{}, nil, nil)
	c.focusedField = composeFieldBody
	c.bodyInput.SetValue("> a\n> b\n> c")
	e := &c.bodyInput.ed
	e.OpenReplace()
	e.SetSearchQuery("^> ")
	e.SetSearchRegex(true)
	if e.ReplaceAll() != 3 || e.Value() != "a\nb\nc" {
		t.Fatal("quote stripping must match every line")
	}
	e.SetValue("price cat")
	e.OpenReplace()
	e.SetSearchQuery("cat")
	e.SetSearchRegex(true)
	e.SetReplacement("$5")
	if e.Search().ReplacementError == "" || e.ReplaceCurrent() || e.Value() != "price cat" {
		t.Fatal("unknown capture must not erase text")
	}
	e.SetReplacement("$$5")
	if !e.ReplaceCurrent() || e.Value() != "price $5" {
		t.Fatal("escaped dollar must remain literal")
	}
}
