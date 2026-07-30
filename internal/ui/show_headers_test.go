package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
)

func headerTestModel(t *testing.T, showHeaders bool) Model {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Display.ShowHeaders = showHeaders
	m := NewModel(nil, cfg, "dev", false)
	m.width = 100
	m.height = 30
	m.focused = paneContent
	m.viewport.Width = m.contentBodyWidth()
	m.viewport.Height = m.contentBodyHeight()
	msgs := []db.Message{
		{ID: 1, Subject: "First", From: "a@example.com", BodyText: "One."},
		{ID: 2, Subject: "Second", From: "b@example.com", BodyText: "Two."},
	}
	m.messages = msgs
	m.filteredMessages = msgs
	m.setViewportForCurrentRow()
	return m
}

// cfg.Display.ShowHeaders was previously written by the settings form but never
// read by the renderer, which hardcoded the headers on.
func TestShowHeadersConfigGatesRenderer(t *testing.T) {
	on := ansi.Strip(headerTestModel(t, true).renderMessageContent(db.Message{ID: 1, Subject: "S", From: "a@example.com"}))
	if !strings.Contains(on, "From:") {
		t.Fatalf("show_headers=true did not render the header block: %q", on)
	}

	off := ansi.Strip(headerTestModel(t, false).renderMessageContent(db.Message{ID: 1, Subject: "S", From: "a@example.com"}))
	if strings.Contains(off, "Message-ID:") {
		t.Fatalf("show_headers=false still rendered the header block: %q", off)
	}
}

// The ctrl+e override was reset to true on every message change, so it could
// never survive moving to the next message.
func TestToggleHeadersPersistsAcrossMessages(t *testing.T) {
	m := headerTestModel(t, true)
	if !m.contentShowHeaders {
		t.Fatal("headers should start on with show_headers=true")
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	m = next.(Model)
	if m.contentShowHeaders {
		t.Fatal("ctrl+e did not turn headers off")
	}

	m.setViewportMessage(m.filteredMessages[1])
	if m.contentShowHeaders {
		t.Fatal("moving to another message reset the ctrl+e override")
	}
}

func TestShowHeadersInitializesFromConfig(t *testing.T) {
	if m := headerTestModel(t, false); m.contentShowHeaders {
		t.Fatal("contentShowHeaders should initialize from cfg.Display.ShowHeaders")
	}
	if m := headerTestModel(t, true); !m.contentShowHeaders {
		t.Fatal("contentShowHeaders should initialize from cfg.Display.ShowHeaders")
	}
}

func TestIsEmojiGraphemeRanges(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{"smiley", "🙂", true},
		{"flag", "🇺🇸", true},
		{"skin tone", "👍🏽", true},
		{"mahjong", "🀄", true},
		{"medal", "🥇", true},
		{"keycap", "1️⃣", true},
		{"enclosed alphanumeric", "🄰", false},
		{"math script", "𝕏", false},
		{"arrow", "→", false},
		{"greek", "π", false},
		{"ascii", "a", false},
		{"em dash", "—", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stripEmojiGraphemes(tc.input) == ""
			if got != tc.want {
				t.Fatalf("stripEmojiGraphemes(%q) stripped=%v, want %v", tc.input, got, tc.want)
			}
		})
	}
}
