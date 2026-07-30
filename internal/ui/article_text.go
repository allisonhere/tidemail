package ui

import (
	"math"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

// Line-shape parsers for plain-text message bodies.
//
// These replace greedy strings.TrimLeft calls, which mangled real content:
// TrimLeft(s, "-") turned "--- separator" into "" and "- -5 degrees" into
// "5 degrees", and TrimLeft(s, ">") flattened nested quote depth.

// splitATXHeading matches a markdown ATX heading: 1-6 '#' followed by
// whitespace. The whitespace requirement is what keeps "#1 priority",
// "#hashtag", "#!/bin/sh" and "#include <foo>" as ordinary prose.
func splitATXHeading(line string) (text string, level int, ok bool) {
	t := strings.TrimLeft(line, " \t")
	i := 0
	for i < len(t) && t[i] == '#' {
		i++
	}
	if i == 0 || i > 6 || i >= len(t) {
		return "", 0, false
	}
	if t[i] != ' ' && t[i] != '\t' {
		return "", 0, false
	}
	text = strings.TrimSpace(strings.TrimRight(t[i:], " \t#"))
	if text == "" {
		return "", 0, false
	}
	return text, i, true
}

// splitQuotePrefix counts the '>' markers that open a quoted line, allowing
// spaces between them ("> > x" is depth 2), and returns the remaining body.
// Depth is preserved so nesting can be rendered, not flattened.
func splitQuotePrefix(line string) (depth int, body string, ok bool) {
	i := 0
	for i < len(line) {
		switch line[i] {
		case ' ', '\t':
			i++
			continue
		case '>':
			depth++
			i++
			continue
		}
		break
	}
	if depth == 0 {
		return 0, "", false
	}
	return depth, strings.TrimLeft(line[i:], " \t"), true
}

// splitBulletMarker matches a list bullet: exactly one marker rune followed by
// whitespace. "- -5 degrees" keeps its minus sign; "---" is not a bullet.
func splitBulletMarker(line string) (body string, ok bool) {
	t := strings.TrimLeft(line, " \t")
	r, size := utf8.DecodeRuneInString(t)
	switch r {
	case '-', '*', '+', '•':
	default:
		return "", false
	}
	rest := t[size:]
	if rest == "" || (rest[0] != ' ' && rest[0] != '\t') {
		return "", false
	}
	body = strings.TrimSpace(rest)
	if body == "" {
		return "", false
	}
	return body, true
}

func isQuoteLine(s string) bool {
	_, _, ok := splitQuotePrefix(s)
	return ok
}

func isHeadingLine(s string) bool {
	_, _, ok := splitATXHeading(s)
	return ok
}

func isListLine(s string) bool {
	if _, ok := splitBulletMarker(s); ok {
		return true
	}
	return isNumberedListItem(strings.TrimLeft(s, " \t"))
}

// isListContinuation reports whether a line is the wrapped remainder of the
// list item above it: indented, and not the start of some other construct.
func isListContinuation(s string) bool {
	if strings.TrimSpace(s) == "" {
		return false
	}
	if !strings.HasPrefix(s, "  ") && !strings.HasPrefix(s, "\t") {
		return false
	}
	return !isListLine(s) && !isQuoteLine(s) && !isHeadingLine(s)
}

func isProseLine(s string) bool {
	if strings.TrimSpace(s) == "" {
		return false
	}
	return !isQuoteLine(s) && !isHeadingLine(s) && !isListLine(s)
}

// expandTabs converts tabs to spaces at fixed stops. stripEmailInvisibles
// deliberately preserves '\t', but lipgloss.Width cannot measure it, so any
// tab left in the output breaks both wrapping math and column alignment.
func expandTabs(s string, stop int) string {
	if !strings.Contains(s, "\t") || stop <= 0 {
		return s
	}
	var b strings.Builder
	col := 0
	for _, r := range s {
		if r == '\t' {
			pad := stop - (col % stop)
			b.WriteString(strings.Repeat(" ", pad))
			col += pad
			continue
		}
		b.WriteRune(r)
		col += lipgloss.Width(string(r))
	}
	return b.String()
}

// Pre-formatted detection.
//
// Plain-text mail mixes prose that should reflow to the pane width with blocks
// whose line breaks carry meaning: postal addresses, ASCII tables, code, logs
// and signature blocks. Reflowing the latter destroys them; preserving the
// former leaves ragged half-width text. These heuristics separate the two.

var (
	leadingIndentRe = regexp.MustCompile(`^[ \t]+\S`)
	columnGapRe     = regexp.MustCompile(`\S {2,}\S`)
	rulerRe         = regexp.MustCompile(`^[ \t]*[-=_*~+#]{4,}[ \t]*$`)
	sigDelimRe      = regexp.MustCompile(`^-- ?$`)
	forwardMarkerRe = regexp.MustCompile(`(?i)^[ \t]*[-_=]{2,}[ \t]*(original message|forwarded message|begin forwarded|reply above this line)`)
)

const (
	// Indents wider than this are tracking-space padding, not layout.
	maxRealIndent = 16
	// A wrap column narrower than this is not evidence of hard-wrapped prose.
	proseMinMeanWidth = 30
	// Ceiling on stddev/mean for line widths to read as a uniform wrap column.
	// Relative, not absolute, so prose wrapped at 40 is caught like prose at 72.
	proseWidthSpread   = 0.12
	proseMaxSymbolFrac = 0.12
)

// looksPreformatted reports whether a paragraph's line breaks should be
// preserved rather than reflowed. False positives cost an extra line break;
// false negatives destroy the sender's layout, so it leans toward preserving.
func looksPreformatted(lines []string, width int) bool {
	body := nonBlankLines(lines)
	n := len(body)
	if n < 2 {
		return false
	}

	// Quoted and list runs have their own structural rendering, which applies
	// this same check to the quote/item bodies at the correct nesting level.
	// Without this guard a short quoted reply looks "all short" and would be
	// emitted verbatim, leaving the '>' markers visible.
	structured := 0
	for _, ln := range body {
		if isQuoteLine(ln) || isListLine(ln) {
			structured++
		}
	}
	if structured*2 >= n {
		return false
	}

	indented, gapped, symbolic, marker := 0, 0, 0, 0
	for _, ln := range body {
		if w := indentWidth(ln); w >= 1 && w <= maxRealIndent && leadingIndentRe.MatchString(ln) {
			indented++
		}
		if columnGapRe.MatchString(ln) {
			gapped++
		}
		if symbolFraction(ln) >= 0.30 {
			symbolic++
		}
		t := strings.TrimRight(ln, " \t")
		if rulerRe.MatchString(t) || sigDelimRe.MatchString(t) || forwardMarkerRe.MatchString(t) {
			marker++
		}
	}

	// Strong signals: any one is decisive.
	if marker >= 1 {
		return true // ruler, "-- " signature delimiter, "----- Original Message -----"
	}
	if indented >= 2 && indented*2 >= n {
		return true // code, hanging layout, quoted-in-place text
	}
	if gapped >= 2 && gapped*2 >= n {
		return true // aligned columns / ASCII table
	}

	// Anti-signal: uniformly hard-wrapped prose must keep reflowing. This is
	// checked before the weak signals because it is the dominant real case.
	if looksHardWrappedProse(body) {
		return false
	}

	if n >= 3 && allShort(body, shortLineLimit(width)) {
		return true // postal addresses, contact blocks, short code
	}
	if digitHeavy(body) {
		return true // logs, ledgers, dated tables
	}
	return symbolic*2 >= n
}

func shortLineLimit(width int) int { return min(45, max(20, width/2)) }

// looksHardWrappedProse reports whether lines cluster tightly just under a wrap
// column, are letter-dominated, and have no aligned column runs.
func looksHardWrappedProse(body []string) bool {
	if len(body) < 3 {
		return false // two lines is not enough evidence of a wrap column
	}
	// The final line of a paragraph is legitimately short; exclude it from the
	// width statistics but require it not to exceed them.
	head := body[:len(body)-1]
	mean, sd := widthStats(head)
	if mean < proseMinMeanWidth || sd > mean*proseWidthSpread {
		return false
	}
	if float64(lipgloss.Width(body[len(body)-1])) > mean+sd {
		return false
	}
	for _, ln := range body {
		if columnGapRe.MatchString(ln) {
			return false
		}
	}
	// Prose is letter-dominated. Timestamped log lines are often uniform width
	// with low symbol density, so without this they read as wrapped prose.
	if digitHeavy(body) {
		return false
	}
	return avgSymbolFraction(body) < proseMaxSymbolFrac
}

func nonBlankLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		if strings.TrimSpace(ln) != "" {
			out = append(out, ln)
		}
	}
	return out
}

func indentWidth(s string) int {
	w := 0
	for _, r := range s {
		switch r {
		case ' ':
			w++
		case '\t':
			w += 8 - (w % 8)
		default:
			return w
		}
	}
	return w
}

// symbolFraction is the share of non-space runes that are neither letters,
// digits, nor ordinary sentence punctuation.
func symbolFraction(s string) float64 {
	total, symbols := 0, 0
	for _, r := range s {
		if unicode.IsSpace(r) {
			continue
		}
		total++
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		if strings.ContainsRune(`.,'"!?;:()-`, r) {
			continue
		}
		symbols++
	}
	if total == 0 {
		return 0
	}
	return float64(symbols) / float64(total)
}

func avgSymbolFraction(lines []string) float64 {
	if len(lines) == 0 {
		return 0
	}
	var sum float64
	for _, ln := range lines {
		sum += symbolFraction(ln)
	}
	return sum / float64(len(lines))
}

func widthStats(lines []string) (mean, sd float64) {
	if len(lines) == 0 {
		return 0, 0
	}
	var sum float64
	for _, ln := range lines {
		sum += float64(lipgloss.Width(ln))
	}
	mean = sum / float64(len(lines))
	var variance float64
	for _, ln := range lines {
		d := float64(lipgloss.Width(ln)) - mean
		variance += d * d
	}
	return mean, math.Sqrt(variance / float64(len(lines)))
}

func allShort(lines []string, limit int) bool {
	for _, ln := range lines {
		if lipgloss.Width(strings.TrimSpace(ln)) > limit {
			return false
		}
	}
	return true
}

// digitHeavy reports whether at least half the lines are at least 15% digits,
// which characterizes logs, ledgers and dated tables.
func digitHeavy(lines []string) bool {
	heavy := 0
	for _, ln := range lines {
		total, digits := 0, 0
		for _, r := range ln {
			if unicode.IsSpace(r) {
				continue
			}
			total++
			if unicode.IsDigit(r) {
				digits++
			}
		}
		if total > 0 && float64(digits)/float64(total) >= 0.15 {
			heavy++
		}
	}
	return heavy > 0 && heavy*2 >= len(lines)
}

// Styling helpers.
//
// These style AFTER wrapping. The reverse order (the original code's) lets
// splitLongWord cut a long URL in the middle of an SGR sequence, and makes an
// outer style.Render() terminate early at an inner reset.

// linkSpans returns non-overlapping [start,end) byte ranges of URLs and email
// addresses. Email matches that fall inside a URL are dropped, since URLs
// frequently contain '@'.
func linkSpans(text string) [][]int {
	spans := urlRe.FindAllStringIndex(text, -1)
	for _, e := range emailRe.FindAllStringIndex(text, -1) {
		overlaps := false
		for _, u := range spans {
			if e[0] < u[1] && u[0] < e[1] {
				overlaps = true
				break
			}
		}
		if !overlaps {
			spans = append(spans, e)
		}
	}
	for i := 1; i < len(spans); i++ {
		for j := i; j > 0 && spans[j][0] < spans[j-1][0]; j-- {
			spans[j], spans[j-1] = spans[j-1], spans[j]
		}
	}
	return spans
}

// styleWithLinks renders one already-wrapped line under base, re-styling URL
// and email spans as links.
func styleWithLinks(text string, base lipgloss.Style, th Theme, plainUI bool) string {
	if plainUI || text == "" {
		return base.Render(text)
	}
	spans := linkSpans(text)
	if len(spans) == 0 {
		return base.Render(text)
	}
	link := base.Foreground(messageLinkColor(th)).Underline(true)
	var b strings.Builder
	last := 0
	for _, span := range spans {
		if span[0] > last {
			b.WriteString(base.Render(text[last:span[0]]))
		}
		raw := text[span[0]:span[1]]
		// The OSC 8 wrapper goes outside the SGR styling so the styled run stays
		// a single unbroken span; both are zero-width to lipgloss.Width.
		b.WriteString(osc8Link(cleanDetectedURL(raw), link.Render(raw), plainUI))
		last = span[1]
	}
	if last < len(text) {
		b.WriteString(base.Render(text[last:]))
	}
	return b.String()
}

// styleBlockWithLinks applies styleWithLinks to every line of a wrapped block.
func styleBlockWithLinks(wrapped string, base lipgloss.Style, th Theme, plainUI bool) string {
	lines := strings.Split(wrapped, "\n")
	for i := range lines {
		lines[i] = styleWithLinks(lines[i], base, th, plainUI)
	}
	return strings.Join(lines, "\n")
}
