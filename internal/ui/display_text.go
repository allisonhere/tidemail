package ui

import (
	"html"
	"strings"
	"unicode"

	"github.com/rivo/uniseg"
)

func unescapeDisplayText(s string) string {
	// Decode entities first, then drop control bytes the terminal would treat as
	// escape sequences — decoding can turn &#27; into a raw ESC, so the strip
	// must happen after. Controls become spaces, which Fields then collapses.
	cleaned := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, html.UnescapeString(s))
	return strings.Join(strings.Fields(cleaned), " ")
}

// messageListDisplayText removes emoji grapheme clusters from list subjects.
// The original subject used by storage, search, and the opened message is never
// changed. Keeping emoji out of the frequently repainted list avoids terminal
// width disagreements and color-emoji repaint artifacts.
func messageListDisplayText(s string) string {
	return strings.Join(strings.Fields(stripEmojiGraphemes(s)), " ")
}

// stripEmojiGraphemes preserves whitespace and ANSI sequences so it can also
// sanitize the fully rendered content preview without disturbing its layout or
// styling.
func stripEmojiGraphemes(s string) string {
	graphemes := uniseg.NewGraphemes(s)
	var out strings.Builder
	for graphemes.Next() {
		if !isEmojiGrapheme(graphemes.Runes(), graphemes.Width()) {
			out.WriteString(graphemes.Str())
		}
	}
	return out.String()
}

func isEmojiGrapheme(runes []rune, width int) bool {
	for _, r := range runes {
		switch {
		case r == '\ufe0f', r == '\u200d', r == '\u20e3':
			return true
		case r >= 0x1f1e6 && r <= 0x1f1ff: // regional-indicator flags
			return true
		case r >= 0x1f3fb && r <= 0x1f3ff: // skin-tone modifiers
			return true
		}
	}
	if len(runes) == 0 {
		return false
	}
	first := runes[0]
	return first >= 0x1f000 || (width == 2 && unicode.Is(unicode.So, first))
}
