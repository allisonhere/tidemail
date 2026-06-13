package ui

import (
	"fmt"
	htmlstd "html"
	"regexp"
	"strings"

	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/PuerkitoBio/goquery"
	"github.com/charmbracelet/lipgloss"
)

var (
	urlRe          = regexp.MustCompile(`https?://[^\s<>"']+`)
	emailRe        = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
	imageIDLabelRe = regexp.MustCompile(`^[a-fA-F0-9]{8}(-[a-fA-F0-9]{4}){3}-[a-fA-F0-9]{12}$`)
)

func formatArticleBody(content string, width int, th Theme, plainUI bool) string {
	content = stripEmailInvisibles(content)
	content = strings.ReplaceAll(content, "\r\n", "\n")
	paras := splitArticleParagraphs(content)
	out := make([]string, 0, len(paras))
	for _, p := range paras {
		if p == "" {
			continue
		}
		out = append(out, formatArticleParagraph(p, width, th, plainUI))
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\n\n")
}

func formatSummaryBody(content string, width int, plainUI bool) string {
	content = stripEmailInvisibles(content)
	content = strings.ReplaceAll(content, "\r\n", "\n")
	paras := splitArticleParagraphs(content)
	if len(paras) == 1 {
		paras = splitDenseSummaryParagraph(paras[0])
	}
	out := make([]string, 0, len(paras))
	for _, p := range paras {
		if p == "" {
			continue
		}
		out = append(out, formatSummaryParagraph(p, width, plainUI))
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\n\n")
}

func splitArticleParagraphs(content string) []string {
	raw := strings.Split(content, "\n\n")
	out := make([]string, 0, len(raw))
	for _, part := range raw {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func formatArticleParagraph(p string, width int, th Theme, plainUI bool) string {
	lines := strings.Split(strings.TrimSpace(p), "\n")
	if len(lines) == 0 {
		return ""
	}

	quoteBar := "│ "
	if plainUI {
		quoteBar = "| "
	}
	trimmed := strings.TrimSpace(lines[0])
	switch {
	case strings.HasPrefix(trimmed, "#"):
		text := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
		style := lipgloss.NewStyle().Background(th.Bg).Bold(true).Foreground(messageHeadingColor(th))
		return style.Render(wrapWords(text, width))
	case strings.HasPrefix(trimmed, ">"):
		quote := normalizeInlineSpacing(strings.TrimSpace(strings.TrimLeft(trimmed, ">")))
		style := lipgloss.NewStyle().Background(th.Bg).Foreground(messageMutedColor(th))
		return style.Render(wrapWords(quoteBar+quote, width))
	case strings.HasPrefix(trimmed, "- "), strings.HasPrefix(trimmed, "* "):
		items := make([]string, 0, len(lines))
		for _, line := range lines {
			line = strings.TrimSpace(strings.TrimLeft(strings.TrimLeft(line, "-"), "*"))
			if line == "" {
				continue
			}
			items = append(items, wrapBullet(line, width, plainUI))
		}
		return strings.Join(items, "\n")
	default:
		text := normalizeInlineSpacing(strings.Join(lines, " "))
		text = highlightInlineLinks(text, th, plainUI)
		return wrapWords(text, width)
	}
}

func formatSummaryParagraph(p string, width int, plainUI bool) string {
	lines := strings.Split(strings.TrimSpace(p), "\n")
	if len(lines) == 0 {
		return ""
	}

	trimmed := strings.TrimSpace(lines[0])
	switch {
	case strings.HasPrefix(trimmed, "- "), strings.HasPrefix(trimmed, "* "):
		items := make([]string, 0, len(lines))
		for _, line := range lines {
			line = strings.TrimSpace(strings.TrimLeft(strings.TrimLeft(line, "-"), "*"))
			if line == "" {
				continue
			}
			items = append(items, wrapBullet(line, width, plainUI))
		}
		return strings.Join(items, "\n")
	case isNumberedListItem(trimmed):
		items := make([]string, 0, len(lines))
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			num, body, ok := splitNumberedListItem(line)
			if !ok {
				items = append(items, wrapWords(normalizeInlineSpacing(line), width))
				continue
			}
			items = append(items, wrapNumberedBullet(num, body, width))
		}
		return strings.Join(items, "\n")
	default:
		return wrapWords(normalizeInlineSpacing(strings.Join(lines, " ")), width)
	}
}

func splitDenseSummaryParagraph(p string) []string {
	p = normalizeInlineSpacing(p)
	if p == "" {
		return nil
	}
	if strings.Contains(p, "\n") || strings.HasPrefix(p, "- ") || strings.HasPrefix(p, "* ") || isNumberedListItem(p) {
		return []string{p}
	}

	sentences := splitSentences(p)
	if len(sentences) < 3 {
		return []string{p}
	}

	paras := make([]string, 0, (len(sentences)+1)/2)
	for i := 0; i < len(sentences); i += 2 {
		end := min(i+2, len(sentences))
		paras = append(paras, strings.Join(sentences[i:end], " "))
	}
	return paras
}

func wrapBullet(text string, width int, plainUI bool) string {
	prefix := "• "
	indent := "  "
	if plainUI {
		prefix = "* "
		indent = "  "
	}
	if width <= 2 {
		return prefix + text
	}
	wrapped := wrapWords(text, width-2)
	lines := strings.Split(wrapped, "\n")
	for i := range lines {
		if i == 0 {
			lines[i] = prefix + lines[i]
		} else {
			lines[i] = indent + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}

func wrapNumberedBullet(num, text string, width int) string {
	prefix := fmt.Sprintf("%s. ", num)
	if width <= lipgloss.Width(prefix) {
		return prefix + text
	}
	wrapped := wrapWords(text, width-lipgloss.Width(prefix))
	lines := strings.Split(wrapped, "\n")
	indent := strings.Repeat(" ", lipgloss.Width(prefix))
	for i := range lines {
		if i == 0 {
			lines[i] = prefix + lines[i]
		} else {
			lines[i] = indent + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}

func wrapWords(text string, width int) string {
	text = normalizeInlineSpacing(text)
	if text == "" || width <= 1 {
		return text
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return ""
	}

	lines := make([]string, 0, len(words))
	first := splitLongWord(words[0], width)
	lines = append(lines, first...)
	for _, word := range words[1:] {
		wordParts := splitLongWord(word, width)
		current := lines[len(lines)-1]
		if len(wordParts) == 1 && lipgloss.Width(current)+1+lipgloss.Width(word) <= width {
			lines[len(lines)-1] = current + " " + word
			continue
		}
		lines = append(lines, wordParts...)
	}
	return strings.Join(lines, "\n")
}

func splitLongWord(word string, width int) []string {
	if width <= 1 || lipgloss.Width(word) <= width {
		return []string{word}
	}
	var parts []string
	var b strings.Builder
	currentW := 0
	for _, r := range word {
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
		return []string{word}
	}
	return parts
}

func normalizeInlineSpacing(s string) string {
	s = stripEmailInvisibles(s)
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}

func stripEmailInvisibles(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\u00ad', // soft hyphen
			'\u034f', // combining grapheme joiner
			'\u061c', // Arabic letter mark
			'\u180e', // Mongolian vowel separator
			'\u200b', // zero-width space
			'\u200c', // zero-width non-joiner
			'\u200d', // zero-width joiner
			'\u200e', // left-to-right mark
			'\u200f', // right-to-left mark
			'\u2060', // word joiner
			'\ufeff': // zero-width no-break space
			return -1
		default:
			return r
		}
	}, s)
}

func splitSentences(s string) []string {
	s = normalizeInlineSpacing(s)
	if s == "" {
		return nil
	}

	var sentences []string
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '.', '!', '?':
			j := i + 1
			for j < len(s) && (s[j] == '"' || s[j] == '\'' || s[j] == ')' || s[j] == ']') {
				j++
			}
			if j == len(s) || s[j] == ' ' {
				part := strings.TrimSpace(s[start:j])
				if part != "" {
					sentences = append(sentences, part)
				}
				for j < len(s) && s[j] == ' ' {
					j++
				}
				start = j
				i = j - 1
			}
		}
	}
	if start < len(s) {
		tail := strings.TrimSpace(s[start:])
		if tail != "" {
			sentences = append(sentences, tail)
		}
	}
	if len(sentences) == 0 {
		return []string{s}
	}
	return sentences
}

func renderHTMLBody(html string, width int, th Theme, plainUI bool) string {
	html = normalizeHTMLForRendering(html)
	converter := md.NewConverter("", true, nil)
	converter.AddRules(spanStyleRule())
	converter.AddRules(buttonLinkRule())
	converter.AddRules(imagePlaceholderRule())
	converter.AddRules(tableTextRule())
	markdown, err := converter.ConvertString(html)
	if err != nil || strings.TrimSpace(markdown) == "" {
		return ""
	}
	markdown = stripEmailInvisibles(markdown)
	return renderMarkdown(markdown, width, th, plainUI)
}

func messageHeadingColor(th Theme) lipgloss.Color {
	return accentReadableOn(th.BorderFocus, th.Bg, 4.5)
}

func messageLinkColor(th Theme) lipgloss.Color {
	return accentReadableOn(th.BorderFocus, th.Bg, 4.5)
}

func messageMutedColor(th Theme) lipgloss.Color {
	return readableText(th.Dimmed, th.Bg, 3.0)
}

func messageCodeBg(th Theme) lipgloss.Color {
	bg := focusLineBg(th)
	if bg == "" || contrastRatio(bg, th.Bg) < 1.2 {
		if isDark(th.Bg) {
			return adjustLightness(th.Bg, 0.08)
		}
		return adjustLightness(th.Bg, -0.08)
	}
	return bg
}

func messageCodeFg(th Theme) lipgloss.Color {
	return readableText(th.Fg, messageCodeBg(th), 4.5)
}

func normalizeHTMLForRendering(raw string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(raw))
	if err != nil {
		return raw
	}
	doc.Find("script,style,noscript,template,head,meta,link,input,textarea,select").Remove()
	doc.Find("[hidden], [aria-hidden='true']").Remove()
	doc.Find("[style]").Each(func(_ int, s *goquery.Selection) {
		style, _ := s.Attr("style")
		style = strings.ToLower(style)
		if strings.Contains(style, "display:none") ||
			strings.Contains(style, "display: none") ||
			strings.Contains(style, "visibility:hidden") ||
			strings.Contains(style, "visibility: hidden") ||
			strings.Contains(style, "font-size:0") ||
			strings.Contains(style, "font-size: 0") ||
			strings.Contains(style, "opacity:0") ||
			strings.Contains(style, "opacity: 0") {
			s.Remove()
		}
	})
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

func tableTextRule() md.Rule {
	return md.Rule{
		Filter: []string{"table"},
		Replacement: func(content string, selec *goquery.Selection, _ *md.Options) *string {
			if selec.Find("table").Length() > 0 || selec.Find("th").Length() == 0 {
				return &content
			}

			var rows [][]string
			selec.Find("tr").Each(func(_ int, tr *goquery.Selection) {
				var row []string
				tr.Find("th,td").Each(func(_ int, cell *goquery.Selection) {
					row = append(row, normalizeInlineSpacing(cell.Text()))
				})
				if len(row) > 0 {
					rows = append(rows, row)
				}
			})
			if len(rows) == 0 {
				return md.String("")
			}

			cols := 0
			for _, row := range rows {
				if len(row) > cols {
					cols = len(row)
				}
			}
			widths := make([]int, cols)
			for _, row := range rows {
				for i, cell := range row {
					if l := lipgloss.Width(cell); l > widths[i] {
						widths[i] = l
					}
				}
			}

			lines := make([]string, 0, len(rows)+2)
			lines = append(lines, "```")
			for _, row := range rows {
				cells := make([]string, cols)
				for i := 0; i < cols; i++ {
					cell := ""
					if i < len(row) {
						cell = row[i]
					}
					cells[i] = cell + strings.Repeat(" ", widths[i]-lipgloss.Width(cell))
				}
				lines = append(lines, strings.Join(cells, " | "))
			}
			lines = append(lines, "```")
			return md.String("\n\n" + strings.Join(lines, "\n") + "\n\n")
		},
	}
}

func imagePlaceholderRule() md.Rule {
	return md.Rule{
		Filter: []string{"img"},
		Replacement: func(_ string, selec *goquery.Selection, _ *md.Options) *string {
			label := strings.TrimSpace(attrFirst(selec, "alt", "title", "aria-label"))
			if label == "" {
				return md.String(" ")
			}
			label = normalizeInlineSpacing(label)
			if isDecorativeImageLabel(label) {
				return md.String(" ")
			}
			placeholder := "[image: " + label + "]"
			return md.String("\n\n" + placeholder + "\n\n")
		},
	}
}

func isDecorativeImageLabel(label string) bool {
	lower := strings.ToLower(strings.TrimSpace(label))
	if lower == "" || lower == "image" {
		return true
	}
	if imageIDLabelRe.MatchString(label) {
		return true
	}
	if strings.HasPrefix(lower, "commentor") {
		return true
	}
	for _, token := range []string{
		"avatar",
		"badge",
		"comment icon",
		"icon",
		"logo",
		"profile photo",
		"reaction",
		"share icon",
	} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

func buttonLinkRule() md.Rule {
	return md.Rule{
		Filter: []string{"a"},
		Replacement: func(content string, selec *goquery.Selection, _ *md.Options) *string {
			href := strings.TrimSpace(attrFirst(selec, "href"))
			text := normalizeInlineSpacing(htmlstd.UnescapeString(selec.Text()))
			if text == "" {
				text = normalizeInlineSpacing(content)
			}
			if href == "" {
				return md.String(" ")
			}
			if selec.Find("table").Length() > 0 {
				text = layoutCellText(selec)
				if text == "" {
					return md.String(" ")
				}
				return md.String("\n\n" + text + "\n\n")
			}
			if !looksLikeButtonLink(selec) {
				if text == "" {
					return md.String(" ")
				}
				if isVisualOnlyLinkText(text, selec) {
					return md.String(" ")
				}
				return md.String(" " + text + " ")
			}
			if text == "" {
				return md.String(" ")
			}
			if isVisualOnlyLinkText(text, selec) {
				return md.String(" ")
			}
			return md.String("[" + text + "]")
		},
	}
}

func layoutCellText(selec *goquery.Selection) string {
	var parts []string
	selec.Find("td,th").Each(func(_ int, cell *goquery.Selection) {
		if cell.Find("td,th").Length() > 0 {
			return
		}
		text := normalizeInlineSpacing(htmlstd.UnescapeString(cell.Text()))
		if text != "" {
			parts = append(parts, text)
		}
	})
	if len(parts) == 0 {
		return normalizeInlineSpacing(htmlstd.UnescapeString(selec.Text()))
	}
	return strings.Join(parts, " ")
}

func isVisualOnlyLinkText(text string, selec *goquery.Selection) bool {
	text = strings.TrimSpace(text)
	if len([]rune(text)) != 1 {
		return false
	}
	style := strings.ToLower(attrFirst(selec, "style"))
	return strings.Contains(style, "display:inline-block") ||
		strings.Contains(style, "display: inline-block")
}

func attrFirst(selec *goquery.Selection, names ...string) string {
	for _, name := range names {
		if v, ok := selec.Attr(name); ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func looksLikeButtonLink(selec *goquery.Selection) bool {
	style := strings.ToLower(attrFirst(selec, "style"))
	className := strings.ToLower(attrFirst(selec, "class"))
	role := strings.ToLower(attrFirst(selec, "role"))
	return strings.Contains(className, "button") ||
		strings.Contains(className, "btn") ||
		role == "button" ||
		strings.Contains(style, "display:inline-block") ||
		strings.Contains(style, "display: inline-block") ||
		strings.Contains(style, "background") && strings.Contains(style, "padding")
}

// spanStyleRule returns a Rule that converts <span style="..."> CSS properties
// into markdown formatting. Handles font-weight (bold), font-style (italic),
// and their combination. Always returns content (styled or plain) since span
// has no default fallback rule in the library.
func spanStyleRule() md.Rule {
	return md.Rule{
		Filter: []string{"span"},
		Replacement: func(content string, selec *goquery.Selection, _ *md.Options) *string {
			style, ok := selec.Attr("style")
			if !ok || style == "" {
				return &content
			}
			style = strings.ToLower(style)

			isBold := strings.Contains(style, "font-weight:bold") ||
				strings.Contains(style, "font-weight: 700") ||
				strings.Contains(style, "font-weight:700") ||
				strings.Contains(style, "font-weight:bold ") ||
				func() bool {
					// Check for font-weight: 600, 800, 900 etc.
					for _, w := range []string{"600", "700", "800", "900"} {
						if strings.Contains(style, "font-weight:"+w) ||
							strings.Contains(style, "font-weight: "+w) {
							return true
						}
					}
					return false
				}()

			isItalic := strings.Contains(style, "font-style:italic") ||
				strings.Contains(style, "font-style:italic ") ||
				strings.Contains(style, "font-style:oblique") ||
				strings.Contains(style, "font-style:oblique ") ||
				strings.Contains(style, "font-style: italic") ||
				strings.Contains(style, "font-style: oblique")

			if isBold && isItalic {
				return md.String("**_" + content + "_**")
			}
			if isBold {
				return md.String("**" + content + "**")
			}
			if isItalic {
				return md.String("_" + content + "_")
			}
			return &content
		},
	}
}

// highlightInlineLinks finds URLs and email addresses in text and wraps them
// in accent-color + underline styling. Only applies when not in plainUI mode.
func highlightInlineLinks(text string, th Theme, plainUI bool) string {
	if plainUI {
		return text
	}
	linkStyle := lipgloss.NewStyle().Background(th.Bg).Foreground(messageLinkColor(th)).Underline(true)
	replace := func(match string) string {
		return linkStyle.Render(match)
	}
	text = urlRe.ReplaceAllStringFunc(text, replace)
	text = emailRe.ReplaceAllStringFunc(text, replace)
	return text
}

func isNumberedListItem(s string) bool {
	_, _, ok := splitNumberedListItem(s)
	return ok
}

func splitNumberedListItem(s string) (num, body string, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", false
	}
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 || i+1 >= len(s) || s[i] != '.' || s[i+1] != ' ' {
		return "", "", false
	}
	body = strings.TrimSpace(s[i+2:])
	if body == "" {
		return "", "", false
	}
	return s[:i], body, true
}
