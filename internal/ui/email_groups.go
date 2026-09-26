package ui

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Newsletter layout tables are flattened into DOM order, which leaves nothing
// between one card and the next. Cells and rows that hold a complete content
// group (an image, heading, prose, or call to action) are marked before
// flattening, and each group is bracketed by a sentinel paragraph. Blank lines
// cannot carry the boundary: html-to-markdown and glamour both collapse them,
// and glamour's styled <hr> renders as nothing. The sentinel survives both and
// is replaced with a themed rule once the body has been rendered.
const (
	groupSeparatorRune = ''
	groupSeparator     = string(groupSeparatorRune)
	groupAttr          = "data-tidemail-group"
)

// minGroupProseWords is how much running text, outside headings and links, a
// cell needs before it counts as having a description.
const minGroupProseWords = 4

// minGroupImagePx separates content images from icons when both dimensions
// are hinted.
const minGroupImagePx = 48

func stripGroupSentinels(s string) string {
	return strings.ReplaceAll(s, groupSeparator, "")
}

// markNewsletterGroups marks the innermost layout cells and rows that form a
// content group. Candidates are visited in reverse document order so every
// descendant is decided before its ancestors.
func markNewsletterGroups(doc *goquery.Document) {
	dataTables := make(map[*xhtml.Node]bool)
	candidates := doc.Find("td,th,tr")
	for i := candidates.Length() - 1; i >= 0; i-- {
		candidate := candidates.Eq(i)
		if !newsletterGroupCandidate(candidate, dataTables) {
			continue
		}
		nested := candidate.Find("[" + groupAttr + "]")
		switch nested.Length() {
		case 0:
			if contentGroupKinds(candidate) >= 2 {
				_ = candidate.SetAttr(groupAttr, "true")
			}
		case 1:
			// An image cell beside a text cell is one card. The text cell
			// qualified on its own, so widen the group to the whole row.
			if rowCompletesGroup(candidate, nested) {
				nested.RemoveAttr(groupAttr)
				_ = candidate.SetAttr(groupAttr, "true")
			}
		}
	}
}

func newsletterGroupCandidate(candidate *goquery.Selection, dataTables map[*xhtml.Node]bool) bool {
	owner := candidate.Closest("table")
	if owner.Length() == 0 || cachedRenderedDataTable(owner, dataTables) {
		return false
	}
	if candidate.ParentsFiltered("blockquote").Length() > 0 {
		return false
	}
	if candidate.Closest(`[class*="signature"],[id*="signature"]`).Length() > 0 {
		return false
	}
	nestedData := false
	candidate.Find("table").EachWithBreak(func(_ int, table *goquery.Selection) bool {
		nestedData = cachedRenderedDataTable(table, dataTables)
		return !nestedData
	})
	return !nestedData
}

func cachedRenderedDataTable(table *goquery.Selection, cache map[*xhtml.Node]bool) bool {
	node := table.Get(0)
	if data, ok := cache[node]; ok {
		return data
	}
	data := isRenderedDataTable(table)
	cache[node] = data
	return data
}

func rowCompletesGroup(row, group *goquery.Selection) bool {
	if !row.Is("tr") {
		return false
	}
	cells := row.ChildrenFiltered("td,th")
	if cells.Length() < 2 || group.Parent().Get(0) != row.Get(0) {
		return false
	}
	other := false
	cells.Each(func(_ int, cell *goquery.Selection) {
		if cell.Get(0) == group.Get(0) {
			return
		}
		if strings.TrimSpace(cell.Text()) != "" || cell.Find("img").Length() > 0 {
			other = true
		}
	})
	return other
}

// contentGroupKinds counts how many of image, heading, prose, and link the
// element contains. Navigation rows and lone images score one.
func contentGroupKinds(selec *goquery.Selection) int {
	kinds := 0
	if hasGroupImage(selec) {
		kinds++
	}
	if strings.TrimSpace(selec.Find("h1,h2,h3,h4,h5,h6").Text()) != "" {
		kinds++
	}
	if hasGroupProse(selec) {
		kinds++
	}
	if hasGroupLink(selec) {
		kinds++
	}
	return kinds
}

func hasGroupImage(selec *goquery.Selection) bool {
	found := false
	selec.Find("img").EachWithBreak(func(_ int, img *goquery.Selection) bool {
		label := normalizeInlineSpacing(attrFirst(img, "alt", "title", "aria-label"))
		if label != "" && isDecorativeImageLabel(label) {
			return true
		}
		width, widthOK := cssDimension(attrFirst(img, "width"))
		height, heightOK := cssDimension(attrFirst(img, "height"))
		if widthOK && heightOK && width < minGroupImagePx && height < minGroupImagePx {
			return true
		}
		found = true
		return false
	})
	return found
}

func hasGroupLink(selec *goquery.Selection) bool {
	found := false
	selec.Find("a[href],[role=button]").EachWithBreak(func(_ int, link *goquery.Selection) bool {
		found = strings.TrimSpace(link.Text()) != "" || link.Find("img").Length() > 0
		return !found
	})
	return found
}

func hasGroupProse(selec *goquery.Selection) bool {
	words := 0
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if words >= minGroupProseWords {
			return
		}
		if n.Type == xhtml.ElementNode {
			switch n.DataAtom {
			case atom.A, atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
				return
			}
		}
		if n.Type == xhtml.TextNode {
			words += len(strings.Fields(n.Data))
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	for _, node := range selec.Nodes {
		walk(node)
	}
	return words >= minGroupProseWords
}

// insertGroupSentinels brackets every marked group with sentinel paragraphs.
// It runs after table normalization so the sentinel text can never influence
// receipt or data-table detection.
func insertGroupSentinels(doc *goquery.Document) {
	doc.Find("[" + groupAttr + "]").Each(func(_ int, group *goquery.Selection) {
		for _, node := range group.Nodes {
			node.InsertBefore(groupSentinelNode(), node.FirstChild)
			node.AppendChild(groupSentinelNode())
		}
	})
}

func groupSentinelNode() *xhtml.Node {
	p := &xhtml.Node{Type: xhtml.ElementNode, Data: "p", DataAtom: atom.P}
	p.AppendChild(&xhtml.Node{Type: xhtml.TextNode, Data: groupSeparator})
	return p
}

// expandGroupSeparators replaces sentinel lines in rendered output with a
// rule. Separators at either edge are dropped, adjacent ones collapse, and
// exactly one blank line is kept on each side. A sentinel glamour merged into
// prose is removed so it can never reach the screen, clipboard, or search.
func expandGroupSeparators(rendered string, width int, th Theme, plainUI bool) string {
	if !strings.ContainsRune(rendered, groupSeparatorRune) {
		return rendered
	}
	lines := strings.Split(rendered, "\n")
	out := make([]string, 0, len(lines))
	blank := ""
	pending := false
	indent := ""
	content := false
	for _, line := range lines {
		plain := ansi.Strip(line)
		if strings.TrimSpace(plain) == groupSeparator {
			if content {
				if !pending {
					indent = plain[:len(plain)-len(strings.TrimLeft(plain, " "))]
				}
				pending = true
			}
			continue
		}
		line = stripGroupSentinels(line)
		if strings.TrimSpace(ansi.Strip(line)) == "" {
			blank = line
			if !pending {
				out = append(out, line)
			}
			continue
		}
		if pending {
			for len(out) > 0 && strings.TrimSpace(ansi.Strip(out[len(out)-1])) == "" {
				out = out[:len(out)-1]
			}
			out = append(out, blank, groupRule(indent, width, th, plainUI), blank)
			pending = false
		}
		out = append(out, line)
		content = true
	}
	for len(out) > 0 && strings.TrimSpace(ansi.Strip(out[len(out)-1])) == "" {
		out = out[:len(out)-1]
	}
	// renderMarkdown trims the document; a dropped leading sentinel took that
	// trim, so repeat it for the line that now opens the body.
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func groupRule(indent string, width int, th Theme, plainUI bool) string {
	avail := max(1, width-len(indent))
	if plainUI {
		return indent + strings.Repeat("-", min(8, avail))
	}
	style := lipgloss.NewStyle().
		Background(th.Bg).
		Foreground(messageMutedColor(th))
	return indent + style.Render(strings.Repeat("─", avail))
}
