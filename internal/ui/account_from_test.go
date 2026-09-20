package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Regression tests for issue #25: an account saved with a display-name-only
// From ("David Blangstrup") passed validation, then every send failed with
// "no sender address" once the message reached SMTP. The form is where that
// has to be caught, because by send time the bad value is already baked into
// the queued message.

// newFromFormAccountManager returns an otherwise-valid Custom account form, so
// the only thing a validation failure can be about is the From field.
func newFromFormAccountManager() AccountManager {
	am := NewAccountManager(nil)
	am.mode = amAdd
	am.nameInput.SetValue("Personal")
	am.imapHostInput.SetValue("imap.example.com")
	am.smtpHostInput.SetValue("smtp.example.com")
	am.userInput.SetValue("alice@example.com")
	return am
}

func TestValidateFormRejectsFromWithoutAddress(t *testing.T) {
	for _, from := range []string{
		"David Blangstrup", // the reported value: a display name, no address
		"Alice",
		"Alice <alice>",        // angle-addr with no domain
		"Alice, Bob <a@b.com>", // two addresses where one belongs
		"alice at example.com", // near-miss for people avoiding "@"
	} {
		am := newFromFormAccountManager()
		am.fromInput.SetValue(from)
		status := am.validateForm(am.buildCfg())
		if status == "" {
			t.Fatalf("expected From %q to fail validation", from)
		}
		if !strings.Contains(status, "FROM") {
			t.Fatalf("From %q: expected the failure to name the field, got %q", from, status)
		}
		if !strings.Contains(status, "@") {
			t.Fatalf("From %q: expected the failure to show the address shape, got %q", from, status)
		}
	}
}

func TestValidateFormAcceptsUsableFrom(t *testing.T) {
	for _, from := range []string{
		"",                                   // optional: send falls back to the username
		"alice@example.com",                  // bare address
		"Alice <alice@example.com>",          // display name plus address
		`"Smith, Alice" <alice@example.com>`, // quoted name with a comma
		"  Alice <alice@example.com>  ",      // surrounding whitespace
	} {
		am := newFromFormAccountManager()
		am.fromInput.SetValue(from)
		if status := am.validateForm(am.buildCfg()); status != "" {
			t.Fatalf("expected From %q to validate, got %q", from, status)
		}
	}
}

// The label and hints have to say an address is required, since the
// placeholder that said so only shows while the field is empty.
func TestAccountManagerFormLabelsFromAsAddress(t *testing.T) {
	am := newFromFormAccountManager()
	am.focusField(amFieldFrom)
	chrome := newManagerChrome(80, CatppuccinMocha, false)

	view := ansi.Strip(am.viewForm(80, 40, chrome))

	if !strings.Contains(view, "From address") {
		t.Fatalf("expected the From row to be labeled as an address, got %q", view)
	}
	if !strings.Contains(view, "you@example.com") {
		t.Fatalf("expected the From row to show the expected address shape, got %q", view)
	}
	if !strings.Contains(view, "Blank sends as the username") {
		t.Fatalf("expected the From row to say blank falls back to the username, got %q", view)
	}
}
