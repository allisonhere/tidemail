package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestRenderHelpDocumentsCredentialSafety(t *testing.T) {
	view := ansi.Strip(renderHelp(100, BuildStyles(CatppuccinMocha, "comfortable"), DefaultKeys))
	for _, want := range []string{"OAuth2", "keychain", "redacted"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected help to document credential safety term %q, got %q", want, view)
		}
	}
}

func TestRenderHelpDocumentsContactsAndNotifications(t *testing.T) {
	view := ansi.Strip(renderHelp(100, BuildStyles(CatppuccinMocha, "comfortable"), DefaultKeys))
	for _, want := range []string{"contacts", "autocomplete", "vCard", "desktop notifications", "compose to selected contact"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected help to document contacts/notifications term %q, got %q", want, view)
		}
	}
}
