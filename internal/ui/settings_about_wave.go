package ui

import (
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// A tideftp-inspired animated wave strip for Settings -> About, framing the
// existing DNA-helix/heart content (renderAboutSection, elsewhere in this
// package, left completely unchanged) with one copy of the wave above it and
// another below. Kept in its own file so it's easy to review or remove
// independently.
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
// if so which block glyph represents the fractional fill at that row, plus a
// normalized depth (0 right at the surface edge, up to 1 at the deepest
// possible cell) for shading — distinct from the sub-cell fill fraction,
// which only varies within the one partially-filled row per layer and is
// otherwise pinned at 1 for every fully submerged cell below it.
func aboutWaveGlyphForRow(height float64, row int) (rune, float64, bool) {
	rowDepth := float64(aboutWaveRows - row) // bottom row -> depth 1
	fill := height - (rowDepth - 1)
	if fill <= 0 {
		return ' ', 0, false
	}
	depth := fill / float64(aboutWaveRows)
	if depth > 1 {
		depth = 1
	}
	if fill >= 1 {
		return aboutWaveBlocks[len(aboutWaveBlocks)-1], depth, true
	}
	idx := int(fill * float64(len(aboutWaveBlocks)-1))
	return aboutWaveBlocks[idx], depth, true
}

// aboutWaveShade blends base darker toward bg as depth increases (0 = base
// unchanged, 1 = halfway to bg), so deeper water reads darker without
// losing the layer's hue or going fully black.
func aboutWaveShade(base, bg lipgloss.Color, depth float64) lipgloss.Color {
	return mixColors(base, bg, depth*0.5)
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
			if g, depth, ok := aboutWaveGlyphForRow(aboutWaveHeight(col, frame, layer), row); ok {
				glyph, color = g, aboutWaveShade(colors[layer], bg, depth)
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

// renderAboutCombinedSection frames the real About content (rendered by
// renderAboutSection, completely unchanged) with the wave strip once above
// and once below: waves, DNA/heart/links as-is, waves. Field anchors from
// the wrapped body are preserved, shifted down by the header's line count,
// so Tab navigation and Enter/Space activation on the heart and the
// Support/Repository/Issues buttons keep working unchanged.
func (s Settings) renderAboutCombinedSection(width int, chrome managerChrome) settingsSectionBody {
	bodyW := max(1, width)
	aboutBg := lipgloss.Color("#000000")
	blank := lipgloss.NewStyle().Background(aboutBg).Width(bodyW).Render("")
	wave := s.renderAboutWaveStrip(bodyW, chrome, aboutBg)
	waveLines := strings.Split(wave, "\n")
	headerLen := len(waveLines) + 1 // wave + trailing blank
	footerLen := 1 + len(waveLines) // leading blank + wave

	// renderAboutSection pads its own content to fill the whole detail pane,
	// assuming it owns the full height. Shrink what it sees by the extra
	// rows this wrapper adds above and below, so the combined total still
	// fits the real pane instead of overflowing past the visible window.
	inner := s
	if inner.detailHeight > 0 {
		inner.detailHeight = max(1, inner.detailHeight-headerLen-footerLen)
	}
	body := inner.renderAboutSection(width, chrome)

	lines := make([]string, 0, headerLen+len(body.lines)+footerLen)
	lines = append(lines, waveLines...)
	lines = append(lines, blank)
	lines = append(lines, body.lines...)
	lines = append(lines, blank)
	lines = append(lines, waveLines...)

	anchors := make(map[settingsField]int, len(body.anchors))
	for field, line := range body.anchors {
		anchors[field] = line + headerLen
	}

	return settingsSectionBody{lines: lines, anchors: anchors}
}
