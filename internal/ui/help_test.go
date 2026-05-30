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
