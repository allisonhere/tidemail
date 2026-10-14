package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/allisonhere/tidemail/internal/config"
)

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
