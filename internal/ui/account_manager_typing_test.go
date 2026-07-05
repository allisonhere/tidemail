package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Regression: Up/Down are bound to the vim runes k/j as well as the arrows.
// Those runes must insert characters while a text field is focused —
// "outlook.com" contains a 'k' and used to jump focus to the next field
// mid-address instead of typing it.
func TestFormTextFieldsAcceptVimNavRunes(t *testing.T) {
	am := NewAccountManager(nil)
	am.mode = amAdd
	am.provider = "Outlook"
	am.focusField(amFieldUser)

	addr := "jkjk.longname@outlook.com"
	cur := am
	for _, r := range addr {
		next, _, _ := cur.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}, DefaultKeys)
		cur = next
	}
	if got := cur.userInput.Value(); got != addr {
		t.Fatalf("email field = %q, want %q", got, addr)
	}
	if cur.focusedField != amFieldUser {
		t.Fatalf("typing letters must not move focus, now on %v", cur.focusedField)
	}

	// Arrow keys must still navigate away from a text field…
	next, _, _ := cur.Update(tea.KeyMsg{Type: tea.KeyDown}, DefaultKeys)
	if next.focusedField == amFieldUser {
		t.Fatal("down arrow should leave the text field")
	}

	// …and j/k still navigate on non-text rows (provider picker).
	next.focusField(amFieldProvider)
	next, _, _ = next.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}, DefaultKeys)
	if next.focusedField == amFieldProvider {
		t.Fatal("j should navigate when a non-text row is focused")
	}
}
