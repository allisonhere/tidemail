package ui

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var httpURLPattern = regexp.MustCompile(`https?://[^\s<>"']+`)
var unsubscribeURIPattern = regexp.MustCompile(`(?i)(?:https?://|mailto:)[^\s<>"']+`)
var redirectParamNames = []string{"url", "u", "target", "redirect", "redirect_url"}

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
	for _, link := range extractRedditPostLinks(content) {
		appendLink(link)
	}
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
	if target, ok := unwrapRedditClickURL(clean); ok {
		return target
	}
	if target, ok := unwrapGenericRedirectURL(clean); ok {
		return target
	}
	if isRedditTrackingURL(clean) {
		return ""
	}
	if strings.HasPrefix(clean, "http://") || strings.HasPrefix(clean, "https://") {
		return cleanRedditURL(clean)
	}
	return ""
}

func unwrapRedditClickURL(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	if !strings.EqualFold(u.Hostname(), "click.redditmail.com") {
		return "", false
	}
	parts := strings.Split(strings.TrimLeft(u.EscapedPath(), "/"), "/")
	if len(parts) < 2 || !strings.EqualFold(parts[0], "CL0") {
		return "", false
	}
	target, err := url.PathUnescape(parts[1])
	if err != nil || target == "" {
		return "", false
	}
	return cleanRedditURL(target), true
}

func unwrapGenericRedirectURL(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	values := u.Query()
	for _, name := range redirectParamNames {
		for _, value := range values[name] {
			if cleaned := cleanRedirectTarget(value); cleaned != "" {
				return cleaned, true
			}
		}
	}
	return "", false
}

func cleanRedirectTarget(raw string) string {
	target := strings.TrimSpace(raw)
	if target == "" {
		return ""
	}
	if unescaped, err := url.QueryUnescape(target); err == nil {
		target = strings.TrimSpace(unescaped)
	}
	if unescaped, err := url.PathUnescape(target); err == nil {
		target = strings.TrimSpace(unescaped)
	}
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		return ""
	}
	target = strings.TrimRight(target, ".,;:!?)]}\"'")
	if reddit := cleanRedditURL(target); reddit != target {
		return reddit
	}
	return target
}

func isRedditTrackingURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "click.redditmail.com" || host == "www.redditstatic.com"
}

func cleanRedditURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	host := strings.ToLower(u.Hostname())
	if host != "reddit.com" && host != "www.reddit.com" {
		return raw
	}
	path := u.EscapedPath()
	if strings.HasPrefix(path, "/mail/notification_off/") || strings.HasPrefix(path, "/mail/unsubscribe/") {
		return ""
	}
	u.Scheme = "https"
	u.Host = "www.reddit.com"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}
