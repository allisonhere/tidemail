package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
)

func composeFieldLine(view, label string) string {
	stripped := ansi.Strip(view)
	for _, line := range strings.Split(stripped, "\n") {
		if strings.Contains(line, label) {
			return line
		}
	}
	return ""
}

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
	if got := c.toInput.Value(); got != "alice <alice@example.com>" {
		t.Fatalf("expected tab to accept To suggestion, got %q", got)
	}
	if c.focusedField != composeFieldCC {
		t.Fatalf("tab should accept To suggestion and advance to CC, got focus %v", c.focusedField)
	}
	toLine := composeFieldLine(c.View(90, 24, BuildStyles(CatppuccinMocha, "compact")), "To")
	if strings.Contains(toLine, " >") {
		t.Fatalf("expected To row not to show focus marker after advancing to CC, got %q", toLine)
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
	if got := c.ccInput.Value(); got != "carol <carol@example.com>" {
		t.Fatalf("expected tab to accept CC suggestion, got %q", got)
	}
	if c.focusedField != composeFieldSubject {
		t.Fatalf("tab should accept CC suggestion and advance to Subject, got focus %v", c.focusedField)
	}
}

func TestReplyTypingStartsAboveQuotedMessage(t *testing.T) {
	c := NewReply(db.Message{
		From:      "alice@example.com",
		Subject:   "Plans",
		MessageID: "<plans@example.com>",
		BodyText:  "quoted line",
	}, config.AccountConfig{}, nil)
	c.focusedField = composeFieldBody
	c.bodyInput.Focus()

	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("H")}, DefaultKeys)

	got := c.bodyInput.Value()
	if !strings.HasPrefix(got, "H\n\nOn alice@example.com wrote:\n> quoted line") {
		t.Fatalf("expected typed reply text above quoted original, got %q", got)
	}
}

func TestForwardTypingStartsAboveQuotedMessage(t *testing.T) {
	c := NewForward(db.Message{
		From:      "alice@example.com",
		Subject:   "Plans",
		MessageID: "<plans@example.com>",
		BodyText:  "forwarded line",
	}, config.AccountConfig{}, nil)
	c.focusedField = composeFieldBody
	c.bodyInput.Focus()

	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("H")}, DefaultKeys)

	got := c.bodyInput.Value()
	if !strings.HasPrefix(got, "H\n\n---------- Forwarded message ----------\nFrom: alice@example.com") {
		t.Fatalf("expected typed forward text above quoted original, got %q", got)
	}
	if !strings.Contains(got, "> forwarded line") {
		t.Fatalf("expected forwarded body to remain quoted, got %q", got)
	}
}
