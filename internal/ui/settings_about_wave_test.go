package ui

import (
	"math"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/allisonhere/tidemail/internal/config"
)

// rgbDistance is a simple Euclidean distance in sRGB space, enough to order
// "closer to bg" vs "closer to base" without needing full perceptual color
// math (that's what contrastRatio is for elsewhere in this package).
func rgbDistance(t *testing.T, a, b lipgloss.Color) float64 {
	t.Helper()
	ar, ag, ab, ok1 := hexToRGB(a)
	br, bg, bb, ok2 := hexToRGB(b)
	if !ok1 || !ok2 {
		t.Fatalf("could not parse colors %q, %q", a, b)
	}
	dr, dg, db := ar-br, ag-bg, ab-bb
	return math.Sqrt(dr*dr + dg*dg + db*db)
}

func aboutWaveTestSettings(t *testing.T) Settings {
	t.Helper()
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setActiveSection(ssAbout)
	s.setFocusedPane(settingsPaneDetail)
	return s
}

func TestAboutWaveTickAdvancesFrameAndRearms(t *testing.T) {
	s := aboutWaveTestSettings(t)

	next, cmd, _ := s.Update(settingsAboutWaveMsg{}, DefaultKeys)
	if next.aboutWaveFrame != 1 {
		t.Fatalf("expected aboutWaveFrame to advance to 1, got %d", next.aboutWaveFrame)
	}
	if cmd == nil {
		t.Fatalf("expected the tick to re-arm itself")
	}
}

func TestAboutWaveTickStopsWhenSectionChanges(t *testing.T) {
	s := aboutWaveTestSettings(t)
	s.setActiveSection(ssSupport)

	next, cmd, _ := s.Update(settingsAboutWaveMsg{}, DefaultKeys)
	if cmd != nil {
		t.Fatalf("expected no re-arm once the active section changed away from About")
	}
	if next.aboutWaveFrame != 0 {
		t.Fatalf("expected aboutWaveFrame to reset to 0 once off About, got %d", next.aboutWaveFrame)
	}
}

func TestAboutWaveTickSettlesAfterIdleThreshold(t *testing.T) {
	s := aboutWaveTestSettings(t)
	s.aboutWaveFrame = settingsAboutWaveSettleFrame - 1

	next, cmd, _ := s.Update(settingsAboutWaveMsg{}, DefaultKeys)
	if cmd != nil {
		t.Fatalf("expected the tick loop to settle (stop re-arming) once past the idle threshold")
	}
	if next.aboutWaveFrame < settingsAboutWaveSettleFrame {
		t.Fatalf("expected aboutWaveFrame to reach the settle threshold, got %d", next.aboutWaveFrame)
	}
}

func TestDecorativePulseCmdRunsBothAboutAnimations(t *testing.T) {
	s := aboutWaveTestSettings(t)

	cmd := s.decorativePulseCmd()
	if cmd == nil {
		t.Fatalf("expected a batched command for the About section")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("expected a tea.BatchMsg combining the gradient and wave pulses, got %T", msg)
	}
	if len(batch) != 2 {
		t.Fatalf("expected both the gradient pulse and the wave pulse armed together, got %d commands", len(batch))
	}
}

func TestAboutWaveShadeDarkensWithDepth(t *testing.T) {
	base := lipgloss.Color("#4080c0")
	bg := lipgloss.Color("#000000")

	if got := aboutWaveShade(base, bg, 0); got != base {
		t.Fatalf("expected depth 0 to leave the base color unchanged, got %q", got)
	}

	shallow := aboutWaveShade(base, bg, 0.3)
	deep := aboutWaveShade(base, bg, 1)

	distShallow := rgbDistance(t, shallow, bg)
	distDeep := rgbDistance(t, deep, bg)
	if !(distDeep < distShallow) {
		t.Fatalf("expected deeper shading to be closer to bg: shallow=%q (dist %.1f) deep=%q (dist %.1f)",
			shallow, distShallow, deep, distDeep)
	}
}

func TestRenderAboutCombinedSectionKeepsAllAnchors(t *testing.T) {
	s := aboutWaveTestSettings(t)
	s.detailHeight = 30

	for _, width := range []int{40, 80, 120} {
		body := s.renderAboutCombinedSection(width, newManagerChrome(width, CatppuccinMocha, false))
		for _, field := range []settingsField{sfAboutHeart, sfAboutSupport, sfAboutRepo, sfAboutIssues} {
			if _, ok := body.anchors[field]; !ok {
				t.Fatalf("width %d: expected an anchor for field %v", width, field)
			}
		}
		if len(body.lines) == 0 {
			t.Fatalf("width %d: expected rendered lines", width)
		}
	}
}
