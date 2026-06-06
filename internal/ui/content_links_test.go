package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
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
