package ui

import "html"

func unescapeDisplayText(s string) string {
	return html.UnescapeString(s)
}
