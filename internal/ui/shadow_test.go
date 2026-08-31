package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// TestOverlayOnBaseShadowChangesRender guards the wiring: passing shadow=true
// must actually blend a drop shadow into the base behind the box, not render
// identically regardless of the flag.
func TestOverlayOnBaseShadowChangesRender(t *testing.T) {
	const w, h = 40, 12
	bg := CatppuccinMocha.Bg
	base := strings.TrimRight(strings.Repeat(strings.Repeat("x", w)+"\n", h), "\n")
	box := lipgloss.NewStyle().
		Background(lipgloss.Color("#444444")).
		Width(16).Height(4).
		Render("hi")

	without := overlayOnBase(base, box, w, h, bg, false)
	with := overlayOnBase(base, box, w, h, bg, true)

	if with == without {
		t.Fatal("overlayOnBase produced identical output with and without the shadow")
	}
	// Dimensions must be preserved either way.
	for _, out := range []string{without, with} {
		if got := strings.Count(out, "\n") + 1; got != h {
			t.Fatalf("line count = %d, want %d", got, h)
		}
		for _, line := range strings.Split(out, "\n") {
			if got := lipgloss.Width(line); got != w {
				t.Fatalf("line width = %d, want %d (%q)", got, w, ansi.Strip(line))
			}
		}
	}
	// The box content itself still lands intact.
	if !strings.Contains(ansi.Strip(with), "hi") {
		t.Fatal("shadowed overlay lost the box content")
	}
}

// TestOverlayOnBaseShadowWideRunesKeepWidth is the regression guard for a
// shadowed overlay corrupting one row: a wide rune in the base straddling the
// column where the compositor slices the row (post-shadow, ansi.Cut counts
// visual columns) could drop a cell or two, dropping the box's right border and
// the shadow sliver on that row. Every row must stay exactly w wide, for a wide
// rune landing at every possible boundary.
func TestOverlayOnBaseShadowWideRunesKeepWidth(t *testing.T) {
	const w, h = 60, 24
	bg := CatppuccinMocha.Bg
	var b strings.Builder
	for i := 0; i < h; i++ {
		if i%2 == 0 {
			b.WriteString(strings.Repeat("-", w))
		} else {
			// shift the run of wide runes by row so a glyph straddles a
			// different column each time
			b.WriteString(clampView(strings.Repeat("x", i)+strings.Repeat("世", w), w, 1, bg))
		}
		if i < h-1 {
			b.WriteByte('\n')
		}
	}
	box := lipgloss.NewStyle().Background(lipgloss.Color("#444444")).Width(30).Height(10).Render("box")

	out := overlayOnBase(b.String(), box, w, h, bg, true)
	lines := strings.Split(out, "\n")
	if len(lines) != h {
		t.Fatalf("line count = %d, want %d", len(lines), h)
	}
	for i, line := range lines {
		if got := lipgloss.Width(line); got != w {
			t.Fatalf("row %d width = %d, want %d (%q)", i, got, w, ansi.Strip(line))
		}
	}
}

// TestBlendShadowRectNoIntersection returns the (clamped) base untouched when
// the shadow rectangle falls entirely outside the viewport.
func TestBlendShadowRectNoIntersection(t *testing.T) {
	const w, h = 20, 6
	bg := CatppuccinMocha.Bg
	base := strings.TrimRight(strings.Repeat(strings.Repeat("y", w)+"\n", h), "\n")

	got := blendShadowRect(base, 0, h+5, 8, 3, w, h, bg, shadowColor(bg))
	want := clampView(base, w, h, bg)
	if got != want {
		t.Fatal("blendShadowRect altered base for an off-screen shadow rectangle")
	}
}
