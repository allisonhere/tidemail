package ui

import "github.com/allisonhere/tide/internal/greader"

// unescapeDisplayText decodes HTML entities in titles and labels shown in the TUI.
// Feeds may come from the local DB (RSS parser), GReader JSON, or prefs — any path can
// still contain sequences like &#039; or &amp;#039;.
func unescapeDisplayText(s string) string {
	return greader.UnescapeAPIString(s)
}
