package ui

import (
	"strings"
	"testing"
)

// A U+2028 LINE SEPARATOR embedded in a body line must not survive into laid-out
// content: terminals honor it as a hard line break the width model can't see,
// overflowing the pane and shifting the whole frame.
func TestNormalizeHardBreaks(t *testing.T) {
	seps := []rune{0x2028, 0x2029, '\v', '\f', 0x0085}
	for _, sep := range seps {
		in := "left" + string(sep) + "right"
		got := normalizeHardBreaks(in)
		if strings.ContainsRune(got, sep) {
			t.Errorf("separator U+%04X survived normalizeHardBreaks: %q", sep, got)
		}
		if got != "left\nright" {
			t.Errorf("normalizeHardBreaks(%q) = %q, want %q", in, got, "left\nright")
		}
	}
	// The replacement char (width 0 in x/ansi, one cell in the terminal) becomes
	// a width-1 '?' so it can't shift a line right.
	if got := normalizeHardBreaks("a" + string(rune(0xFFFD)) + "b"); got != "a?b" {
		t.Errorf("normalizeHardBreaks did not neutralize U+FFFD: %q", got)
	}

	// ESC and ordinary runes must be untouched so ANSI styling survives.
	styled := "\x1b[31mred\x1b[0m x"
	if got := normalizeHardBreaks(styled); got != styled {
		t.Errorf("normalizeHardBreaks mangled styled text: %q", got)
	}
}
