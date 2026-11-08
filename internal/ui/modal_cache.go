package ui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// renderCache is a single-entry, pointer-owned render stage. Keys describe
// display dependencies, never event types. Invalidate releases the artifact;
// Get builds lazily and records timings only when diagnostics are enabled.
// Like Model.View, it is owned exclusively by the Bubble Tea event loop.
type renderCache[K comparable, V any] struct {
	key   K
	value V
	valid bool
	stats *renderCacheStats
}
type renderCacheStats struct {
	Hits, Misses uint64
	BuildTime    time.Duration
}

func (c *renderCache[K, V]) Invalidate() {
	c.valid = false
	var key K
	var value V
	c.key, c.value = key, value
}
func (c *renderCache[K, V]) Get(key K, build func() V) V {
	if c.valid && c.key == key {
		if c.stats != nil {
			c.stats.Hits++
		}
		return c.value
	}
	var start time.Time
	if c.stats != nil {
		c.stats.Misses++
		start = time.Now()
	}
	c.value = build()
	c.key = key
	c.valid = true
	if c.stats != nil {
		c.stats.BuildTime += time.Since(start)
	}
	return c.value
}

// modalComposite caches the background's ANSI cuts and shadow for one geometry.
// It never owns foreground rows. Keeping these slices avoids full-screen ANSI
// parsing and shadow cell-buffer round trips on every selection/cursor update.
type modalComposite struct {
	prepared renderCache[modalCompositeKey, modalSlices]
	rowStats *renderCacheStats
}
type modalCompositeKey struct {
	base                      string
	x, y, w, h, width, height int
	bg                        lipgloss.Color
	shadow                    bool
	profile                   int
}
type modalSlices struct {
	rows, left, right []string
	foreground        []renderCache[string, string]
}

func (c *modalComposite) View(base, box string, width, height int, bg lipgloss.Color, shadow bool) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	lines := strings.Split(box, "\n")
	w := 0
	for _, line := range lines {
		w = max(w, lipgloss.Width(line))
	}
	h := len(lines)
	x, y := max(0, (width-w)/2), max(0, (height-h)/2)
	key := modalCompositeKey{base, x, y, w, h, width, height, bg, shadow, int(lipgloss.ColorProfile())}
	slices := c.prepared.Get(key, func() modalSlices {
		background := clampView(base, width, height, bg)
		if shadow {
			background = blendShadowRect(background, x+shadowOffsetX, y+shadowOffsetY, w, h, width, height, bg, shadowColor(bg))
		}
		rows := strings.Split(background, "\n")
		s := modalSlices{rows: strings.Split(clampView(background, width, height, bg), "\n"), left: make([]string, height), right: make([]string, height), foreground: make([]renderCache[string, string], height)}
		for row := y; row < min(height, y+h); row++ {
			s.left[row] = ansi.Cut(rows[row], 0, x)
			s.right[row] = ansi.Cut(rows[row], x+w, width)
		}
		return s
	})
	result := append([]string(nil), slices.rows...)
	for row := y; row < min(height, y+h); row++ {
		// Preserve overlayAt's wide-rune boundary repair, but only parse rows that
		// changed. Uncovered rows are already normalized by preparation.
		slices.foreground[row].stats = c.rowStats
		result[row] = slices.foreground[row].Get(lines[row-y], func() string {
			return clampView(slices.left[row]+lines[row-y]+slices.right[row], width, 1, bg)
		})
	}
	return strings.Join(result, "\n")
}

// modalFinish memoizes screen normalization and page styling by row. Lipgloss
// applies this borderless, unpadded style independently to each line, so fixed
// screen dimensions allow the final pass to reuse every unchanged row.
type modalFinish struct {
	rows  []renderCache[modalFinishKey, string]
	stats *renderCacheStats
}
type modalFinishKey struct {
	row     string
	width   int
	bg      lipgloss.Color
	profile int
}

func (c *modalFinish) View(view string, width, height int, bg lipgloss.Color) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	lines := strings.Split(view, "\n")
	if len(c.rows) != height {
		c.rows = make([]renderCache[modalFinishKey, string], height)
	}
	result := make([]string, height)
	for y := range result {
		line := ""
		if y < len(lines) {
			line = lines[y]
		}
		key := modalFinishKey{line, width, bg, int(lipgloss.ColorProfile())}
		c.rows[y].stats = c.stats
		result[y] = c.rows[y].Get(key, func() string {
			return lipgloss.NewStyle().Width(width).Height(1).Background(bg).Render(clampView(line, width, 1, bg))
		})
	}
	return strings.Join(result, "\n")
}
