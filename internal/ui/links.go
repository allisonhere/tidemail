package ui

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	xhtml "golang.org/x/net/html"
)

var httpURLPattern = regexp.MustCompile(`https?://[^\s<>"'\[]+`)
var unsubscribeURIPattern = regexp.MustCompile(`(?i)(?:https?://|mailto:)[^\s<>"'\[]+`)
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
	links, _ := extractHTMLLinks(content, articleURL)
	return links
}

// Explicit anchors also support mail actions. Unlike detected prose URLs,
// their mailto payload must not be trimmed, unescaped again, or rewritten.
func cleanAnchorDestination(raw string) string {
	target := strings.TrimSpace(raw)
	if isMailtoDestination(target) {
		for _, r := range target {
			if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
				return ""
			}
		}
		return target
	}
	return cleanDetectedURL(target)
}

func isMailtoDestination(target string) bool {
	return len(target) > len("mailto:") && strings.EqualFold(target[:len("mailto:")], "mailto:")
}

// Collect destinations and labels together so both use the same normalized DOM.
// The original DOM is needed only for Outlook fallbacks and input captions.
func extractHTMLLinks(content, articleURL string) ([]string, map[string]string) {
	seen := make(map[string]bool)
	var links []string
	labels := make(map[string]string)
	appendLink := func(target string) {
		if target != "" && !seen[target] {
			seen[target] = true
			links = append(links, target)
		}
	}
	setLabel := func(target, label string) {
		if target != "" && label != "" && label != target && labels[target] == "" {
			labels[target] = label
		}
	}
	appendLink(cleanDetectedURL(articleURL))
	// Preserve the existing Reddit policy, including posts inside layouts that
	// generic body normalization intentionally removes.
	for _, link := range extractRedditPostLinks(content) {
		appendLink(link)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(normalizeHTMLForRendering(content)))
	if err != nil {
		return links, labels
	}
	trackers := trackingPixelTargets(content)
	doc.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		target := cleanAnchorDestination(attrFirst(anchor, "href"))
		if trackers[target] {
			return
		}
		appendLink(target)
		setLabel(target, descriptiveLinkLabel(anchor))
	})
	for _, match := range httpURLPattern.FindAllString(visibleHTMLLinkText(doc), -1) {
		appendLink(cleanDetectedURL(match))
	}

	lower := strings.ToLower(content)
	if !strings.Contains(lower, "mso-hide") && !strings.Contains(lower, "<input") {
		return links, labels
	}
	raw, err := goquery.NewDocumentFromReader(strings.NewReader(content))
	if err != nil {
		return links, labels
	}
	raw.Find("script,style,noscript,template,head,meta,link").Remove()
	raw.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		target := cleanAnchorDestination(attrFirst(anchor, "href"))
		if target == "" {
			return
		}
		msoHidden, visible := fallbackLabelVisibility(anchor)
		if !visible {
			return
		}
		if msoHidden {
			appendLink(target)
		}
		if !seen[target] || labels[target] != "" {
			return
		}
		var button string
		anchor.Find("input").EachWithBreak(func(_ int, input *goquery.Selection) bool {
			kind := strings.ToLower(strings.TrimSpace(attrFirst(input, "type")))
			if kind != "button" && kind != "submit" {
				return true
			}
			if _, visible := fallbackLabelVisibility(input); !visible {
				return true
			}
			button = cleanLinkLabel(attrFirst(input, "value"))
			return button == ""
		})
		if !msoHidden && button == "" {
			return
		}
		label := descriptiveLinkLabel(anchor)
		if label == "" {
			label = button
		}
		setLabel(target, label)
	})
	return links, labels
}

// Only text nodes contribute bare URLs. Ordinary inline markup stays continuous;
// anchor, block and line boundaries keep separate displayed URLs apart.
// Image descriptions are labels, not another source of bare destinations.
func visibleHTMLLinkText(doc *goquery.Document) string {
	var out strings.Builder
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.TextNode {
			out.WriteString(n.Data)
			return
		}
		if n.Type != xhtml.ElementNode && n.Type != xhtml.DocumentNode {
			return
		}
		block := false
		switch n.Data {
		case "a", "address", "article", "aside", "blockquote", "br", "dd", "div", "dl", "dt", "fieldset", "figcaption", "figure", "footer", "form", "h1", "h2", "h3", "h4", "h5", "h6", "header", "hr", "li", "main", "nav", "ol", "p", "pre", "section", "table", "tbody", "td", "tfoot", "th", "thead", "tr", "ul", "img":
			block = true
		}
		if block {
			out.WriteByte('\n')
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
		if block {
			out.WriteByte('\n')
		}
	}
	for _, root := range doc.Find("body").Nodes {
		walk(root)
	}
	return out.String()
}

// Reddit's specialized renderer supplies its own targets; reuse HTML label
// recovery without adding destinations outside that renderer's policy.
func extractLinkLabels(content string, targets []string) map[string]string {
	if len(targets) == 0 {
		return nil
	}
	_, available := extractHTMLLinks(content, "")
	labels := make(map[string]string)
	for _, target := range targets {
		if label := available[target]; label != "" {
			labels[target] = label
		}
	}
	return labels
}

func cleanLinkLabel(text string) string { return sanitizePluginLine(unescapeDisplayText(text)) }

func descriptiveLinkLabel(sel *goquery.Selection) string {
	label := cleanLinkLabel(sel.Text())
	if label == "" {
		images := sel.Find("img")
		if sel.Is("img") {
			images = sel
		}
		images.EachWithBreak(func(_ int, image *goquery.Selection) bool {
			if alt := cleanLinkLabel(attrFirst(image, "alt", "title", "aria-label")); alt != "" {
				label = "[image: " + alt + "]"
				return false
			}
			return true
		})
	}
	if label == "" {
		label = cleanLinkLabel(attrFirst(sel, "aria-label", "title"))
	}
	return label
}

// fallbackLabelVisibility ignores only mso-hide, not general hidden content.
// Parent styles matter because Outlook fallbacks can be hidden on a table cell.
func fallbackLabelVisibility(sel *goquery.Selection) (msoHidden, visible bool) {
	visible = true
	sel.AddSelection(sel.Parents()).EachWithBreak(func(_ int, node *goquery.Selection) bool {
		_, hidden := node.Attr("hidden")
		if hidden || strings.EqualFold(strings.TrimSpace(attrFirst(node, "aria-hidden")), "true") {
			visible = false
			return false
		}
		style := parseInlineStyle(attrFirst(node, "style"))
		if strings.Contains(style["mso-hide"], "all") {
			msoHidden = true
		}
		delete(style, "mso-hide")
		if hiddenByInlineStyle(style) {
			visible = false
			return false
		}
		return true
	})
	return msoHidden, visible
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

// firstOSC8Target returns the first hyperlink URI carried by an OSC 8 escape on
// line, or "" when there is none.
//
// The sequence is ESC ] 8 ; <params> ; <uri> ST, where ST is either ESC \ or
// BEL. The closing half of a pair carries an empty URI and is skipped, as is a
// sequence that never terminates — a mangled line must not swallow the rest of
// the content. Reading these back matters because ansi.Strip removes the whole
// escape, so a link rendered this way has no URL left in its visible text.
func firstOSC8Target(line string) string {
	const intro = "\x1b]8;"
	rest := line
	for {
		i := strings.Index(rest, intro)
		if i < 0 {
			return ""
		}
		rest = rest[i+len(intro):]
		// Params end at the next ';'. OSC 8 separates its own params with ':',
		// so the first ';' always closes the params section.
		semi := strings.IndexByte(rest, ';')
		if semi < 0 {
			return ""
		}
		rest = rest[semi+1:]

		end, termLen := -1, 0
		if j := strings.Index(rest, "\x1b\\"); j >= 0 {
			end, termLen = j, 2
		}
		if j := strings.IndexByte(rest, '\x07'); j >= 0 && (end < 0 || j < end) {
			end, termLen = j, 1
		}
		if end < 0 {
			return ""
		}
		uri := rest[:end]
		rest = rest[end+termLen:]
		if uri != "" {
			return uri
		}
	}
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
	if host != "reddit.com" && host != "www.reddit.com" && host != "old.reddit.com" {
		return raw
	}
	path := u.EscapedPath()
	if strings.HasPrefix(path, "/mail/notification_off/") || strings.HasPrefix(path, "/mail/unsubscribe/") {
		return ""
	}
	if cleanedPath := cleanRedditPostPath(path); cleanedPath != "" {
		u.Path = cleanedPath
		u.RawPath = ""
	}
	u.Scheme = "https"
	u.Host = "www.reddit.com"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func cleanRedditPostPath(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 5 || parts[0] != "r" || parts[2] != "comments" {
		return ""
	}
	keep := parts[:5]
	if len(parts) > 5 && isLikelyRedditCommentID(parts[5]) {
		keep = parts[:6]
	}
	return "/" + strings.Join(keep, "/") + "/"
}

func isLikelyRedditCommentID(s string) bool {
	if len(s) < 5 {
		return false
	}
	for _, r := range s {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') {
			continue
		}
		return false
	}
	return true
}

var labelHostPattern = regexp.MustCompile(`(?i)\b(?:https?://)?((?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,})\b`)

// linkHost returns the lower-case host of an http(s) destination without a
// leading "www.", or "" when link has no host.
func linkHost(link string) string {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || u.Hostname() == "" {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}

// linkDestinationSuffix is the " (host)" tail shown after a link's label.
func linkDestinationSuffix(link string) string {
	if host := linkHost(link); host != "" {
		return " (" + host + ")"
	}
	return ""
}

// linkLabelMismatch reports whether label names a domain that is not the
// destination's host or one of its subdomains/parents, the classic phishing
// shape: a label reading "paypal.com" over a link to somewhere else.
func linkLabelMismatch(label, link string) bool {
	dest := linkHost(link)
	if dest == "" {
		return false
	}
	for _, m := range labelHostPattern.FindAllStringSubmatch(label, -1) {
		named := strings.TrimPrefix(strings.ToLower(m[1]), "www.")
		if named == dest || strings.HasSuffix(dest, "."+named) || strings.HasSuffix(named, "."+dest) {
			continue
		}
		return true
	}
	return false
}

// trackingPixelTargets returns the destinations whose only anchors are
// open-tracking stubs: no visible text and only tiny or hidden images with no
// alt text. Senders wrap a 1x1 pixel in a link to log that a message was
// opened; listing it would make the first LINKS entry a tracker. It reads the
// original HTML because normalization for display discards those images. A
// destination that also has a normal anchor is kept.
func trackingPixelTargets(content string) map[string]bool {
	if !strings.Contains(strings.ToLower(content), "<img") {
		return nil
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(content))
	if err != nil {
		return nil
	}
	pixel := make(map[string]bool)
	visible := make(map[string]bool)
	doc.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		target := cleanAnchorDestination(attrFirst(anchor, "href"))
		if target == "" {
			return
		}
		if isTrackingPixelAnchor(anchor) {
			pixel[target] = true
		} else {
			visible[target] = true
		}
	})
	for target := range visible {
		delete(pixel, target)
	}
	return pixel
}

func isTrackingPixelAnchor(anchor *goquery.Selection) bool {
	if cleanLinkLabel(anchor.Text()) != "" || cleanLinkLabel(attrFirst(anchor, "aria-label", "title")) != "" {
		return false
	}
	images := anchor.Find("img")
	if images.Length() == 0 || anchor.Find("input,svg,picture,video,canvas").Length() > 0 {
		return false
	}
	pixelOnly := true
	images.EachWithBreak(func(_ int, img *goquery.Selection) bool {
		hidden := hiddenByInlineStyle(parseInlineStyle(attrFirst(img, "style")))
		if cleanLinkLabel(attrFirst(img, "alt", "title", "aria-label")) != "" || (!hidden && !isTrackingImage(img)) {
			pixelOnly = false
			return false
		}
		return true
	})
	return pixelOnly
}
