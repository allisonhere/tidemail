package ui

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
	xhtml "golang.org/x/net/html"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Body links are styled after glamour has run, by locating their text in the
// finished output rather than by marking it beforehand.
//
// Two approaches are ruled out. Emitting Markdown links does not work because
// glamour renders `[text](url)` as "text url", printing the destination inline,
// and keeping destinations out of the body (they live in the actionable-links
// list) is a deliberate readability decision. Marking the text in the Markdown
// does not work either: a visible marker is measured while wrapping and moves
// the line breaks, while an invisible escape sequence is dropped by glamour.
//
// So buttonLinkRule records each link's text and destination in document order,
// and this pass finds those texts again in the rendered output, in the same
// order, tolerating the whitespace changes that wrapping introduces.

// bodyLink is one link recorded during HTML→Markdown conversion.
type bodyLink struct {
	href string
	text string
}

// bodyLinkCollector holds the links recorded in one render. A converter is
// built per render, so a collector is never shared.
type bodyLinkCollector struct {
	links []bodyLink
}

// record notes a link and returns text unchanged, so the Markdown that glamour
// wraps is exactly what it would have been without link styling.
func (c *bodyLinkCollector) record(href, text string) string {
	if c == nil {
		return text
	}
	href = strings.TrimSpace(href)
	trimmed := strings.TrimSpace(text)
	if href == "" || trimmed == "" {
		return text
	}
	c.links = append(c.links, bodyLink{href: href, text: trimmed})
	return text
}

// visibleIndex maps each byte of the ANSI-stripped view of s back to its offset
// in s, so a match found in the stripped text can be applied to the original.
func visibleIndex(s string) (string, []int) {
	var plain strings.Builder
	offsets := make([]int, 0, len(s))
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			if j := skipEscape(s, i); j > i {
				i = j
				continue
			}
		}
		offsets = append(offsets, i)
		plain.WriteByte(s[i])
		i++
	}
	return plain.String(), offsets
}

// skipEscape returns the offset just past the escape sequence starting at i,
// or i when the bytes are not a sequence it recognizes.
func skipEscape(s string, i int) int {
	if i+1 >= len(s) {
		return i
	}
	switch s[i+1] {
	case '[': // CSI: parameters and intermediates, then a final byte.
		for j := i + 2; j < len(s); j++ {
			if s[j] >= 0x40 && s[j] <= 0x7e {
				return j + 1
			}
		}
	case ']': // OSC: terminated by BEL or ST.
		for j := i + 2; j < len(s); j++ {
			if s[j] == 0x07 {
				return j + 1
			}
			if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
				return j + 2
			}
		}
	}
	return i
}

// matchLinkText finds text in plain at or after from, allowing any run of
// whitespace where the text has a space, since wrapping may have replaced a
// space with a newline and indentation. Returns -1, -1 when absent.
//
// This is a hand-rolled scan rather than a regexp: a newsletter carries dozens
// of links and this runs on every render, so compiling a pattern per link is
// work worth avoiding.
func matchLinkText(plain, text string, from int) (int, int) {
	fields := strings.Fields(text)
	if len(fields) == 0 || from >= len(plain) {
		return -1, -1
	}
	for i := from; i < len(plain); {
		at := strings.Index(plain[i:], fields[0])
		if at < 0 {
			return -1, -1
		}
		start := i + at
		pos := start + len(fields[0])
		matched := true
		for _, field := range fields[1:] {
			gap := pos
			for gap < len(plain) && isSpaceByte(plain[gap]) {
				gap++
			}
			if gap == pos || !strings.HasPrefix(plain[gap:], field) {
				matched = false
				break
			}
			pos = gap + len(field)
		}
		if matched {
			return start, pos
		}
		i = start + 1
	}
	return -1, -1
}

func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// styleBodyLinks colors and underlines each recorded link's text in rendered,
// optionally wrapping it in an OSC 8 hyperlink. Runs split across lines by
// wrapping are styled per line — a style must never span a newline — and
// leading indentation is left outside the styled span.
func styleBodyLinks(rendered string, th Theme, plainUI bool, c *bodyLinkCollector, allowOSC8 bool) string {
	if rendered == "" || c == nil || len(c.links) == 0 {
		return rendered
	}
	// Nothing this pass does is visible without styling: osc8Link declines in
	// plain mode too.
	if plainUI {
		return rendered
	}

	plain, offsets := visibleIndex(rendered)
	style := lipgloss.NewStyle().
		Background(th.Bg).
		Foreground(messageLinkColor(th)).
		Underline(true)

	type span struct {
		start, end int // byte offsets into rendered
		href       string
	}

	var spans []span
	from := 0
	for _, link := range c.links {
		start, end := matchLinkText(plain, link.text, from)
		if start < 0 {
			// The text did not survive conversion (filtered, or rewritten).
			continue
		}
		from = end
		if start >= len(offsets) || end > len(offsets) {
			continue
		}
		endByte := len(rendered)
		if end < len(offsets) {
			endByte = offsets[end]
		}
		spans = append(spans, span{start: offsets[start], end: endByte, href: link.href})
	}

	// Apply from the end so earlier offsets stay valid.
	for i := len(spans) - 1; i >= 0; i-- {
		sp := spans[i]
		segment := rendered[sp.start:sp.end]
		lines := strings.Split(segment, "\n")
		for j, line := range lines {
			// Drop glamour's own styling inside the run so a reset partway
			// through cannot cut the underline short.
			text := ansi.Strip(line)
			trimmed := strings.TrimSpace(text)
			if trimmed == "" {
				lines[j] = text
				continue
			}
			at := strings.Index(text, trimmed)
			lead, tail := text[:at], text[at+len(trimmed):]
			out := trimmed
			if !plainUI {
				out = style.Render(trimmed)
			}
			if allowOSC8 {
				out = osc8Link(sp.href, out, plainUI)
			}
			lines[j] = lead + out + tail
		}
		rendered = rendered[:sp.start] + strings.Join(lines, "\n") + rendered[sp.end:]
	}
	return rendered
}

// linkPadding reports whether a space is needed on each side of a link so it
// does not run into neighbouring text. The rule used to add one unconditionally,
// which doubled the space whenever the surrounding text node already ended or
// began with one.
func linkPadding(selec *goquery.Selection) (lead, trail string) {
	if len(selec.Nodes) == 0 {
		return " ", " "
	}
	node := selec.Nodes[0]
	if edgeNeedsSpace(node.PrevSibling, false) {
		lead = " "
	}
	if edgeNeedsSpace(node.NextSibling, true) {
		trail = " "
	}
	return lead, trail
}

// edgeNeedsSpace reports whether a sibling would collide with the link text.
// A missing sibling means the link sits at the edge of its block, where a pad
// would only add stray indentation.
func edgeNeedsSpace(sib *xhtml.Node, leading bool) bool {
	if sib == nil {
		return false
	}
	if sib.Type != xhtml.TextNode || sib.Data == "" {
		return true
	}
	r := sib.Data[len(sib.Data)-1]
	if leading {
		r = sib.Data[0]
	}
	return r != ' ' && r != '\n' && r != '\t' && r != '\r'
}
