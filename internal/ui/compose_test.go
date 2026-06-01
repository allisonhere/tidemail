package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
)

func TestComposeActionsStayVisibleInShortView(t *testing.T) {
	c := NewCompose(config.AccountConfig{}, nil, nil)
	c.focusedField = composeFieldBody
	c.bodyInput.Focus()
	c.bodyInput.SetValue(strings.Repeat("this is a long compose line that wraps inside the body editor ", 20))

	view := c.View(60, 12, BuildStyles(CatppuccinMocha, "compact"))
	stripped := ansi.Strip(view)

	if !strings.Contains(stripped, "SEND") {
		t.Fatalf("expected compose actions to stay visible in short view, got %q", stripped)
	}
}

func TestComposeActionsStayVisibleInOverlay(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "dev", false)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 18})
	m = next.(Model)
	m.overlay = overlayCompose
	m.compose = NewCompose(config.AccountConfig{}, nil, nil)
	m.compose.focusedField = composeFieldBody
	m.compose.bodyInput.Focus()
	m.compose.bodyInput.SetValue(strings.Repeat("this is a long compose line that wraps inside the body editor ", 40))

	view := m.View()
	stripped := ansi.Strip(view)

	if !strings.Contains(stripped, "SEND") {
		t.Fatalf("expected compose actions to stay visible in overlay, got %q", stripped)
	}
}

func TestComposeActionsWrapInNarrowView(t *testing.T) {
	c := NewCompose(config.AccountConfig{}, nil, nil)
	view := c.View(32, 16, BuildStyles(CatppuccinMocha, "compact"))
	stripped := ansi.Strip(view)

	for _, want := range []string{"SEND", "GRAMMAR"} {
		if !strings.Contains(stripped, want) {
			t.Fatalf("expected compose action %q to stay visible in narrow view, got %q", want, stripped)
		}
	}
}

func TestComposeTabAdvancesAfterAcceptingRecipientSuggestion(t *testing.T) {
	c := NewCompose(config.AccountConfig{}, nil, []string{"Alice <alice@example.com>"})

	var cmd tea.Cmd
	c, cmd, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ali"), Paste: true}, DefaultKeys)
	if cmd != nil {
		cmd()
	}

	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyTab}, DefaultKeys)
	if c.focusedField != composeFieldTo {
		t.Fatalf("first tab should accept the To suggestion before advancing, got focus %v", c.focusedField)
	}
	if got := c.toInput.Value(); got != "alice <alice@example.com>" {
		t.Fatalf("expected tab to accept To suggestion, got %q", got)
	}

	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyTab}, DefaultKeys)
	if c.focusedField != composeFieldCC {
		t.Fatalf("second tab should advance from To to CC, got focus %v", c.focusedField)
	}
}

func TestComposeTabAdvancesAfterAcceptingCCSuggestion(t *testing.T) {
	c := NewCompose(config.AccountConfig{}, nil, []string{"Carol <carol@example.com>"})
	c.advanceField(1)

	var cmd tea.Cmd
	c, cmd, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("car"), Paste: true}, DefaultKeys)
	if cmd != nil {
		cmd()
	}

	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyTab}, DefaultKeys)
	if c.focusedField != composeFieldCC {
		t.Fatalf("first tab should accept the CC suggestion before advancing, got focus %v", c.focusedField)
	}
	if got := c.ccInput.Value(); got != "carol <carol@example.com>" {
		t.Fatalf("expected tab to accept CC suggestion, got %q", got)
	}

	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyTab}, DefaultKeys)
	if c.focusedField != composeFieldSubject {
		t.Fatalf("second tab should advance from CC to Subject, got focus %v", c.focusedField)
	}
}
