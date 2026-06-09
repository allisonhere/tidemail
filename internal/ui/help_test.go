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

func TestRenderHelpDocumentsAccountManagerShortcutAsUppercaseM(t *testing.T) {
	view := ansi.Strip(renderHelp(100, BuildStyles(CatppuccinMocha, "comfortable"), DefaultKeys))
	if !strings.Contains(view, "M accounts") {
		t.Fatalf("expected help to document uppercase account shortcut, got %q", view)
	}
	if strings.Contains(view, "m accounts") {
		t.Fatalf("expected help not to document lowercase account shortcut, got %q", view)
	}
}

func TestRenderHelpScopesModalShortcuts(t *testing.T) {
	view := ansi.Strip(renderHelp(100, BuildStyles(CatppuccinMocha, "comfortable"), DefaultKeys))
	for _, want := range []string{"Compose Modal", "ctrl+g", "Account Manager Modal", "Contact Manager Modal", "Filters Modal"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected help to include scoped shortcut term %q, got %q", want, view)
		}
	}
	if strings.Contains(view, "Messages / Content") || strings.Contains(view, "Accounts / Contacts") {
		t.Fatalf("expected mixed help sections to be replaced with scoped modal sections, got %q", view)
	}
}
