// Package bodytext provides unstyled body text for cached reads and search.
package bodytext

import (
	"golang.org/x/net/html"
	"strings"
)

// Resolve preserves a nonblank plain body verbatim. HTML is only a fallback.
func Resolve(plain, markup string) (string, string) {
	if strings.TrimSpace(plain) != "" {
		return plain, "plain"
	}
	text := FromHTML(markup)
	if text == "" {
		return "", "none"
	}
	return text, "html"
}

// FromHTML extracts labels and text without link targets or remote resources.
func FromHTML(markup string) string {
	root, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		return ""
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "head", "script", "style", "template", "noscript":
				return
			}
			for _, a := range n.Attr {
				if a.Key == "hidden" || (a.Key == "aria-hidden" && strings.EqualFold(a.Val, "true")) {
					return
				}
			}
		}
		block := n.Type == html.ElementNode && strings.Contains("|p|div|br|tr|li|ul|ol|table|section|article|blockquote|pre|h1|h2|h3|h4|h5|h6|", "|"+n.Data+"|")
		if block {
			b.WriteByte('\n')
		}
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		if n.Type == html.ElementNode && (n.Data == "td" || n.Data == "th") {
			b.WriteByte(' ')
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if block {
			b.WriteByte('\n')
		}
	}
	walk(root)
	var lines []string
	for _, line := range strings.Split(b.String(), "\n") {
		if line = strings.Join(strings.Fields(line), " "); line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}
