package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestEditorAreaTextareaLikeAPI(t *testing.T) {
	e := newEditorArea()
	e.SetWidth(40)
	e.SetHeight(3)

	// Update applies key messages and returns the updated value (textarea-like).
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello")})
	if got := e.Value(); got != "hello" {
		t.Fatalf("Value after typing = %q, want %q", got, "hello")
	}

	// SetValue replaces content.
	e.SetValue("replaced")
	if got := e.Value(); got != "replaced" {
		t.Fatalf("Value after SetValue = %q", got)
	}

	// Select-all then cut (editor-owned) empties the buffer and returns a
	// clipboard write command (not executed here, so no real shell-out).
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	if got := e.SelectedText(); got != "replaced" {
		t.Fatalf("SelectedText after select-all = %q", got)
	}
	var cmd tea.Cmd
	e, cmd = e.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	if got := e.Value(); got != "" {
		t.Fatalf("Value after cut = %q", got)
	}
	if cmd == nil {
		t.Fatal("cut should return a clipboard write command")
	}
}

func TestEditorAreaFocusBlurAffectsView(t *testing.T) {
	e := newEditorArea()
	e.SetWidth(20)
	e.SetHeight(1)
	e.SetValue("hi")

	e.Focus()
	focused := e.View()
	e.Blur()
	blurred := e.View()

	if focused == blurred {
		t.Fatalf("focused and blurred views should differ (cursor visibility); both = %q", focused)
	}
	if !strings.Contains(blurred, "hi") {
		t.Fatalf("blurred view should still render text, got %q", blurred)
	}
}

func TestEditorAreaPlaceholder(t *testing.T) {
	e := newEditorArea()
	e.SetWidth(40)
	e.SetHeight(2)
	e.SetPlaceholder("Write your message…")

	if got := e.View(); !strings.Contains(got, "rite your message") {
		t.Fatalf("empty editor area should render placeholder, got %q", got)
	}
	e.SetValue("x")
	if got := e.View(); strings.Contains(got, "rite your message") {
		t.Fatalf("placeholder should disappear once text is set, got %q", got)
	}
}
