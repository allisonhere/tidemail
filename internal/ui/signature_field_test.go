package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tidemail/internal/config"
)

func sigForm(t *testing.T, value string) AccountManager {
	t.Helper()
	am := NewAccountManager(nil)
	am.mode = amEdit
	am.sigArea.SetValue(value)
	am.focusField(amFieldSignature)
	return am
}

func sendKey(am AccountManager, msg tea.KeyMsg) AccountManager {
	next, _, _ := am.updateForm(msg, DefaultKeys)
	return next
}

// Enter makes a new line instead of jumping to the next field. This is the
// whole point: the signature used to need a literal \n escape typed into a
// single-line box.
func TestSignatureEnterStartsANewLine(t *testing.T) {
	am := sigForm(t, "")
	for _, r := range "Allie" {
		am = sendKey(am, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	am = sendKey(am, tea.KeyMsg{Type: tea.KeyEnter})
	for _, r := range "alliehere.com" {
		am = sendKey(am, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	if am.focusedField != amFieldSignature {
		t.Fatalf("enter left the signature field, focus = %v", am.focusedField)
	}
	if got, want := am.buildCfg().Signature, "Allie\nalliehere.com"; got != want {
		t.Fatalf("signature = %q, want %q", got, want)
	}
	if strings.Contains(am.buildCfg().Signature, `\n`) {
		t.Fatal("signature still carries a literal backslash-n escape")
	}
}

// A stored multi-line signature must survive a trip through the form unchanged.
func TestSignatureRoundTripsThroughTheForm(t *testing.T) {
	const sig = "Allie\n--\nalliehere.com"
	am := NewAccountManager(nil)
	am.populateFormFrom(config.AccountConfig{Name: "Personal", Signature: sig})
	if got := am.buildCfg().Signature; got != sig {
		t.Fatalf("signature = %q, want %q", got, sig)
	}
}

// Up and down walk the signature's own lines, and leave at its edges.
func TestSignatureArrowsWalkLinesThenLeave(t *testing.T) {
	up := tea.KeyMsg{Type: tea.KeyUp}
	down := tea.KeyMsg{Type: tea.KeyDown}

	// Cursor starts on the last line after SetValue, so down leaves immediately.
	am := sendKey(sigForm(t, "one\ntwo\nthree"), down)
	if am.focusedField != amFieldSyncInterval {
		t.Fatalf("down on the last line should leave the field, focus = %v", am.focusedField)
	}

	// Walking up stays inside until the first line, then leaves.
	am = sendKey(sigForm(t, "one\ntwo\nthree"), up)
	if am.focusedField != amFieldSignature {
		t.Fatalf("up from the last line should stay inside, focus = %v", am.focusedField)
	}
	am = sendKey(am, up)
	if am.focusedField != amFieldSignature {
		t.Fatalf("up to the first line should stay inside, focus = %v", am.focusedField)
	}
	am = sendKey(am, up)
	if am.focusedField != amFieldFrom {
		t.Fatalf("up from the first line should leave, focus = %v", am.focusedField)
	}
}

// Tab is the reliable way out from anywhere in the box.
func TestSignatureTabLeavesFromTheMiddle(t *testing.T) {
	am := sendKey(sigForm(t, "one\ntwo\nthree"), tea.KeyMsg{Type: tea.KeyUp})
	if am.focusedField != amFieldSignature {
		t.Fatal("expected to still be in the signature")
	}
	am = sendKey(am, tea.KeyMsg{Type: tea.KeyTab})
	if am.focusedField != amFieldSyncInterval {
		t.Fatalf("tab did not leave the signature, focus = %v", am.focusedField)
	}
}

// Adding an account after editing one must not inherit its signature.
func TestResetFormClearsTheSignature(t *testing.T) {
	am := NewAccountManager(nil)
	am.populateFormFrom(config.AccountConfig{Name: "Personal", Signature: "Allie\n--\nalliehere.com"})
	am.resetForm()
	if got := am.buildCfg().Signature; got != "" {
		t.Fatalf("signature carried over into a new account: %q", got)
	}
}

// The box is the tallest control in a form that already scrolls.
func TestSignatureFieldFitsNarrowForm(t *testing.T) {
	const width = 56
	am := sigForm(t, "Allie\n--\nalliehere.com")
	body := ansi.Strip(am.View(width, 40, BuildStyles(CatppuccinMocha, "compact", "square")))
	for i, line := range strings.Split(body, "\n") {
		if w := ansi.StringWidth(line); w > width {
			t.Fatalf("line %d is %d cells wide, want <= %d: %q", i+1, w, width, line)
		}
	}
	if !strings.Contains(body, "alliehere.com") {
		t.Fatalf("signature lines not rendered:\n%s", body)
	}
}
