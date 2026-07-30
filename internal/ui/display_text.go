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

// normalizeHardBreaks neutralizes runes the line-based viewport layout can't
// measure the way a terminal renders them. Terminals disagree with the width
// libraries here on two fronts:
//
//   - Line/paragraph separators (U+2028, U+2029, VT, FF, NEL) measure as a
//     zero/one-width rune, but many terminals honor them as an actual line break.
//     Left intact, a single content line carrying one renders as two terminal
//     lines, overflows the pane, and shifts the whole frame. They become "\n" so
//     clampView can split, measure, and pad each real line correctly.
//   - The replacement char U+FFFD (from undecodable bytes) measures as width 0 in
//     x/ansi but occupies one cell in the terminal, so each one shifts its line a
//     cell right. It becomes "?" — width 1 everywhere, and an honest stand-in for
//     a byte that already failed to decode.
//
// It runs on the fully assembled, ANSI-styled content, so it must not touch ESC
// (U+001B) or any byte an SGR sequence uses; only the runes below are remapped.
func normalizeHardBreaks(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case 0x2028, // LINE SEPARATOR
			0x2029, // PARAGRAPH SEPARATOR
			'\v',   // VERTICAL TAB
			'\f',   // FORM FEED
			0x0085: // NEXT LINE (NEL)
			return '\n'
		case 0xFFFD: // REPLACEMENT CHARACTER (x/ansi measures width 0)
			return '?'
		}
		return r
	}, s)
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
		case r == 0xfe0f, r == 0x200d, r == 0x20e3:
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
	switch {
	case first >= 0x1f300 && first <= 0x1faff:
		// Misc pictographs, emoticons, transport, supplemental & extended-A symbols.
		return true
	case first >= 0x1f000 && first <= 0x1f0ff:
		// Mahjong tiles, dominoes, playing cards — width-2 and repaint like emoji.
		return true
	}
	// Deliberately not a blanket `first >= 0x1f000`: that swallowed enclosed
	// alphanumerics (U+1F100–1F1DF) and the legacy-computing block (U+1FB00+),
	// whose sextants and box characters are used in ASCII-art mail.
	return width == 2 && unicode.Is(unicode.So, first)
}
