package ui

import (
	"html"
	"strings"
)

func unescapeDisplayText(s string) string {
	return strings.Join(strings.Fields(html.UnescapeString(s)), " ")
}
