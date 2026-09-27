package ui

import (
	"fmt"
	"math"
	"strings"

	"github.com/allisonhere/tidemail/internal/plugin"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Structured plugin views (plugin.View) drawn in the user's theme. Plugins
// supply only semantics — numbers, labels, tones, "these are categories" —
// and every color here comes from the theme or the user's tag colors. Every
// styled segment carries the panel background so the terminal's own
// background never shows through.

var (
	viewSparkBlocks = []rune("▁▂▃▄▅▆▇█")
	viewBarEighths  = []rune("▏▎▍▌▋▊▉")
)

// viewPainter holds the palette for one render.
type viewPainter struct {
	m      Model
	chrome managerChrome
	width  int
	plain  bool
}

func (p viewPainter) style(fg lipgloss.Color) lipgloss.Style {
	s := lipgloss.NewStyle().Background(p.chrome.baseBg)
	if p.plain {
		return lipgloss.NewStyle()
	}
	if fg != "" {
		s = s.Foreground(fg)
	}
	return s
}

func (p viewPainter) text() lipgloss.Style  { return p.style(p.chrome.text) }
func (p viewPainter) muted() lipgloss.Style { return p.style(p.chrome.muted) }
func (p viewPainter) accent() lipgloss.Color {
	return accentReadableOn(p.chrome.accent, p.chrome.baseBg, 3)
}

// toneColor maps a plugin's tone to a theme color.
func (p viewPainter) toneColor(tone string) lipgloss.Color {
	switch tone {
	case plugin.ToneAttention:
		return accentReadableOn(p.m.tagColorFor(tagKeyImportant).bg, p.chrome.baseBg, 3)
	case plugin.ToneCritical:
		return accentReadableOn(p.m.tagColorFor(tagKeyUrgent).bg, p.chrome.baseBg, 3)
	case plugin.TonePositive:
		return accentReadableOn(p.chrome.successFg, p.chrome.baseBg, 3)
	}
	return p.accent()
}

// seriesColor gives each sparkline series its own theme hue.
func (p viewPainter) seriesColor(i int) lipgloss.Color {
	palette := []lipgloss.Color{p.accent(), p.m.tagColorFor(tagKeyReply).bg, p.chrome.successFg}
	return accentReadableOn(palette[i%len(palette)], p.chrome.baseBg, 3)
}

// ramp returns a color from faint (t=0) to full strength (t=1) of c.
func (p viewPainter) ramp(c lipgloss.Color, t float64) lipgloss.Color {
	return mixColors(p.chrome.baseBg, c, 0.3+0.7*math.Max(0, math.Min(1, t)))
}

// pluginViewLines renders a view to panel lines of at most width cells.
func (m Model) pluginViewLines(v *plugin.View, width int, chrome managerChrome) []string {
	p := viewPainter{m: m, chrome: chrome, width: max(20, width), plain: chrome.plainUI}
	var lines []string
	title := p.style(p.accent()).Bold(true).Render(sanitizePluginLine(v.Title))
	if v.Subtitle != "" {
		title += p.muted().Render("  " + sanitizePluginLine(v.Subtitle))
	}
	lines = append(lines, truncateStyled(title, p.width, chrome.baseBg), "")
	for _, b := range v.Blocks {
		if b.Title != "" || b.Note != "" {
			lines = append(lines, p.heading(b.Title, b.Note))
		}
		switch b.Type {
		case plugin.BlockStats:
			lines = append(lines, p.stats(b.Items)...)
		case plugin.BlockSparkline:
			lines = append(lines, p.sparklines(b)...)
		case plugin.BlockBars:
			lines = append(lines, p.bars(b)...)
		case plugin.BlockHeatmap:
			lines = append(lines, p.heatmap(b)...)
		case plugin.BlockTable:
			lines = append(lines, p.table(b)...)
		case plugin.BlockText:
			for _, l := range strings.Split(sanitizePluginText(b.Text), "\n") {
				for _, part := range strings.Split(ansi.Wrap(l, p.width, ""), "\n") {
					lines = append(lines, p.text().Render(part))
				}
			}
		}
		lines = append(lines, "")
	}
	return lines
}

// heading is a section title with its note right-aligned.
func (p viewPainter) heading(title, note string) string {
	left := p.style(p.accent()).Render("▌") + p.style(p.chrome.text).Bold(true).Render(" "+sanitizePluginLine(title))
	note = sanitizePluginLine(note)
	gap := p.width - lipgloss.Width(left) - lipgloss.Width(note)
	if note == "" || gap < 2 {
		return truncateStyled(left, p.width, p.chrome.baseBg)
	}
	return left + p.muted().Render(strings.Repeat(" ", gap)+note)
}

// stats draws tiles, wrapping to as many rows as the width needs.
func (p viewPainter) stats(items []plugin.ViewItem) []string {
	var tiles []string
	anyNote := false
	for _, it := range items {
		anyNote = anyNote || it.Note != ""
	}
	for _, it := range items {
		label := strings.ToUpper(sanitizePluginLine(it.Label))
		value := sanitizePluginLine(it.Value)
		note := sanitizePluginLine(it.Note)
		inner := max(10, lipgloss.Width(label), lipgloss.Width(value), lipgloss.Width(note)) + 2
		c := p.toneColor(it.Tone)
		box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Width(inner).Align(lipgloss.Center)
		body := []string{p.muted().Width(inner).Align(lipgloss.Center).Render(label),
			p.style(c).Bold(true).Width(inner).Align(lipgloss.Center).Render(value)}
		if anyNote { // same height for every tile in the block
			body = append(body, p.muted().Width(inner).Align(lipgloss.Center).Render(note))
		}
		if !p.plain {
			box = box.BorderForeground(c).BorderBackground(p.chrome.baseBg).Background(p.chrome.baseBg)
		}
		tiles = append(tiles, box.Render(strings.Join(body, "\n")))
	}
	var rows []string
	var row []string
	used := 0
	flush := func() {
		if len(row) == 0 {
			return
		}
		// The spacer is as tall as the tiles: JoinHorizontal would pad a
		// one-line spacer with unstyled spaces.
		gap := strings.TrimRight(strings.Repeat(p.style("").Render(" ")+"\n", lipgloss.Height(row[0])), "\n")
		joined := row[0]
		for _, t := range row[1:] {
			joined = lipgloss.JoinHorizontal(lipgloss.Top, joined, gap, t)
		}
		rows = append(rows, strings.Split(joined, "\n")...)
		row, used = nil, 0
	}
	for _, t := range tiles {
		w := lipgloss.Width(t)
		if used > 0 && used+1+w > p.width {
			flush()
		}
		if used > 0 {
			used++
		}
		row = append(row, t)
		used += w
	}
	flush()
	return rows
}

// sparklines draws each series as a colored sparkline whose blocks brighten
// with height, sharing one scale so series compare honestly.
func (p viewPainter) sparklines(b plugin.Block) []string {
	labelW := 0
	for _, s := range b.Series {
		labelW = max(labelW, lipgloss.Width(sanitizePluginLine(s.Label)))
	}
	cols := max(8, p.width-labelW-2)
	peak := 0.0
	series := make([][]float64, len(b.Series))
	for i, s := range b.Series {
		series[i] = stretchValues(downsampleValues(s.Values, cols), cols)
		for _, v := range series[i] {
			peak = math.Max(peak, v)
		}
	}
	var lines []string
	for i, s := range b.Series {
		c := p.seriesColor(i)
		var sb strings.Builder
		sb.WriteString(p.muted().Render(padRight(sanitizePluginLine(s.Label), labelW) + "  "))
		for _, v := range series[i] {
			level := 0
			if peak > 0 && v > 0 {
				level = max(1, int(math.Round(v/peak*float64(len(viewSparkBlocks)-1))))
			}
			sb.WriteString(p.style(p.ramp(c, float64(level)/float64(len(viewSparkBlocks)-1))).Render(string(viewSparkBlocks[level])))
		}
		lines = append(lines, sb.String())
	}
	if b.Start != "" || b.End != "" {
		n := len(series[0])
		start, end := sanitizePluginLine(b.Start), sanitizePluginLine(b.End)
		gap := max(1, n-lipgloss.Width(start)-lipgloss.Width(end))
		lines = append(lines, p.muted().Render(strings.Repeat(" ", labelW+2)+start+strings.Repeat(" ", gap)+end))
	}
	return lines
}

// bars draws horizontal bars with eighth-block precision. Category bars use
// the same colors as category tags.
func (p viewPainter) bars(b plugin.Block) []string {
	labelW, noteW, countW := 0, 0, 0
	peak := 0.0
	for _, it := range b.Items {
		labelW = max(labelW, lipgloss.Width(sanitizePluginLine(it.Label)))
		noteW = max(noteW, lipgloss.Width(sanitizePluginLine(it.Note)))
		countW = max(countW, len(formatCount(it.Count)))
		peak = math.Max(peak, it.Count)
	}
	labelW = min(labelW, 18)
	barW := max(4, p.width-labelW-noteW-countW-4)
	var lines []string
	for i, it := range b.Items {
		label := sanitizePluginLine(it.Label)
		c := p.accent()
		if b.Kind == plugin.BarsCategory {
			c = accentReadableOn(p.m.tagColorFor(tagKeyCategory+"."+strings.ToLower(label)).bg, p.chrome.baseBg, 2)
		} else if i > 0 {
			c = p.ramp(p.accent(), 1-0.5*float64(i)/float64(max(1, len(b.Items)-1)))
		}
		line := p.text().Render(padRight(truncateText(label, labelW), labelW)+" ") +
			p.style(c).Render(barCells(it.Count, peak, barW)) +
			p.muted().Render(" "+padLeft(sanitizePluginLine(it.Note), noteW)+" ") +
			p.text().Render(padLeft(formatCount(it.Count), countW))
		lines = append(lines, line)
	}
	return lines
}

// heatmap shades each cell from faint to the accent by its value.
func (p viewPainter) heatmap(b plugin.Block) []string {
	labelW := 0
	cols := 0
	peak := 0.0
	for _, r := range b.Rows {
		labelW = max(labelW, lipgloss.Width(sanitizePluginLine(r.Label)))
		cols = max(cols, len(r.Values))
		for _, v := range r.Values {
			peak = math.Max(peak, v)
		}
	}
	cellW := min(6, max(2, (p.width-labelW-1)/max(1, cols)))
	var lines []string
	if len(b.Columns) > 0 && cellW >= 2 {
		var sb strings.Builder
		sb.WriteString(strings.Repeat(" ", labelW+1))
		for _, c := range b.Columns[:min(len(b.Columns), cols)] {
			sb.WriteString(padRight(truncateText(sanitizePluginLine(c), cellW-1), cellW))
		}
		lines = append(lines, p.muted().Render(sb.String()))
	}
	c := p.accent()
	for _, r := range b.Rows {
		var sb strings.Builder
		sb.WriteString(p.muted().Render(padRight(sanitizePluginLine(r.Label), labelW) + " "))
		for _, v := range r.Values {
			t := 0.0
			if peak > 0 {
				t = v / peak
			}
			// Lower blocks leave a sliver between rows, so cells read as a
			// grid rather than stripes.
			glyphs := max(1, cellW-1)
			cell := strings.Repeat("▆", glyphs) + strings.Repeat(" ", cellW-glyphs)
			if v == 0 {
				cell = padRight(" ·", cellW)
				sb.WriteString(p.muted().Render(cell))
				continue
			}
			// Busy cells warm from the accent toward the theme's highlight.
			heat := mixColors(p.ramp(c, t), p.toneColor(plugin.ToneAttention), 0.7*t*t)
			sb.WriteString(p.style(heat).Render(cell))
		}
		lines = append(lines, sb.String())
	}
	return lines
}

// table lays columns out to their content, shrinking the widest to fit.
func (p viewPainter) table(b plugin.Block) []string {
	widths := make([]int, len(b.Columns))
	for i, c := range b.Columns {
		widths[i] = lipgloss.Width(sanitizePluginLine(c))
	}
	for _, row := range b.Cells {
		for i, c := range row {
			widths[i] = max(widths[i], lipgloss.Width(sanitizePluginLine(c)))
		}
	}
	for total := sumInts(widths) + 2*(len(widths)-1); total > p.width; total = sumInts(widths) + 2*(len(widths)-1) {
		widest := 0
		for i := range widths {
			if widths[i] > widths[widest] {
				widest = i
			}
		}
		if widths[widest] <= 4 {
			break
		}
		widths[widest]--
	}
	render := func(cells []string, st lipgloss.Style) string {
		parts := make([]string, len(cells))
		for i, c := range cells {
			parts[i] = padRight(truncateText(sanitizePluginLine(c), widths[i]), widths[i])
		}
		return st.Render(padRight(strings.Join(parts, "  "), p.width))
	}
	lines := []string{render(b.Columns, p.style(p.accent()).Bold(true))}
	for i, row := range b.Cells {
		st := p.text()
		if i%2 == 1 && !p.plain {
			st = st.Background(mixColors(p.chrome.baseBg, p.chrome.text, 0.05))
		}
		lines = append(lines, render(row, st))
	}
	return lines
}

func barCells(v, peak float64, width int) string {
	if peak <= 0 || v <= 0 {
		return strings.Repeat(" ", width)
	}
	eighths := int(v / peak * float64(width*8))
	eighths = max(1, eighths)
	s := strings.Repeat("█", eighths/8)
	if r := eighths % 8; r > 0 {
		s += string(viewBarEighths[r-1])
	}
	return padRight(s, width)
}

// stretchValues repeats each value so a short series fills the width.
func stretchValues(values []float64, cols int) []float64 {
	if len(values) == 0 || len(values)*2 > cols {
		return values
	}
	k := cols / len(values)
	out := make([]float64, 0, len(values)*k)
	for _, v := range values {
		for j := 0; j < k; j++ {
			out = append(out, v)
		}
	}
	return out
}

func downsampleValues(values []float64, cols int) []float64 {
	if cols <= 0 || len(values) <= cols {
		return values
	}
	group := (len(values) + cols - 1) / cols
	var out []float64
	for i := 0; i < len(values); i += group {
		sum := 0.0
		for _, v := range values[i:min(i+group, len(values))] {
			sum += v
		}
		out = append(out, sum)
	}
	return out
}

func formatCount(v float64) string {
	if v == math.Trunc(v) {
		n := int64(v)
		s := fmt.Sprint(n)
		for i := len(s) - 3; i > 0; i -= 3 {
			s = s[:i] + "," + s[i:]
		}
		return s
	}
	return fmt.Sprintf("%.1f", v)
}

func truncateText(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	return ansi.Truncate(s, max(0, w), "…")
}

func padLeft(s string, w int) string {
	return strings.Repeat(" ", max(0, w-lipgloss.Width(s))) + s
}

func sumInts(v []int) int {
	n := 0
	for _, x := range v {
		n += x
	}
	return n
}
