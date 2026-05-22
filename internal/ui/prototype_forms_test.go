package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tide/internal/config"
)

func TestPrototypeFormsInitialViewShowsSettings(t *testing.T) {
	m := NewPrototypeFormsModel(config.DefaultConfig())
	next, _ := m.Update(tea.WindowSizeMsg{Width: 96, Height: 28})
	m = next.(PrototypeFormsModel)

	view := ansi.Strip(m.View())

	for _, want := range []string{"FORM LAB", "SETTINGS", "Display", "Updates", "SAVE"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected prototype view to contain %q, got %q", want, view)
		}
	}
}

func TestPrototypeFormsSwitchesScreens(t *testing.T) {
	m := NewPrototypeFormsModel(config.DefaultConfig())
	next, _ := m.Update(tea.WindowSizeMsg{Width: 96, Height: 28})
	m = next.(PrototypeFormsModel)

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = next.(PrototypeFormsModel)
	if view := ansi.Strip(m.View()); !strings.Contains(view, "MANAGE ACCOUNT") || !strings.Contains(view, "TEST") {
		t.Fatalf("expected account prototype, got %q", view)
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	m = next.(PrototypeFormsModel)
	if view := ansi.Strip(m.View()); !strings.Contains(view, "COMPOSE") || !strings.Contains(view, "SEND") {
		t.Fatalf("expected compose prototype, got %q", view)
	}
}

func TestPrototypeFormsKeepsPrimaryActionsVisibleInSmallTerminal(t *testing.T) {
	m := NewPrototypeFormsModel(config.DefaultConfig())
	next, _ := m.Update(tea.WindowSizeMsg{Width: 54, Height: 16})
	m = next.(PrototypeFormsModel)

	cases := map[rune]string{
		'1': "SAVE",
		'2': "TEST",
		'3': "SEND",
	}
	for key, primary := range cases {
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		m = next.(PrototypeFormsModel)
		view := ansi.Strip(m.View())
		if !strings.Contains(view, primary) {
			t.Fatalf("expected primary action %q to stay visible on screen %q, got %q", primary, string(key), view)
		}
		if !strings.Contains(view, "CANCEL") {
			t.Fatalf("expected cancel action to stay visible on screen %q, got %q", string(key), view)
		}
	}
}

func TestPrototypeAccountScreenStaysDenseAtNarrowWidth(t *testing.T) {
	m := NewPrototypeFormsModel(config.DefaultConfig())
	next, _ := m.Update(tea.WindowSizeMsg{Width: 64, Height: 20})
	m = next.(PrototypeFormsModel)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = next.(PrototypeFormsModel)

	view := ansi.Strip(m.View())

	if strings.Contains(view, "Server Details") {
		t.Fatalf("expected account prototype to use one compact card at narrow widths, got %q", view)
	}
	for _, broken := range []string{"advanced details second", "production."} {
		if strings.Contains(view, broken) {
			t.Fatalf("expected account prototype to avoid wrapped prose %q, got %q", broken, view)
		}
	}
	if strings.Count(view, "\n") > 19 {
		t.Fatalf("expected account prototype to fit within terminal height, got %d lines in %q", strings.Count(view, "\n")+1, view)
	}
}

func TestPrototypeAccountScreenShowsSecondHRAccount(t *testing.T) {
	m := NewPrototypeFormsModel(config.DefaultConfig())
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(PrototypeFormsModel)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = next.(PrototypeFormsModel)

	view := ansi.Strip(m.View())

	for _, want := range []string{"alliehere.com", "HR", "hr@example.com", "imap.hr.example.com:993", "smtp.hr.example.com:587"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected account prototype to include %q, got %q", want, view)
		}
	}
	if strings.Index(view, "HR") < strings.Index(view, "alliehere.com") {
		t.Fatalf("expected HR account below first account, got %q", view)
	}
}
