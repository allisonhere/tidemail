package ui

import (
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Prototype: a tideftp-inspired animated wave screen for Settings -> About,
// toggled with `w` (see the key check in Settings.Update). Kept in its own
// file so it's easy to review or remove independently of the real About
// screen (renderAboutSection, elsewhere in this package), which this leaves
// completely untouched.
//
// The wave itself ports the layered-sine-wave technique from
// ~/Projects/tideftp/internal/about/about.go: three sine terms at different
// spatial/temporal frequencies and phases, summed with a baseline, give each
// column's "water level" in rows; Unicode block glyphs (▁▂▃▄▅▆▇█) give each
// cell 8 levels of sub-cell vertical resolution. Same-color horizontal runs
// are coalesced into one styled span before emitting, same rationale as
// tideftp: fewer ANSI escapes per frame.

const (
	settingsAboutWavePeriod      = 100 * time.Millisecond // 10fps, matches tideftp
	settingsAboutWaveSettleFrame = 400                    // ~40s idle, then stop ticking
	aboutWaveRows                = 3
)

type settingsAboutWaveMsg struct{}

func settingsAboutWaveCmd() tea.Cmd {
	return tea.Tick(settingsAboutWavePeriod, func(time.Time) tea.Msg {
		return settingsAboutWaveMsg{}
	})
}

// aboutWaveBlocks gives 8 levels of sub-cell fill, bottom to top.
var aboutWaveBlocks = []rune("▁▂▃▄▅▆▇█")

// aboutWaveHeight returns a layer's water level, in rows, at column col and
// animation frame. Three layers (0=back, 1=mid, 2=front) at increasing
// amplitude and decreasing baseline height, each its own frequency/phase.
func aboutWaveHeight(col, frame, layer int) float64 {
	fx := float64(col)
	t := float64(frame) * 1.25
	switch layer {
	case 0:
		return aboutWaveRows*0.85 + 0.55*math.Sin(fx*0.20+t*0.10)
	case 1:
		return aboutWaveRows*0.55 + 0.65*math.Sin(fx*0.29-t*0.16+1.7)
	default:
		return aboutWaveRows*0.30 + 0.75*math.Sin(fx*0.45+t*0.23+3.1)
	}
}

// aboutWaveGlyphForRow reports whether a layer at the given water height
// reaches row (0 = top of the strip, aboutWaveRows-1 = bottom/baseline), and
// if so which block glyph represents the fractional fill at that row.
func aboutWaveGlyphForRow(height float64, row int) (rune, bool) {
	depth := float64(aboutWaveRows - row) // bottom row -> depth 1
	fill := height - (depth - 1)
	if fill <= 0 {
		return ' ', false
	}
	if fill >= 1 {
		return aboutWaveBlocks[len(aboutWaveBlocks)-1], true
	}
	idx := int(fill * float64(len(aboutWaveBlocks)-1))
	return aboutWaveBlocks[idx], true
}

// aboutWaveRow renders one row of the wave strip, width cells wide. For each
// column, the front-most (highest-index) layer that reaches this row wins,
// matching tideftp's compositing order.
func aboutWaveRow(width, row, frame int, bg lipgloss.Color, colors []lipgloss.Color) string {
	var b strings.Builder
	runLen := 0
	runGlyph := rune(0)
	runColor := bg
	flush := func() {
		if runLen == 0 {
			return
		}
		b.WriteString(lipgloss.NewStyle().Background(bg).Foreground(runColor).Render(strings.Repeat(string(runGlyph), runLen)))
	}
	for col := range width {
		glyph := ' '
		color := bg
		for layer := len(colors) - 1; layer >= 0; layer-- {
			if g, ok := aboutWaveGlyphForRow(aboutWaveHeight(col, frame, layer), row); ok {
				glyph, color = g, colors[layer]
				break
			}
		}
		if runLen > 0 && glyph == runGlyph && color == runColor {
			runLen++
			continue
		}
		flush()
		runGlyph, runColor, runLen = glyph, color, 1
	}
	flush()
	return b.String()
}

// renderAboutWaveStrip renders the full aboutWaveRows-tall strip, deriving
// three wave shades from the current theme's accent color blended toward bg
// (internal/ui/color.go's mixColors), so it stays theme-consistent without a
// separate palette system.
func (s Settings) renderAboutWaveStrip(width int, chrome managerChrome, bg lipgloss.Color) string {
	colors := []lipgloss.Color{
		mixColors(bg, chrome.accent, 0.35),
		mixColors(bg, chrome.accent, 0.62),
		mixColors(bg, chrome.accent, 0.92),
	}
	rows := make([]string, aboutWaveRows)
	for r := range aboutWaveRows {
		rows[r] = aboutWaveRow(width, r, s.aboutWaveFrame, bg, colors)
	}
	return strings.Join(rows, "\n")
}

// renderAboutWaveSection is the wave screen's equivalent of
// renderAboutSection: same settingsSectionBody shape, same link row/anchors
// (so Tab navigation and Enter/Space activation on the Support/Repository/
// Issues buttons keep working unchanged), but with the wave strip standing
// in for the DNA-helix hero and heart, header and footer.
func (s Settings) renderAboutWaveSection(width int, chrome managerChrome) settingsSectionBody {
	bodyW := max(1, width)
	aboutBg := lipgloss.Color("#000000")
	blank := lipgloss.NewStyle().Background(aboutBg).Width(bodyW).Render("")
	center := func(text string, fg lipgloss.Color, italic bool) string {
		return lipgloss.NewStyle().Background(aboutBg).Foreground(fg).Italic(italic).Bold(!italic).
			Width(bodyW).Align(lipgloss.Center).Render(text)
	}

	wave := s.renderAboutWaveStrip(bodyW, chrome, aboutBg)
	waveLines := strings.Split(wave, "\n")

	lines := make([]string, 0, 32)
	lines = append(lines, waveLines...)
	lines = append(lines, blank,
		center("TIDEMAIL", chrome.text, false),
		center("your mail, your rules", chrome.muted, true),
		blank,
		center(s.update.currentVersion, chrome.muted, false),
		blank,
	)

	linksBlock := s.renderAboutLinks(bodyW, chrome)
	linksLines := strings.Split(linksBlock, "\n")
	hint := center("w  back to the original about screen", chrome.muted, true)
	footerLen := 1 + len(waveLines) + 1 // hint + wave + trailing blank

	if targetHeight := s.detailHeight - 2; targetHeight > 0 {
		for len(lines)+footerLen+len(linksLines) < targetHeight {
			lines = append(lines, blank)
		}
	}
	lines = append(lines, hint)
	lines = append(lines, waveLines...)
	lines = append(lines, blank)

	linksStart := len(lines)
	lines = append(lines, linksLines...)

	return settingsSectionBody{
		lines: lines,
		anchors: map[settingsField]int{
			sfAboutSupport: linksStart,
			sfAboutRepo:    linksStart,
			sfAboutIssues:  linksStart,
		},
	}
}
