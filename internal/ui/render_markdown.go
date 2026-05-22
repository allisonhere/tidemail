package ui

import (
	"fmt"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

func renderMarkdown(src string, width int, plainUI bool) string {
	source := []byte(src)
	reader := text.NewReader(source)
	doc := goldmark.New().Parser().Parse(reader)

	var out []string
	for child := doc.FirstChild(); child != nil; child = child.NextSibling() {
		if b := mdBlock(child, source, width, plainUI); strings.TrimSpace(b) != "" {
			out = append(out, b)
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n\n"))
}

func mdBlock(node ast.Node, source []byte, width int, plainUI bool) string {
	switch node.Kind() {
	case ast.KindParagraph, ast.KindTextBlock:
		return wrapWords(mdInlineText(node, source), width)

	case ast.KindHeading:
		return wrapWords(mdInlineText(node, source), width)

	case ast.KindBlockquote:
		prefix := "│ "
		if plainUI {
			prefix = "| "
		}
		var blocks []string
		for child := node.FirstChild(); child != nil; child = child.NextSibling() {
			if b := mdBlock(child, source, max(1, width-2), plainUI); strings.TrimSpace(b) != "" {
				blocks = append(blocks, b)
			}
		}
		joined := strings.Join(blocks, "\n\n")
		var lines []string
		for _, line := range strings.Split(joined, "\n") {
			lines = append(lines, prefix+line)
		}
		return strings.Join(lines, "\n")

	case ast.KindList:
		list := node.(*ast.List)
		var items []string
		counter := list.Start
		for item := node.FirstChild(); item != nil; item = item.NextSibling() {
			t := mdListItemText(item, source)
			if list.IsOrdered() {
				items = append(items, wrapNumberedBullet(fmt.Sprintf("%d", counter), t, width))
				counter++
			} else {
				items = append(items, wrapBullet(t, width, plainUI))
			}
		}
		return strings.Join(items, "\n")

	case ast.KindCodeBlock:
		cb := node.(*ast.CodeBlock)
		return mdCodeLines(cb.Lines(), source)

	case ast.KindFencedCode:
		cb := node.(*ast.FencedCodeBlock)
		return mdCodeLines(cb.Lines(), source)

	case ast.KindHTMLBlock:
		return ""

	case ast.KindThematicBreak:
		if plainUI {
			return strings.Repeat("-", width)
		}
		return strings.Repeat("─", width)

	default:
		var blocks []string
		for child := node.FirstChild(); child != nil; child = child.NextSibling() {
			if b := mdBlock(child, source, width, plainUI); strings.TrimSpace(b) != "" {
				blocks = append(blocks, b)
			}
		}
		return strings.Join(blocks, "\n\n")
	}
}

func mdCodeLines(segs *text.Segments, source []byte) string {
	var lines []string
	for i := 0; i < segs.Len(); i++ {
		seg := segs.At(i)
		line := strings.TrimRight(string(seg.Value(source)), "\r\n")
		lines = append(lines, "    "+line)
	}
	return strings.Join(lines, "\n")
}

// mdListItemText collects plain text from a list item node.
// Tight list items contain TextBlock; loose items contain Paragraph.
func mdListItemText(node ast.Node, source []byte) string {
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		k := child.Kind()
		if k == ast.KindParagraph || k == ast.KindTextBlock {
			return mdInlineText(child, source)
		}
	}
	return mdInlineText(node, source)
}

// mdInlineText recursively collects plain text from inline nodes. No ANSI codes.
func mdInlineText(node ast.Node, source []byte) string {
	var sb strings.Builder
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		switch child.Kind() {
		case ast.KindText:
			t := child.(*ast.Text)
			val := strings.TrimRight(string(t.Segment.Value(source)), "\r\n")
			sb.WriteString(val)
			if t.SoftLineBreak {
				sb.WriteByte(' ')
			}
		case ast.KindString:
			s := child.(*ast.String)
			sb.Write(s.Value)
		case ast.KindCodeSpan:
			sb.WriteByte('`')
			sb.WriteString(mdInlineText(child, source))
			sb.WriteByte('`')
		case ast.KindLink:
			link := child.(*ast.Link)
			linkText := mdInlineText(child, source)
			dest := strings.TrimSpace(string(link.Destination))
			if dest != "" && dest != linkText {
				sb.WriteString(linkText)
				sb.WriteString(" (")
				sb.WriteString(dest)
				sb.WriteByte(')')
			} else {
				sb.WriteString(linkText)
			}
		case ast.KindAutoLink:
			al := child.(*ast.AutoLink)
			sb.WriteString(string(al.URL(source)))
		case ast.KindRawHTML:
			// skip
		default:
			// Emphasis, Strong, and other inline containers: just recurse
			sb.WriteString(mdInlineText(child, source))
		}
	}
	return strings.TrimRight(sb.String(), " \t")
}
