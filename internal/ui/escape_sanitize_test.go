package ui

import (
	"strings"
	"testing"
)

// TestRenderStripsTerminalEscapes guards against terminal escape-sequence
// injection: untrusted email content rendered to the terminal must not carry
// raw control bytes (ESC/BEL/DEL) that the emulator would interpret (OSC 52
// clipboard hijack, UI spoofing, cursor control).
func TestRenderStripsTerminalEscapes(t *testing.T) {
	// Raw control bytes embedded directly in a plain-text body.
	body := "Hello\x1b]52;c;ZXZpbAo=\x07world\x1b[2J done\x7f"
	out := formatArticleBody(body, 80, Theme{}, true)
	for _, r := range []rune{'\x1b', '\x07', '\x7f'} {
		if strings.ContainsRune(out, r) {
			t.Errorf("body render leaked control byte %#x: %q", r, out)
		}
	}

	// HTML entity-encoded escape (&#27; -> ESC) must be neutralized after the
	// entity is decoded during rendering.
	htmlBody := "<p>click &#27;]8;;http://evil&#7; here</p>"
	htmlOut := renderHTMLBody(htmlBody, 80, Theme{}, true)
	if strings.ContainsRune(htmlOut, '\x1b') || strings.ContainsRune(htmlOut, '\x07') {
		t.Errorf("HTML render leaked decoded escape: %q", htmlOut)
	}
}

// TestSubjectStripsTerminalEscapes covers the message-list path, including the
// entity-decode-then-render case unique to subjects.
func TestSubjectStripsTerminalEscapes(t *testing.T) {
	cases := []string{
		"Subject\x1b[31m injected", // raw ESC
		"Subject &#27;[2J cleared", // entity-encoded ESC
		"alarm\x07bell",            // raw BEL
	}
	for _, in := range cases {
		out := unescapeDisplayText(in)
		if strings.ContainsRune(out, '\x1b') || strings.ContainsRune(out, '\x07') {
			t.Errorf("subject %q rendered with control byte: %q", in, out)
		}
	}
}
