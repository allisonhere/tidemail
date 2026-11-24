// Package bodytext provides unstyled body text for cached reads and search.
package bodytext

import (
	"golang.org/x/net/html"
	"strings"
)

// Resolve preserves a nonblank plain body verbatim. HTML is only a fallback.
// blockElements end a line before and after their content.
var blockElements = map[string]bool{
	"address": true, "article": true, "aside": true, "blockquote": true, "br": true,
	"caption": true, "center": true, "dd": true, "details": true, "div": true,
	"dl": true, "dt": true, "fieldset": true, "figcaption": true, "figure": true,
	"footer": true, "form": true, "h1": true, "h2": true, "h3": true, "h4": true,
	"h5": true, "h6": true, "header": true, "hgroup": true, "hr": true,
	"legend": true, "li": true, "main": true, "nav": true, "ol": true, "p": true,
	"pre": true, "section": true, "summary": true, "table": true, "tr": true,
	"ul": true,
}

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
		block := n.Type == html.ElementNode && blockElements[n.Data]
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
