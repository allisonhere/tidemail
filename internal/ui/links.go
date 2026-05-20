package ui

import (
	"regexp"
	"strings"
)

var httpURLPattern = regexp.MustCompile(`https?://[^\s<>"']+`)

func extractActionableLinks(content, articleURL string) []string {
	seen := map[string]struct{}{}
	links := make([]string, 0, 8)

	appendLink := func(raw string) {
		link := cleanDetectedURL(raw)
		if link == "" {
			return
		}
		if _, exists := seen[link]; exists {
			return
		}
		seen[link] = struct{}{}
		links = append(links, link)
	}

	appendLink(articleURL)
	for _, match := range httpURLPattern.FindAllString(content, -1) {
		appendLink(match)
	}
	return links
}

func filterLinksFromContent(content string) string {
	return httpURLPattern.ReplaceAllString(content, "")
}

func cleanDetectedURL(raw string) string {
	clean := strings.TrimSpace(raw)
	clean = strings.TrimRight(clean, ".,;:!?)]}\"'")
	if strings.HasPrefix(clean, "http://") || strings.HasPrefix(clean, "https://") {
		return clean
	}
	return ""
}
