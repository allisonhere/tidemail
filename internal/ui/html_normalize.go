package ui

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
	xhtml "golang.org/x/net/html"
)

func normalizeHTMLForRendering(raw string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(raw))
	if err != nil {
		return raw
	}
	doc.Find("script,style,noscript,template,head,meta,link,input,textarea,select").Remove()
	doc.Find("[hidden]").Remove()
	doc.Find("[aria-hidden]").Each(func(_ int, s *goquery.Selection) {
		if strings.EqualFold(strings.TrimSpace(attrFirst(s, "aria-hidden")), "true") {
			s.Remove()
		}
	})
	doc.Find("[style]").Each(func(_ int, s *goquery.Selection) {
		if hiddenByInlineStyle(parseInlineStyle(attrFirst(s, "style"))) {
			s.Remove()
		}
	})
	doc.Find("img").Each(func(_ int, s *goquery.Selection) {
		if isTrackingImage(s) {
			s.Remove()
		}
	})
	removeZeroFontText(doc)
	removeEmailSpacerElements(doc)
	normalizeEmailQuotes(doc)
	normalizeEmailTables(doc)
	body := doc.Find("body")
	if body.Length() > 0 {
		if out, err := body.Html(); err == nil {
			return out
		}
	}
	if out, err := doc.Html(); err == nil {
		return out
	}
	return raw
}

// Font size is inherited, but descendants can restore it. Remove only text
// that remains zero-sized, keeping the DOM structure and visible descendants.
func removeZeroFontText(doc *goquery.Document) {
	var walk func(*xhtml.Node, bool)
	walk = func(n *xhtml.Node, zero bool) {
		if n.Type == xhtml.ElementNode {
			for _, a := range n.Attr {
				if a.Key != "style" {
					continue
				}
				size := parseInlineStyle(a.Val)["font-size"]
				if size != "" && size != "inherit" && size != "unset" {
					zero = isZeroDimension(size)
				}
			}
		}
		if n.Type == xhtml.TextNode && zero {
			n.Data = ""
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, zero)
		}
	}
	for _, n := range doc.Nodes {
		walk(n, false)
	}
}
