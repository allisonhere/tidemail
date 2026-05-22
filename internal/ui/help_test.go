package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestRenderHelpDocumentsCredentialSafety(t *testing.T) {
	view := ansi.Strip(renderHelp(100, BuildStyles(CatppuccinMocha, "comfortable"), DefaultKeys))
	for _, want := range []string{"app passwords", "0600", "redacted"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected help to document credential safety term %q, got %q", want, view)
		}
	}
}
