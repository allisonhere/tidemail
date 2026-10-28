package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestLinkLabelMismatch(t *testing.T) {
	for _, tc := range []struct {
		label, link string
		want        bool
	}{
		{"Verify your account at paypal.com", "https://evil.example/login", true},
		{"https://www.paypal.com/security", "https://paypal.com.evil.example/x", true},
		{"paypal.com", "https://www.paypal.com/login", false},
		{"Sign in at accounts.example.com", "https://example.com/signin", false},
		{"example.com", "https://accounts.example.com/signin", false},
		{"View the UNO Events Calendar", "https://track.example/click?id=1", false},
		{"Unsubscribe", "https://list.example/u/123", false},
		{"see v1.2 notes", "https://example.com/notes", false},
	} {
		if got := linkLabelMismatch(tc.label, tc.link); got != tc.want {
			t.Errorf("linkLabelMismatch(%q, %q) = %v, want %v", tc.label, tc.link, got, tc.want)
		}
	}
}

func TestLinkDestinationSuffix(t *testing.T) {
	if got := linkDestinationSuffix("https://www.Example.com/a?b=1"); got != " (example.com)" {
		t.Fatalf("suffix = %q", got)
	}
	if got := linkDestinationSuffix("mailto:a@example.com"); got != "" {
		t.Fatalf("mailto suffix = %q", got)
	}
}

func TestReaderLinksShowDestinationAndFlagMismatch(t *testing.T) {
	source := `<p><a href="https://evil.example/login">Sign in at paypal.com</a></p>` +
		`<p><a href="https://track.example/c?id=1">Read the report</a></p>`
	m := readerLinksModel(t, source, "")
	footer := ansi.Strip(m.renderContentLinks(m.contentBodyWidth()))
	if !strings.Contains(footer, "! Sign in at paypal.com (evil.example)") {
		t.Fatalf("mismatched label not flagged with its real host: %q", footer)
	}
	if !strings.Contains(footer, "Read the report (track.example)") || strings.Contains(footer, "! Read the report") {
		t.Fatalf("ordinary label should show host without a warning: %q", footer)
	}
	// A narrow pane keeps the destination host and shortens the label instead.
	narrow := ansi.Strip(m.renderContentLinks(32))
	if !strings.Contains(narrow, "(evil.example)") {
		t.Fatalf("narrow row lost its destination: %q", narrow)
	}
}
