package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/allisonhere/tidemail/internal/config"
)

// sgr turns a theme color into the truecolor escape lipgloss emits for it, so a
// test can tell a foreground from a fill without hardcoding channel values.
func sgr(t *testing.T, layer int, c lipgloss.Color) string {
	t.Helper()
	var r, g, b int
	if _, err := fmt.Sscanf(string(c), "#%02x%02x%02x", &r, &g, &b); err != nil {
		t.Fatalf("parse color %q: %v", c, err)
	}
	return fmt.Sprintf("%d;2;%d;%d;%d", layer, r, g, b)
}

func foreground(t *testing.T, c lipgloss.Color) string { return sgr(t, 38, c) }
func background(t *testing.T, c lipgloss.Color) string { return sgr(t, 48, c) }

// lineContaining returns the rendered line whose visible text contains want.
func lineContaining(t *testing.T, view, want string) string {
	t.Helper()
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(ansi.Strip(line), want) {
			return line
		}
	}
	t.Fatalf("no rendered line contains %q, got %q", want, ansi.Strip(view))
	return ""
}

func trueColor(t *testing.T) {
	t.Helper()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
}

// A validation failure is the reason a save did not happen, so it is a filled
// red label rather than one more line of colored text.
func TestAccountManagerFailureStatusIsFilledRed(t *testing.T) {
	trueColor(t)

	am := newFromFormAccountManager()
	am.fromInput.SetValue("David Blangstrup")
	am, _, _ = am.submitForm()
	chrome := newManagerChrome(80, CatppuccinMocha, false)

	line := lineContaining(t, am.viewForm(80, 40, chrome), "FROM NEEDS AN EMAIL ADDRESS")

	if !strings.Contains(line, background(t, chrome.errorFg)) {
		t.Fatalf("expected the failure label to be filled with the error color, got %q", line)
	}
	if !strings.Contains(line, foreground(t, contrastFg(chrome.errorFg))) {
		t.Fatalf("expected the failure label to use a contrasting foreground, got %q", line)
	}
	// The fill hugs the message; the rest of the row stays on the form's own
	// background rather than becoming a full-width red bar.
	if !strings.Contains(line, background(t, chrome.baseBg)) {
		t.Fatalf("expected the failure label to stop short of the full width, got %q", line)
	}
}

// A duplicate name does not stop the save, so it must not be dressed as one.
func TestAccountManagerNoteStatusStaysPlain(t *testing.T) {
	trueColor(t)

	am := newFromFormAccountManager()
	am.statusMsg = "NOTE: ANOTHER ACCOUNT IS ALSO NAMED PERSONAL"
	chrome := newManagerChrome(80, CatppuccinMocha, false)

	line := lineContaining(t, am.viewForm(80, 40, chrome), "ANOTHER ACCOUNT IS ALSO NAMED")

	if strings.Contains(line, background(t, chrome.errorFg)) {
		t.Fatalf("expected a note to stay unfilled, got %q", line)
	}
}

// A confirmation is not a failure either.
func TestAccountManagerSuccessStatusStaysPlain(t *testing.T) {
	trueColor(t)

	am := newFromFormAccountManager()
	am.statusMsg = "SAVED: PERSONAL"
	chrome := newManagerChrome(80, CatppuccinMocha, false)

	line := lineContaining(t, am.viewForm(80, 40, chrome), "SAVED: PERSONAL")

	if strings.Contains(line, background(t, chrome.errorFg)) {
		t.Fatalf("expected a confirmation to stay unfilled, got %q", line)
	}
	if !strings.Contains(line, foreground(t, chrome.successFg)) {
		t.Fatalf("expected a confirmation to keep the success color, got %q", line)
	}
}

// The account list shows the same failures the form does — a save that failed
// against the server arrives after the form has closed — so it has to redact
// them the same way.
func TestAccountManagerListStatusRedactsPassword(t *testing.T) {
	am := NewAccountManager(nil)
	am.mode = amList
	am.configs = []config.AccountConfig{{ID: "a1", Name: "Personal", Password: "mail-secret"}}
	am.statusMsg = "SAVE FAILED: LOGIN mail-secret rejected"
	chrome := newManagerChrome(80, CatppuccinMocha, false)

	view := am.viewList(80, 30, chrome, BuildStyles(CatppuccinMocha, "compact", "square"))

	if strings.Contains(view, "mail-secret") {
		t.Fatalf("expected the account list status to hide the password, got %q", ansi.Strip(view))
	}
	if !strings.Contains(ansi.Strip(view), "[redacted]") {
		t.Fatalf("expected the account list status to show the redaction marker, got %q", ansi.Strip(view))
	}
}
