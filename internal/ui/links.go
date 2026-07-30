package ui

import (
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var httpURLPattern = regexp.MustCompile(`https?://[^\s<>"']+`)
var unsubscribeURIPattern = regexp.MustCompile(`(?i)(?:https?://|mailto:)[^\s<>"']+`)

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

func extractActionableLinksFromHTML(content, articleURL string) []string {
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
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(normalizeHTMLForRendering(content)))
	if err == nil {
		doc.Find("a[href], img[src], img[data-src]").Each(func(_ int, s *goquery.Selection) {
			appendLink(attrFirst(s, "href", "src", "data-src"))
		})
	}
	for _, match := range httpURLPattern.FindAllString(content, -1) {
		appendLink(match)
	}
	return links
}

func filterLinksFromContent(content string) string {
	return httpURLPattern.ReplaceAllString(content, "")
}

var markdownLinkRe = regexp.MustCompile(`\[([^\]]*)\]\((https?://[^)\s]*)\)`)

// filterLinksFromMarkdown removes link targets while keeping the link text, so
// a filtered HTML message still reads as prose instead of losing its wording.
func filterLinksFromMarkdown(s string) string {
	s = markdownLinkRe.ReplaceAllString(s, "$1")
	return httpURLPattern.ReplaceAllString(s, "")
}

// maxOSC8URILen caps hyperlink targets. The OSC 8 spec suggests keeping URIs
// under ~2KB, and tracking URLs routinely exceed anything sensible.
const maxOSC8URILen = 2048

// safeOSC8URI reports whether raw can be embedded in an OSC 8 escape sequence.
//
// The URI goes inside a terminal escape, so a control byte in it could
// terminate the sequence early and let untrusted mail inject arbitrary escape
// codes. Body text is already run through stripEmailInvisibles, but link
// targets are harvested straight from href attributes and raw content, so this
// validates independently rather than trusting the caller.
func safeOSC8URI(raw string) bool {
	if raw == "" || len(raw) > maxOSC8URILen {
		return false
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		return false
	}
	for _, r := range raw {
		// Reject C0 controls, DEL, and C1 (0x80-0x9f: ESC-equivalents in some
		// 8-bit terminal modes). Anything non-printable has no business here.
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return false
		}
	}
	return true
}

// osc8Link wraps label in an OSC 8 hyperlink pointing at uri. Terminals that
// do not support OSC 8 ignore the sequence and show the label unchanged.
// Returns label untouched when the URI fails validation or hyperlinks are off.
func osc8Link(uri, label string, plainUI bool) string {
	if plainUI || !safeOSC8URI(uri) {
		return label
	}
	return "\x1b]8;;" + uri + "\x1b\\" + label + "\x1b]8;;\x1b\\"
}

func cleanDetectedURL(raw string) string {
	clean := strings.TrimSpace(raw)
	clean = strings.TrimRight(clean, ".,;:!?)]}\"'")
	if strings.HasPrefix(clean, "http://") || strings.HasPrefix(clean, "https://") {
		return clean
	}
	return ""
}
