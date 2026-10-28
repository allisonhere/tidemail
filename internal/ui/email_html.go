package ui

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// emailExportCSP forbids every network fetch from an exported message while
// leaving link navigation alone. It is the enforcement; the element and
// attribute stripping below keeps the file tidy and lets us count what was
// blocked.
const emailExportCSP = "default-src 'none'; img-src data:; style-src 'unsafe-inline'; font-src data:; base-uri 'none'; form-action 'none'"

var (
	cssURL    = regexp.MustCompile(`(?i)url\(\s*(?:"[^"]*"|'[^']*'|[^)]*)\)`)
	cssImport = regexp.MustCompile(`(?i)@import[^;{]*;?`)
)

// sanitizeEmailHTML returns src with remote content removed so a browser makes
// no network requests when it opens the export. Links are preserved. blocked
// counts the remote images that were dropped.
func sanitizeEmailHTML(src string) (out string, blocked int, err error) {
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return "", 0, err
	}
	var head *html.Node
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; {
			next := c.NextSibling
			if c.Type == html.ElementNode {
				switch c.DataAtom {
				case atom.Head:
					head = c
				case atom.Script, atom.Iframe, atom.Frame, atom.Frameset, atom.Object,
					atom.Embed, atom.Applet, atom.Link, atom.Base, atom.Video, atom.Audio,
					atom.Source, atom.Track:
					n.RemoveChild(c)
					c = next
					continue
				case atom.Meta:
					if isMetaRefresh(c) {
						n.RemoveChild(c)
						c = next
						continue
					}
				case atom.Style:
					for t := c.FirstChild; t != nil; t = t.NextSibling {
						if t.Type == html.TextNode {
							t.Data = stripCSSRemote(t.Data)
						}
					}
				}
				blocked += stripRemoteAttrs(c)
			}
			walk(c)
			c = next
		}
	}
	walk(doc)
	if head != nil {
		meta := &html.Node{Type: html.ElementNode, DataAtom: atom.Meta, Data: "meta", Attr: []html.Attribute{
			{Key: "http-equiv", Val: "Content-Security-Policy"},
			{Key: "content", Val: emailExportCSP},
		}}
		head.InsertBefore(meta, head.FirstChild)
	}
	var sb strings.Builder
	if err := html.Render(&sb, doc); err != nil {
		return "", 0, err
	}
	return sb.String(), blocked, nil
}

func isMetaRefresh(n *html.Node) bool {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, "http-equiv") && strings.EqualFold(strings.TrimSpace(a.Val), "refresh") {
			return true
		}
	}
	return false
}

// stripRemoteAttrs drops attributes that would load or phone home from n and
// returns 1 when a remote image was removed.
func stripRemoteAttrs(n *html.Node) (blocked int) {
	kept := n.Attr[:0]
	removedImage := false
	for _, a := range n.Attr {
		key := strings.ToLower(a.Key)
		switch {
		case strings.HasPrefix(key, "on"):
			continue
		case key == "srcset" || key == "imagesrcset":
			if !isInlineSource(a.Val) {
				removedImage = removedImage || n.DataAtom == atom.Img
				continue
			}
		case key == "src" || key == "background" || key == "poster" || key == "data" ||
			key == "lowsrc" || key == "dynsrc" || key == "longdesc" || key == "ping":
			if !isInlineSource(a.Val) {
				removedImage = removedImage || n.DataAtom == atom.Img
				continue
			}
		case key == "style":
			a.Val = stripCSSRemote(a.Val)
		}
		kept = append(kept, a)
	}
	n.Attr = kept
	if removedImage {
		return 1
	}
	return 0
}

// isInlineSource reports whether v carries its own data (or is empty or a
// cid: reference that cannot hit the network).
func isInlineSource(v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	return v == "" || strings.HasPrefix(v, "data:") || strings.HasPrefix(v, "cid:")
}

func stripCSSRemote(css string) string {
	css = cssImport.ReplaceAllString(css, "")
	return cssURL.ReplaceAllStringFunc(css, func(m string) string {
		inner := strings.TrimSpace(m[strings.Index(m, "(")+1 : len(m)-1])
		inner = strings.Trim(inner, `"'`)
		if isInlineSource(inner) && inner != "" {
			return m
		}
		return "none"
	})
}
