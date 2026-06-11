package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

func renderMarkdown(src string, width int, th Theme, plainUI bool) string {
	source := []byte(src)
	reader := text.NewReader(source)
	doc := goldmark.New().Parser().Parse(reader)

	var out []string
	for child := doc.FirstChild(); child != nil; child = child.NextSibling() {
		if b := mdBlock(child, source, width, th, plainUI); strings.TrimSpace(b) != "" {
			out = append(out, b)
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n\n"))
}

func mdBlock(node ast.Node, source []byte, width int, th Theme, plainUI bool) string {
	switch node.Kind() {
	case ast.KindParagraph, ast.KindTextBlock:
		return wrapWords(mdInlineText(node, source, th, plainUI), width)

	case ast.KindHeading:
		text := mdInlineText(node, source, th, plainUI)
		style := lipgloss.NewStyle().Background(th.Bg).Bold(true).Foreground(messageHeadingColor(th))
		return style.Render(wrapWords(text, width))

	case ast.KindBlockquote:
		prefix := "│ "
		if plainUI {
			prefix = "| "
		}
		var blocks []string
		for child := node.FirstChild(); child != nil; child = child.NextSibling() {
			if b := mdBlock(child, source, max(1, width-2), th, plainUI); strings.TrimSpace(b) != "" {
				blocks = append(blocks, b)
			}
		}
		joined := strings.Join(blocks, "\n\n")
		var lines []string
		for _, line := range strings.Split(joined, "\n") {
			lines = append(lines, prefix+line)
		}
		style := lipgloss.NewStyle().Background(th.Bg).Foreground(messageMutedColor(th))
		return style.Render(strings.Join(lines, "\n"))

	case ast.KindList:
		list := node.(*ast.List)
		var items []string
		counter := list.Start
		for item := node.FirstChild(); item != nil; item = item.NextSibling() {
			t := mdListItemText(item, source, width, th, plainUI)
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
		code := mdCodeLines(cb.Lines(), source, width)
		style := lipgloss.NewStyle().Background(th.Bg).Foreground(messageMutedColor(th))
		return style.Render(code)

	case ast.KindFencedCodeBlock:
		cb := node.(*ast.FencedCodeBlock)
		code := mdCodeLines(cb.Lines(), source, width)
		style := lipgloss.NewStyle().Background(th.Bg).Foreground(messageMutedColor(th))
		return style.Render(code)

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
			if b := mdBlock(child, source, width, th, plainUI); strings.TrimSpace(b) != "" {
				blocks = append(blocks, b)
			}
		}
		return strings.Join(blocks, "\n\n")
	}
}

func mdCodeLines(segs *text.Segments, source []byte, width int) string {
	var lines []string
	prefix := "    "
	contentW := max(1, width-lipgloss.Width(prefix))
	for i := 0; i < segs.Len(); i++ {
		seg := segs.At(i)
		line := strings.TrimRight(string(seg.Value(source)), "\r\n")
		for _, part := range splitDisplayWidth(line, contentW) {
			lines = append(lines, prefix+part)
		}
	}
	return strings.Join(lines, "\n")
}

func splitDisplayWidth(s string, width int) []string {
	if width <= 0 || s == "" {
		return []string{s}
	}
	if lipgloss.Width(s) <= width {
		return []string{s}
	}
	var parts []string
	var b strings.Builder
	currentW := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if currentW > 0 && currentW+rw > width {
			parts = append(parts, b.String())
			b.Reset()
			currentW = 0
		}
		b.WriteRune(r)
		currentW += rw
	}
	if b.Len() > 0 {
		parts = append(parts, b.String())
	}
	if len(parts) == 0 {
		return []string{s}
	}
	return parts
}

// mdListItemText collects plain text from a list item node.
// Tight list items contain TextBlock; loose items contain Paragraph.
func mdListItemText(node ast.Node, source []byte, width int, th Theme, plainUI bool) string {
	var parts []string
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		k := child.Kind()
		if k == ast.KindParagraph || k == ast.KindTextBlock {
			parts = append(parts, mdInlineText(child, source, th, plainUI))
			continue
		}
		if b := mdBlock(child, source, width, th, plainUI); strings.TrimSpace(b) != "" {
			parts = append(parts, b)
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, " ")
	}
	return mdInlineText(node, source, th, plainUI)
}

// mdInlineText recursively collects text from inline nodes, applying ANSI styles
// for bold, emphasis, and code spans.
func mdInlineText(node ast.Node, source []byte, th Theme, plainUI bool) string {
	var sb strings.Builder
	linkStyle := lipgloss.NewStyle().Background(th.Bg).Foreground(messageLinkColor(th)).Underline(true)
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		switch child.Kind() {
		case ast.KindText:
			t := child.(*ast.Text)
			val := strings.TrimRight(string(t.Segment.Value(source)), "\r\n")
			sb.WriteString(val)
			if t.SoftLineBreak() {
				sb.WriteByte(' ')
			}
		case ast.KindString:
			s := child.(*ast.String)
			sb.Write(s.Value)
		case ast.KindCodeSpan:
			inner := mdInlineText(child, source, th, plainUI)
			style := lipgloss.NewStyle().Background(messageCodeBg(th)).Foreground(messageCodeFg(th))
			sb.WriteString(style.Render(inner))
		case ast.KindLink:
			link := child.(*ast.Link)
			linkText := mdInlineText(child, source, th, plainUI)
			dest := strings.TrimSpace(string(link.Destination))
			if dest != "" && linkText == "" {
				linkText = dest
			}
			if linkText != "" {
				if !plainUI {
					sb.WriteString(linkStyle.Render(linkText))
				} else {
					sb.WriteString(linkText)
				}
			}
		case ast.KindAutoLink:
			al := child.(*ast.AutoLink)
			url := string(al.URL(source))
			if !plainUI {
				sb.WriteString(linkStyle.Render(url))
			} else {
				sb.WriteString(url)
			}
		case ast.KindRawHTML:
			// skip
		case ast.KindEmphasis:
			em := child.(*ast.Emphasis)
			inner := mdInlineText(child, source, th, plainUI)
			if em.Level >= 2 {
				// Strong / bold
				style := lipgloss.NewStyle().Background(th.Bg).Bold(true)
				sb.WriteString(style.Render(inner))
			} else {
				// Emphasis / italic
				style := lipgloss.NewStyle().Background(th.Bg).Italic(true).Foreground(messageMutedColor(th))
				sb.WriteString(style.Render(inner))
			}
		default:
			// Other inline containers: just recurse
			sb.WriteString(mdInlineText(child, source, th, plainUI))
		}
	}
	return strings.TrimRight(sb.String(), " \t")
}
