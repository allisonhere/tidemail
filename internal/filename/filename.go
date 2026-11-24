// Package filename makes attachment names safe to create in a directory.
package filename

import (
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Safe reduces an attachment name to a lowercase filename of letters, digits,
// '.', '_' and '-', keeping non-ASCII letters, digits and marks. Whitespace runs
// become one '-'; other characters, including path separators, are dropped.
// It never returns "", "." or "..", and cuts long names to 100 bytes on a
// UTF-8 boundary, keeping an extension of up to 10 bytes.
func Safe(name string) string {
	var b strings.Builder
	sep := func() {
		if b.Len() > 0 {
			s := b.String()
			if s[len(s)-1] != '-' {
				b.WriteByte('-')
			}
		}
	}
	for _, r := range name {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + 'a' - 'A')
		case unicode.IsSpace(r) || r == '-':
			sep()
		case r >= utf8.RuneSelf && (unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r)):
			// Keep non-ASCII names (Chinese, accented, ...) readable.
			b.WriteRune(unicode.ToLower(r))
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" || s == "." || s == ".." {
		s = "attachment"
	}
	if len(s) > 100 {
		// Cut on a rune boundary, keeping a short extension.
		ext := filepath.Ext(s)
		if len(ext) > 10 {
			ext = ""
		}
		stem := s[:len(s)-len(ext)]
		limit := 100 - len(ext)
		for limit > 0 && !utf8.RuneStart(stem[limit]) {
			limit--
		}
		s = stem[:limit] + ext
	}
	return s
}
