package ui

import (
	"fmt"
	htmlstd "html"
	"regexp"
	"strconv"
	"strings"

	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/PuerkitoBio/goquery"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var (
	urlRe          = regexp.MustCompile(`https?://[^\s<>"']+`)
	emailRe        = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
	imageLabelRe   = regexp.MustCompile(`\[image: [^\]\n]+\]`)
	imageIDLabelRe = regexp.MustCompile(`^[a-fA-F0-9]{8}(-[a-fA-F0-9]{4}){3}-[a-fA-F0-9]{12}$`)
	dimensionRe    = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)(?:px|pt|pc|in|cm|mm|q|em|rem|ex|ch|%|vw|vh|vmin|vmax)?$`)
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
		// Trim only blank lines, not leading whitespace: the first line's
		// indentation is the primary signal for code and hanging layout.
		part = strings.Trim(part, "\n")
		if strings.TrimSpace(part) == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

// paragraphLines normalizes a paragraph into lines, trimming trailing
// whitespace per line and dropping leading/trailing blank lines while
// preserving each line's leading indentation.
func paragraphLines(p string) []string {
	lines := strings.Split(p, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t\r")
	}
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// formatArticleParagraph renders one paragraph of a plain-text body.
//
// It scans runs of like-shaped lines, consuming every line exactly once. The
// previous implementation switched on lines[0] and, in the quote and heading
// branches, rendered only that line — silently discarding the rest of the
// paragraph.
func formatArticleParagraph(p string, width int, th Theme, plainUI bool) string {
	lines := paragraphLines(p)
	if len(lines) == 0 {
		return ""
	}
	if looksPreformatted(lines, width) {
		return renderPreformattedBlock(lines, width, th, plainUI)
	}

	var out []string
	for i := 0; i < len(lines); {
		switch {
		case strings.TrimSpace(lines[i]) == "":
			i++
		case isQuoteLine(lines[i]):
			j := i
			for j < len(lines) && isQuoteLine(lines[j]) {
				j++
			}
			out = append(out, renderQuoteRun(lines[i:j], width, th, plainUI))
			i = j
		case isHeadingLine(lines[i]):
			out = append(out, renderHeadingLine(lines[i], width, th, plainUI))
			i++
		case isListLine(lines[i]):
			j := i + 1
			for j < len(lines) && (isListLine(lines[j]) || isListContinuation(lines[j])) {
				j++
			}
			out = append(out, renderListRun(lines[i:j], width, th, plainUI))
			i = j
		default:
			j := i
			for j < len(lines) && isProseLine(lines[j]) {
				j++
			}
			out = append(out, renderProseRun(lines[i:j], width, th, plainUI))
			i = j
		}
	}
	return strings.Join(out, "\n")
}

func renderProseRun(lines []string, width int, th Theme, plainUI bool) string {
	base := lipgloss.NewStyle().Background(th.Bg)
	text := normalizeInlineSpacing(strings.Join(lines, " "))
	return styleBlockWithLinks(wrapWords(text, width), base, th, plainUI)
}

func renderHeadingLine(line string, width int, th Theme, plainUI bool) string {
	text, _, ok := splitATXHeading(line)
	if !ok {
		return renderProseRun([]string{line}, width, th, plainUI)
	}
	base := lipgloss.NewStyle().Background(th.Bg).Bold(true).Foreground(messageHeadingColor(th))
	return styleBlockWithLinks(wrapWords(text, width), base, th, plainUI)
}

// maxQuoteDepth caps how many quote bars are drawn, so a deeply nested thread
// cannot consume the whole pane width with indentation.
const maxQuoteDepth = 5

// renderQuoteRun groups consecutive quoted lines by nesting depth and applies
// the quote bar AFTER wrapping, so every continuation line stays barred and
// indented rather than merging into the surrounding body text.
func renderQuoteRun(lines []string, width int, th Theme, plainUI bool) string {
	bar := "│ "
	if plainUI {
		bar = "| "
	}
	base := lipgloss.NewStyle().Background(th.Bg).Foreground(messageMutedColor(th))

	var out []string
	for i := 0; i < len(lines); {
		depth, _, _ := splitQuotePrefix(lines[i])
		j := i
		var bodies []string
		for j < len(lines) {
			d, b, ok := splitQuotePrefix(lines[j])
			if !ok || d != depth {
				break
			}
			bodies = append(bodies, b)
			j++
		}
		i = j

		prefix := strings.Repeat(bar, min(depth, maxQuoteDepth))
		avail := max(4, width-lipgloss.Width(prefix))

		var wrapped string
		if looksPreformatted(bodies, avail) {
			// A forwarded table or address block inside a reply keeps its shape.
			var kept []string
			for _, b := range bodies {
				kept = append(kept, ansi.Hardwrap(expandTabs(b, 8), avail, true))
			}
			wrapped = strings.Join(kept, "\n")
		} else {
			wrapped = wrapWords(strings.Join(bodies, " "), avail)
		}
		for _, ln := range strings.Split(wrapped, "\n") {
			out = append(out, base.Render(prefix)+styleWithLinks(ln, base, th, plainUI))
		}
	}
	return strings.Join(out, "\n")
}

// renderListRun renders a run of list items, folding each item's wrapped
// continuation lines back into the item they belong to.
func renderListRun(lines []string, width int, th Theme, plainUI bool) string {
	var items []string
	var cur string
	flush := func() {
		if cur == "" {
			return
		}
		items = append(items, cur)
		cur = ""
	}
	for _, line := range lines {
		switch {
		case isListContinuation(line) && cur != "":
			cur += " " + strings.TrimSpace(line)
		default:
			flush()
			if body, ok := splitBulletMarker(line); ok {
				cur = body
				continue
			}
			if _, body, ok := splitNumberedListItem(strings.TrimLeft(line, " \t")); ok {
				cur = body
				continue
			}
			// Routed here but parses as neither: render verbatim, never drop.
			cur = strings.TrimSpace(line)
		}
	}
	flush()

	base := lipgloss.NewStyle().Background(th.Bg)
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, styleBlockWithLinks(wrapBullet(item, width, plainUI), base, th, plainUI))
	}
	return strings.Join(out, "\n")
}

// renderPreformattedBlock preserves the sender's line breaks, expanding tabs so
// column alignment survives and hard-wrapping only lines that overflow.
func renderPreformattedBlock(lines []string, width int, th Theme, plainUI bool) string {
	base := lipgloss.NewStyle().Background(th.Bg)
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		ln = expandTabs(ln, 8)
		for _, seg := range strings.Split(ansi.Hardwrap(ln, max(1, width), true), "\n") {
			out = append(out, styleWithLinks(seg, base, th, plainUI))
		}
	}
	return strings.Join(out, "\n")
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
			// splitBulletMarker, not a greedy TrimLeft: "- -5 degrees" must
			// keep its minus sign and "--- separator" must not become empty.
			body, ok := splitBulletMarker(line)
			if !ok {
				body = strings.TrimSpace(line)
			}
			if body == "" {
				continue
			}
			items = append(items, wrapBullet(body, width, plainUI))
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
		// Strip C0 control bytes (and DEL) that the terminal would interpret as
		// escape sequences. This runs after HTML entities are decoded but before
		// lipgloss styling is applied, so it neutralizes both raw and
		// entity-encoded (e.g. &#27;) escapes in message content without touching
		// the app's own styling codes. Tab/newline are preserved for layout.
		if (r < 0x20 && r != '\t' && r != '\n' && r != '\r') || r == 0x7f {
			return -1
		}
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
	return renderHTMLBodyOpts(html, width, th, plainUI, false)
}

// renderHTMLBodyOpts renders an HTML body, optionally stripping link targets.
// Filtering happens on the intermediate markdown — plain text, before glamour
// styling and before any OSC 8 hyperlink is emitted. Running a URL regex over
// the finished output would gut the URI out of its own escape sequence.
func renderHTMLBodyOpts(html string, width int, th Theme, plainUI, filterLinks bool) string {
	html = normalizeHTMLForRendering(html)
	converter := md.NewConverter("", true, nil)
	converter.AddRules(spanStyleRule())
	converter.AddRules(buttonLinkRule())
	converter.AddRules(imagePlaceholderRule())
	converter.AddRules(preformattedRule(width))
	converter.AddRules(tableTextRule(width))
	markdown, err := converter.ConvertString(html)
	if err != nil || strings.TrimSpace(markdown) == "" {
		return ""
	}
	markdown = stripEmailInvisibles(markdown)
	if filterLinks {
		markdown = filterLinksFromMarkdown(markdown)
	}
	rendered := renderMarkdown(markdown, width, th, plainUI)
	rendered = styleImagePlaceholders(rendered, th, plainUI)
	if width > 0 {
		rendered = ansi.Hardwrap(rendered, width, false)
	}
	if !hasMeaningfulRenderedHTML(rendered) {
		return ""
	}
	return rendered
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

func messageImageColor(th Theme) lipgloss.Color {
	accent := accentReadableOn(th.BorderFocus, th.Bg, 3.0)
	if contrastRatio(accent, th.Bg) >= 3 {
		return accent
	}
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

func styleImagePlaceholders(rendered string, th Theme, plainUI bool) string {
	if plainUI || rendered == "" {
		return rendered
	}
	style := lipgloss.NewStyle().
		Background(th.Bg).
		Foreground(messageImageColor(th)).
		Italic(true)
	return imageLabelRe.ReplaceAllStringFunc(rendered, func(match string) string {
		return style.Render(match)
	})
}

func normalizeHTMLForRendering(raw string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(raw))
	if err != nil {
		return raw
	}
	doc.Find("script,style,noscript,template,head,meta,link,input,textarea,select").Remove()
	doc.Find("[class],[id]").Each(func(_ int, s *goquery.Selection) {
		if hiddenByEmailIdentity(attrFirst(s, "class") + " " + attrFirst(s, "id")) {
			s.Remove()
		}
	})
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

func hasMeaningfulRenderedHTML(rendered string) bool {
	text := normalizeInlineSpacing(ansi.Strip(rendered))
	if text == "" {
		return false
	}
	lower := strings.ToLower(text)
	phrase := strings.TrimSpace(strings.NewReplacer("[", "", "]", "").Replace(lower))
	switch phrase {
	case "view in browser", "open in browser", "view this email in your browser", "unsubscribe", "manage preferences":
		return false
	}
	boilerplate := strings.NewReplacer(
		"[", " ",
		"]", " ",
		":", " ",
		"|", " ",
		"•", " ",
		"*", " ",
		"-", " ",
		"_", " ",
	).Replace(lower)
	words := strings.Fields(boilerplate)
	if len(words) == 0 {
		return false
	}
	contentWords := 0
	for _, word := range words {
		switch word {
		case "image", "logo", "icon", "spacer", "pixel", "tracking", "open", "view", "browser":
			continue
		default:
			contentWords++
		}
	}
	return contentWords > 0
}

func parseInlineStyle(raw string) map[string]string {
	declarations := make(map[string]string)
	for _, declaration := range strings.Split(raw, ";") {
		name, value, ok := strings.Cut(declaration, ":")
		if !ok {
			continue
		}
		name = strings.ToLower(strings.TrimSpace(name))
		value = strings.ToLower(strings.TrimSpace(value))
		value = strings.TrimSpace(strings.TrimSuffix(value, "!important"))
		if name != "" {
			declarations[name] = value
		}
	}
	return declarations
}

func hiddenByInlineStyle(style map[string]string) bool {
	if style["display"] == "none" {
		return true
	}
	if visibility := style["visibility"]; visibility == "hidden" || visibility == "collapse" {
		return true
	}
	if strings.Contains(style["mso-hide"], "all") {
		return true
	}
	return isZeroDimension(style["opacity"]) || isZeroDimension(style["font-size"])
}

func hiddenByEmailIdentity(identity string) bool {
	identity = strings.ToLower(identity)
	for _, token := range []string{
		"preheader", "preview-text", "preview_text", "hidden-preheader",
		"email-hidden", "gmail-fix",
	} {
		if strings.Contains(identity, token) {
			return true
		}
	}
	return false
}

func isTrackingImage(selec *goquery.Selection) bool {
	style := parseInlineStyle(attrFirst(selec, "style"))
	width, widthOK := cssDimension(attrFirst(selec, "width"))
	if styled, ok := cssDimension(style["width"]); ok {
		width, widthOK = styled, true
	}
	height, heightOK := cssDimension(attrFirst(selec, "height"))
	if styled, ok := cssDimension(style["height"]); ok {
		height, heightOK = styled, true
	}
	return widthOK && heightOK && width <= 1 && height <= 1
}

func isZeroDimension(value string) bool {
	dimension, ok := cssDimension(value)
	return ok && dimension == 0
}

func cssDimension(value string) (float64, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	match := dimensionRe.FindStringSubmatch(value)
	if len(match) != 2 {
		return 0, false
	}
	dimension, err := strconv.ParseFloat(match[1], 64)
	return dimension, err == nil
}

func removeEmailSpacerElements(doc *goquery.Document) {
	doc.Find("td,th,div,p,span").Each(func(_ int, selec *goquery.Selection) {
		if !isEmailSpacerElement(selec) {
			return
		}
		selec.Remove()
	})
}

func isEmailSpacerElement(selec *goquery.Selection) bool {
	if selec.Children().Length() > 0 {
		onlySpacerImages := true
		selec.Children().EachWithBreak(func(_ int, child *goquery.Selection) bool {
			if child.Is("img") && isTrackingImage(child) {
				return true
			}
			onlySpacerImages = false
			return false
		})
		if !onlySpacerImages {
			return false
		}
	}
	text := strings.TrimSpace(strings.ReplaceAll(selec.Text(), "\u00a0", " "))
	if text != "" {
		return false
	}
	style := parseInlineStyle(attrFirst(selec, "style"))
	width, widthOK := cssDimension(attrFirst(selec, "width"))
	if styled, ok := cssDimension(style["width"]); ok {
		width, widthOK = styled, true
	}
	height, heightOK := cssDimension(attrFirst(selec, "height"))
	if styled, ok := cssDimension(style["height"]); ok {
		height, heightOK = styled, true
	}
	lineHeight, lineHeightOK := cssDimension(style["line-height"])
	if widthOK && width <= 1 {
		return true
	}
	if heightOK && height <= 1 {
		return true
	}
	if lineHeightOK && lineHeight <= 1 {
		return true
	}
	return false
}

func normalizeEmailQuotes(doc *goquery.Document) {
	doc.Find("div,section").Each(func(_ int, selec *goquery.Selection) {
		if selec.ParentsFiltered("blockquote").Length() > 0 || selec.Find("blockquote").Length() > 0 {
			return
		}
		identity := strings.ToLower(attrFirst(selec, "class") + " " + attrFirst(selec, "id"))
		quoted := false
		for _, marker := range []string{
			"gmail_quote", "yahoo_quoted", "protonmail_quote", "moz-cite-prefix",
			"divrplyfwdmsg", "appendonsend", "replyforwardmessage",
		} {
			if strings.Contains(identity, marker) {
				quoted = true
				break
			}
		}
		if !quoted {
			return
		}
		for _, node := range selec.Nodes {
			node.Data = "blockquote"
			node.DataAtom = atom.Blockquote
		}
	})
}

func normalizeEmailTables(doc *goquery.Document) {
	var tables []*goquery.Selection
	doc.Find("table").Each(func(_ int, table *goquery.Selection) {
		tables = append(tables, table)
	})
	for i := len(tables) - 1; i >= 0; i-- {
		if isDataTable(tables[i]) {
			continue
		}
		flattenLayoutTable(tables[i])
	}
}

func isDataTable(table *goquery.Selection) bool {
	if strings.EqualFold(strings.TrimSpace(attrFirst(table, "role")), "table") {
		return true
	}
	if strings.TrimSpace(table.ChildrenFiltered("caption").Text()) != "" {
		return true
	}
	foundHeader := false
	table.Find("th").EachWithBreak(func(_ int, header *goquery.Selection) bool {
		foundHeader = selectionBelongsToTable(header, table)
		return !foundHeader
	})
	return foundHeader
}

func flattenLayoutTable(table *goquery.Selection) {
	_ = table.SetAttr("data-tidemail-layout-table", "true")
	table.Find("td,th").Each(func(_ int, cell *goquery.Selection) {
		if selectionBelongsToTable(cell, table) {
			_ = cell.SetAttr("data-tidemail-layout-cell", "true")
		}
	})
	table.Find("thead,tbody,tfoot,tr,td,th").AddSelection(table).Each(func(_ int, selec *goquery.Selection) {
		if !selectionBelongsToTable(selec, table) {
			return
		}
		for _, node := range selec.Nodes {
			if node.DataAtom == atom.Td || node.DataAtom == atom.Th {
				node.AppendChild(&xhtml.Node{Type: xhtml.ElementNode, Data: "br", DataAtom: atom.Br})
			}
			node.Data = "div"
			node.DataAtom = atom.Div
		}
	})
}

func selectionBelongsToTable(selec, table *goquery.Selection) bool {
	owner := selec.Closest("table")
	return owner.Length() > 0 && table.Length() > 0 && owner.Get(0) == table.Get(0)
}

func tableTextRule(width int) md.Rule {
	return md.Rule{
		Filter: []string{"table"},
		Replacement: func(content string, selec *goquery.Selection, _ *md.Options) *string {
			if selec.Find("table").Length() > 0 || !isDataTable(selec) {
				return &content
			}

			var rows [][]string
			selec.Find("tr").Each(func(_ int, tr *goquery.Selection) {
				if !selectionBelongsToTable(tr, selec) {
					return
				}
				var row []string
				tr.ChildrenFiltered("th,td").Each(func(_ int, cell *goquery.Selection) {
					row = append(row, tableCellText(cell))
				})
				if len(row) > 0 {
					rows = append(rows, row)
				}
			})
			if len(rows) == 0 {
				return md.String("")
			}

			tableWidth := max(1, width-4)
			firstRow := selec.Find("tr").FilterFunction(func(_ int, row *goquery.Selection) bool {
				return selectionBelongsToTable(row, selec)
			}).First()
			lines := renderTextTable(rows, tableWidth, firstRow.ChildrenFiltered("th").Length() > 0)
			lines = append([]string{"```"}, lines...)
			lines = append(lines, "```")
			caption := normalizeInlineSpacing(selec.ChildrenFiltered("caption").Text())
			if caption != "" {
				lines = append([]string{"**" + caption + "**", ""}, lines...)
			}
			return md.String("\n\n" + strings.Join(lines, "\n") + "\n\n")
		},
	}
}

func tableCellText(cell *goquery.Selection) string {
	parts := []string{normalizeInlineSpacing(cell.Text())}
	cell.Find("img").Each(func(_ int, image *goquery.Selection) {
		label := normalizeInlineSpacing(attrFirst(image, "alt", "title", "aria-label"))
		if label != "" && !isDecorativeImageLabel(label) {
			parts = append(parts, "[image: "+label+"]")
		}
	})
	return normalizeInlineSpacing(strings.Join(parts, " "))
}

func renderTextTable(rows [][]string, width int, header bool) []string {
	cols := 0
	for _, row := range rows {
		cols = max(cols, len(row))
	}
	if cols == 0 {
		return nil
	}

	separatorWidth := 3 * (cols - 1)
	if width <= separatorWidth+cols {
		var lines []string
		for _, row := range rows {
			lines = append(lines, strings.Split(wrapWords(strings.Join(row, " | "), width), "\n")...)
		}
		return lines
	}

	desired := make([]int, cols)
	for _, row := range rows {
		for col, cell := range row {
			desired[col] = max(desired[col], lipgloss.Width(cell))
		}
	}
	columnWidths := allocateColumnWidths(desired, width-separatorWidth)
	var lines []string
	for rowIdx, row := range rows {
		wrapped := make([][]string, cols)
		rowHeight := 1
		for col := 0; col < cols; col++ {
			cell := ""
			if col < len(row) {
				cell = row[col]
			}
			wrapped[col] = strings.Split(wrapWords(cell, columnWidths[col]), "\n")
			rowHeight = max(rowHeight, len(wrapped[col]))
		}
		for lineIdx := 0; lineIdx < rowHeight; lineIdx++ {
			cells := make([]string, cols)
			for col := 0; col < cols; col++ {
				cell := ""
				if lineIdx < len(wrapped[col]) {
					cell = wrapped[col][lineIdx]
				}
				cells[col] = cell + strings.Repeat(" ", columnWidths[col]-lipgloss.Width(cell))
			}
			lines = append(lines, strings.TrimRight(strings.Join(cells, " | "), " "))
		}
		if header && rowIdx == 0 {
			separators := make([]string, cols)
			for col := range separators {
				separators[col] = strings.Repeat("-", columnWidths[col])
			}
			lines = append(lines, strings.Join(separators, "-+-"))
		}
	}
	return lines
}

func allocateColumnWidths(desired []int, available int) []int {
	widths := make([]int, len(desired))
	for i := range widths {
		widths[i] = 1
		desired[i] = max(1, desired[i])
	}
	remaining := max(0, available-len(widths))
	for remaining > 0 {
		grew := false
		for i := range widths {
			if widths[i] >= desired[i] || remaining == 0 {
				continue
			}
			widths[i]++
			remaining--
			grew = true
		}
		if !grew {
			break
		}
	}
	return widths
}

func preformattedRule(width int) md.Rule {
	return md.Rule{
		Filter: []string{"pre"},
		Replacement: func(_ string, selec *goquery.Selection, _ *md.Options) *string {
			text := stripEmailInvisibles(strings.ReplaceAll(selec.Text(), "\r\n", "\n"))
			text = strings.Trim(text, "\n")
			if text == "" {
				return md.String("")
			}
			codeWidth := max(1, width-4)
			lines := strings.Split(text, "\n")
			for i, line := range lines {
				lines[i] = ansi.Hardwrap(line, codeWidth, true)
			}
			fence := "```"
			if strings.Contains(text, fence) {
				fence = "````"
			}
			return md.String("\n\n" + fence + "\n" + strings.Join(lines, "\n") + "\n" + fence + "\n\n")
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
			imageLabel := firstUsefulImageLabel(selec)
			if text == "" {
				text = imageLabel
			}
			if text == "" {
				text = normalizeInlineSpacing(content)
			}
			if text == "" {
				text = normalizeInlineSpacing(attrFirst(selec, "aria-label", "title"))
			}
			if href == "" {
				return md.String(" ")
			}
			if selec.Find("table,[data-tidemail-layout-table]").Length() > 0 {
				text = layoutCellText(selec)
				if text == "" {
					return md.String(" ")
				}
				if label := firstUsefulImageLabel(selec); label != "" && text == label {
					text = "[image: " + label + "]"
				}
				return md.String("\n\n" + text + "\n\n")
			}
			if !looksLikeButtonLink(selec) {
				if text == "" {
					return md.String(" ")
				}
				if imageLabel != "" && text == imageLabel {
					return md.String("\n\n[image: " + imageLabel + "]\n\n")
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

func firstUsefulImageLabel(selec *goquery.Selection) string {
	label := ""
	selec.Find("img").EachWithBreak(func(_ int, image *goquery.Selection) bool {
		candidate := normalizeInlineSpacing(attrFirst(image, "alt", "title", "aria-label"))
		if candidate == "" || isDecorativeImageLabel(candidate) {
			return true
		}
		label = candidate
		return false
	})
	return label
}

func layoutCellText(selec *goquery.Selection) string {
	var parts []string
	selec.Find("td,th,[data-tidemail-layout-cell]").Each(func(_ int, cell *goquery.Selection) {
		if cell.Find("td,th,[data-tidemail-layout-cell]").Length() > 0 {
			return
		}
		text := normalizeInlineSpacing(htmlstd.UnescapeString(cell.Text()))
		if text != "" {
			parts = append(parts, text)
		}
		cell.Find("img").Each(func(_ int, image *goquery.Selection) {
			label := normalizeInlineSpacing(attrFirst(image, "alt", "title", "aria-label"))
			if label != "" && !isDecorativeImageLabel(label) {
				parts = append(parts, "[image: "+label+"]")
			}
		})
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
		strings.Contains(className, "cta") ||
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
