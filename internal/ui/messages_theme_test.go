package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// The multi-selected-row tint and the search-mode header badge must come from
// the active theme, not fixed hex values that clash on light themes. Rendered
// under a light theme (truecolor profile so colors survive as RGB), the old
// hardcoded colors must be gone and the theme's own accent must be present.
func TestMessagesPaneUsesThemeColorsNotHardcodedHex(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	cfg := config.DefaultConfig()
	cfg.Theme = "catppuccin-latte" // a light theme; BorderFocus #1e66f5, Unread #40a02b
	m := NewModel(nil, cfg, "dev", false)
	m.width = 100
	m.height = 40

	now := time.Now()
	m.filteredMessages = []db.Message{
		{ID: 1, Subject: "First", From: "a@example.com", Date: now},
		{ID: 2, Subject: "Second", From: "b@example.com", Date: now},
	}
	m.messageCursor = 0                          // cursor on row 0
	m.selectedMessages = map[int64]bool{2: true} // row 1 selected but not cursor -> accent tint
	m.searchMode = true                          // renders the header badge
	m.searchQuery = "hello"

	view := m.renderMessagesPane()

	// Old hardcoded colors, as truecolor escapes, must be gone. (The badge
	// foreground is intentionally not checked: accentReadableOn can legitimately
	// drive it to white for contrast on the blue badge — that's themed, not the
	// old hardcode.)
	banned := map[string]string{
		"38;2;166;227;161": "selection green #a6e3a1",
		"48;2;42;42;42":    "badge background #2a2a2a",
	}
	for esc, desc := range banned {
		if strings.Contains(view, esc) {
			t.Errorf("rendered pane still uses hardcoded %s under a light theme", desc)
		}
	}

	// The search badge must now use the theme's focus accent as its background.
	if !strings.Contains(view, "48;2;30;102;245") {
		t.Error("search badge does not use the theme BorderFocus (#1e66f5) background")
	}
}
