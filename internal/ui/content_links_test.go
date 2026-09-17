package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
)

func newFocusLineModel(t *testing.T) Model {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	cfg := config.DefaultConfig()
	cfg.Display.FocusLine = true
	m := NewModel(database, cfg, "dev", false)
	m.focused = paneContent
	m.filteredMessages = []db.Message{{ID: 1, Subject: "Hi"}}
	return m
}

func TestFocusedLineLinkOpensURLOnFocusLine(t *testing.T) {
	m := newFocusLineModel(t)
	m.contentLines = []string{"Greetings,", "See https://example.com/article for details.", "Bye"}
	m.contentFocusLine = 1

	link, ok := m.focusedLineLink()
	if !ok {
		t.Fatalf("expected a link on the focused line")
	}
	if link != "https://example.com/article" {
		t.Fatalf("link = %q", link)
	}

	// Pressing `o` should dispatch the open-browser command.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if cmd == nil {
		t.Fatalf("expected `o` to return an open-browser command for the focused link")
	}
}

func TestFocusedLineLinkNoURLReturnsNothing(t *testing.T) {
	m := newFocusLineModel(t)
	m.contentLines = []string{"Greetings,", "No link on this line.", "Bye"}
	m.contentFocusLine = 1

	if _, ok := m.focusedLineLink(); ok {
		t.Fatalf("expected no link on a line without a URL")
	}

	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}}); cmd != nil {
		t.Fatalf("expected `o` to do nothing when the focused line has no link")
	}
}

func TestFocusedLineLinkRequiresFocusLineSetting(t *testing.T) {
	m := newFocusLineModel(t)
	m.cfg.Display.FocusLine = false
	m.contentLines = []string{"See https://example.com here."}
	m.contentFocusLine = 0

	if _, ok := m.focusedLineLink(); ok {
		t.Fatalf("expected no focused-line link when FocusLine display setting is off")
	}
}

const osc8TestPost = "https://www.reddit.com/r/omarchy/comments/1vxc6xv/free_ai_in_omarchy/"

// A Reddit CTA carries its URL only inside an OSC 8 escape, which ansi.Strip
// removes from contentLines — so the link has to come from the parallel slice.
func TestFocusedLineLinkFallsBackToOSC8Target(t *testing.T) {
	m := newFocusLineModel(t)
	m.contentLines = []string{"[Read post]"}
	m.contentLineLinks = []string{osc8TestPost}
	m.contentFocusLine = 0

	link, ok := m.focusedLineLink()
	if !ok {
		t.Fatal("expected the OSC 8 target to resolve")
	}
	if link != osc8TestPost {
		t.Fatalf("link = %q, want %q", link, osc8TestPost)
	}
}

// Visible text stays authoritative: what the reader can see is what they meant.
func TestFocusedLineLinkPrefersVisibleURLOverHyperlink(t *testing.T) {
	m := newFocusLineModel(t)
	m.contentLines = []string{"See https://example.com/visible now"}
	m.contentLineLinks = []string{"https://example.com/hidden"}
	m.contentFocusLine = 0

	link, ok := m.focusedLineLink()
	if !ok {
		t.Fatal("expected a link")
	}
	if link != "https://example.com/visible" {
		t.Fatalf("link = %q, want the visible URL", link)
	}
}

// Callers that set contentLines without the parallel slice must still work.
func TestFocusedLineLinkHandlesMissingLineLinks(t *testing.T) {
	m := newFocusLineModel(t)
	m.contentLines = []string{"no link here", "nor here"}
	m.contentLineLinks = nil
	m.contentFocusLine = 1

	if _, ok := m.focusedLineLink(); ok {
		t.Fatal("expected no link when the line has none and no link slice exists")
	}
}

func TestEnterOpensFocusedLineLink(t *testing.T) {
	m := newFocusLineModel(t)
	m.contentLines = []string{"[Read post]"}
	m.contentLineLinks = []string{osc8TestPost}
	m.contentFocusLine = 0

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected Enter to dispatch the open-browser command")
	}
}

func TestEnterInContentPaneWithoutLinkDoesNothing(t *testing.T) {
	m := newFocusLineModel(t)
	m.contentLines = []string{"nothing to open here"}
	m.contentFocusLine = 0

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("expected Enter to stay inert when no link resolves")
	}
}

// Enter must still move focus from the list into the reading pane.
func TestEnterFromMessageListStillFocusesContentPane(t *testing.T) {
	m := newFocusLineModel(t)
	m.focused = paneMessages
	m.messages = m.filteredMessages
	m.messageCursor = 0

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := next.(Model).focused; got != paneContent {
		t.Fatalf("focused = %v, want paneContent", got)
	}
}

func TestFocusedLineLinkIgnoresOSC8TargetWhenFocusLineDisabled(t *testing.T) {
	m := newFocusLineModel(t)
	m.cfg.Display.FocusLine = false
	m.contentLines = []string{"[Read post]"}
	m.contentLineLinks = []string{osc8TestPost}
	m.contentFocusLine = 0

	if _, ok := m.focusedLineLink(); ok {
		t.Fatal("expected no link when the focus line is disabled")
	}
}
