package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tidemail/internal/config"
)

const longRecipients = "alice.anderson@example.com, bob.brown@example.com, carol.clark@example.com, dave.davis@example.com, erin.evans@example.com"

func composeScrollModel(t *testing.T) ComposeModel {
	t.Helper()
	acfg := config.AccountConfig{ID: "a", User: "me@example.com"}
	c := NewCompose(acfg, []config.AccountConfig{acfg}, nil)
	c.ccInput.SetValue(longRecipients)
	c.SetWidth(composeOverlayWidth(80))
	return c
}

// A recipient list longer than the field scrolls so the end (where the cursor
// is) stays visible, instead of running off the right edge.
func TestComposeLongCCScrollsToCursor(t *testing.T) {
	c := composeScrollModel(t)
	c.focusedField = composeFieldCC
	c.toInput.Blur()
	c.ccInput.Focus()

	view := ansi.Strip(c.View(composeOverlayWidth(80), 30, BuildStyles(CatppuccinMocha, "compact", "square")))
	if !strings.Contains(view, "erin.evans@example.com") {
		t.Fatalf("end of the Cc list is not visible:\n%s", view)
	}
	if strings.Contains(view, "alice.anderson") {
		t.Fatalf("start of the Cc list should have scrolled out:\n%s", view)
	}

	c.ccInput.CursorStart()
	view = ansi.Strip(c.View(composeOverlayWidth(80), 30, BuildStyles(CatppuccinMocha, "compact", "square")))
	if !strings.Contains(view, "alice.anderson") || strings.Contains(view, "erin.evans") {
		t.Fatalf("Home should scroll back to the start:\n%s", view)
	}
}

// Typing into a full field keeps the cursor end in view: the stored input
// (not just the render copy) must know its width.
func TestComposeTypingPastFieldWidthKeepsCursorVisible(t *testing.T) {
	acfg := config.AccountConfig{ID: "a", User: "me@example.com"}
	m := NewModel(nil, config.Config{Accounts: []config.AccountConfig{acfg}, Display: config.DefaultConfig().Display}, "", false)
	m.width, m.height = 80, 40
	m.compose = NewCompose(acfg, m.cfg.Accounts, nil)
	m.overlay = overlayCompose
	m.compose.focusedField = composeFieldCC
	m.compose.toInput.Blur()
	m.compose.ccInput.Focus()

	for _, r := range longRecipients {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(Model)
	}
	view := ansi.Strip(m.compose.View(composeOverlayWidth(80), 30, BuildStyles(CatppuccinMocha, "compact", "square")))
	if !strings.Contains(view, "erin.evans@example.com") || strings.Contains(view, "alice.anderson") {
		t.Fatalf("typed text should scroll with the cursor:\n%s", view)
	}
}

// The cursor block at the end of a full field must fit inside the row, not
// be cut off by the right edge.
func TestComposeLongCCCursorCellIsNotClipped(t *testing.T) {
	c := composeScrollModel(t)
	c.focusedField = composeFieldCC
	c.toInput.Blur()
	c.ccInput.Focus()

	_, textW := composeFieldWidths(composeOverlayWidth(80))
	if got := ansi.StringWidth(c.ccInput.View()); got > textW {
		t.Fatalf("input view is %d cells, field text area is %d: cursor would be clipped", got, textW)
	}
}
