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

func TestRenderHelpDocumentsSettingsCtrlSSave(t *testing.T) {
	view := ansi.Strip(renderHelp(100, BuildStyles(CatppuccinMocha, "comfortable"), DefaultKeys))
	settingsStart := strings.Index(view, "Settings Modal")
	if settingsStart < 0 {
		t.Fatalf("expected Settings Modal section, got %q", view)
	}
	settingsEnd := strings.Index(view[settingsStart+len("Settings Modal"):], "Pickers And Overlays")
	settingsSection := view[settingsStart:]
	if settingsEnd >= 0 {
		settingsSection = view[settingsStart : settingsStart+len("Settings Modal")+settingsEnd]
	}

	if !strings.Contains(settingsSection, "ctrl+s") || !strings.Contains(settingsSection, "save settings") {
		t.Fatalf("expected Settings help to document ctrl+s save, got %q", settingsSection)
	}
	if strings.Contains(settingsSection, "save and close") {
		t.Fatalf("expected Settings help not to document escape as save-and-close, got %q", settingsSection)
	}
}

func TestRenderHelpDocumentsNativeMessageSelection(t *testing.T) {
	view := ansi.Strip(renderHelp(100, BuildStyles(CatppuccinMocha, "comfortable"), DefaultKeys))
	for _, want := range []string{"v/V", "visual select", "y/ctrl+c", "copy selected message text", "`"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected help to document native message copy term %q, got %q", want, view)
		}
	}
	if strings.Contains(view, "v                  open AI summary overlay") {
		t.Fatalf("expected AI summary to move off v, got %q", view)
	}
}
