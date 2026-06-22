package ui

import (
	"html"
	"strings"
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
