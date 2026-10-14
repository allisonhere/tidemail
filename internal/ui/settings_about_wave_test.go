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

func TestAboutWaveToggleKeyFlipsScreen(t *testing.T) {
	s := aboutWaveTestSettings(t)
	if s.aboutUseWaveScreen {
		t.Fatalf("expected the wave screen to start off")
	}

	next, cmd, done := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}}, DefaultKeys)
	if done {
		t.Fatalf("expected the toggle key to stay in Settings, not exit")
	}
	if !next.aboutUseWaveScreen {
		t.Fatalf("expected 'w' to turn the wave screen on")
	}
	if cmd == nil {
		t.Fatalf("expected a command to start the wave animation")
	}

	next, _, _ = next.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}}, DefaultKeys)
	if next.aboutUseWaveScreen {
		t.Fatalf("expected a second 'w' to flip the wave screen back off")
	}
}

func TestAboutWaveTickAdvancesFrameAndRearms(t *testing.T) {
	s := aboutWaveTestSettings(t)
	s.aboutUseWaveScreen = true

	next, cmd, _ := s.Update(settingsAboutWaveMsg{}, DefaultKeys)
	if next.aboutWaveFrame != 1 {
		t.Fatalf("expected aboutWaveFrame to advance to 1, got %d", next.aboutWaveFrame)
	}
	if cmd == nil {
		t.Fatalf("expected the tick to re-arm itself")
	}
}

func TestAboutWaveTickStopsWhenScreenIsOff(t *testing.T) {
	s := aboutWaveTestSettings(t)
	s.aboutUseWaveScreen = false

	next, cmd, _ := s.Update(settingsAboutWaveMsg{}, DefaultKeys)
	if next.aboutWaveFrame != 0 {
		t.Fatalf("expected no frame advance while the wave screen is off, got %d", next.aboutWaveFrame)
	}
	if cmd != nil {
		t.Fatalf("expected no re-arm while the wave screen is off")
	}
}

func TestAboutWaveTickStopsWhenSectionChanges(t *testing.T) {
	s := aboutWaveTestSettings(t)
	s.aboutUseWaveScreen = true
	s.setActiveSection(ssSupport)

	next, cmd, _ := s.Update(settingsAboutWaveMsg{}, DefaultKeys)
	if cmd != nil {
		t.Fatalf("expected no re-arm once the active section changed away from About")
	}
	_ = next
}

func TestAboutWaveTickSettlesAfterIdleThreshold(t *testing.T) {
	s := aboutWaveTestSettings(t)
	s.aboutUseWaveScreen = true
	s.aboutWaveFrame = settingsAboutWaveSettleFrame - 1

	next, cmd, _ := s.Update(settingsAboutWaveMsg{}, DefaultKeys)
	if cmd != nil {
		t.Fatalf("expected the tick loop to settle (stop re-arming) once past the idle threshold")
	}
	if next.aboutWaveFrame < settingsAboutWaveSettleFrame {
		t.Fatalf("expected aboutWaveFrame to reach the settle threshold, got %d", next.aboutWaveFrame)
	}
}

func TestAboutSectionFieldsOmitHeartOnWaveScreen(t *testing.T) {
	s := aboutWaveTestSettings(t)

	withHeart := s.sectionFields(ssAbout)
	foundHeart := false
	for _, f := range withHeart {
		if f == sfAboutHeart {
			foundHeart = true
		}
	}
	if !foundHeart {
		t.Fatalf("expected sfAboutHeart in the default About field list")
	}

	s.aboutUseWaveScreen = true
	withoutHeart := s.sectionFields(ssAbout)
	for _, f := range withoutHeart {
		if f == sfAboutHeart {
			t.Fatalf("expected sfAboutHeart to be omitted when the wave screen is active")
		}
	}
}

func TestRenderAboutWaveSectionKeepsLinkAnchors(t *testing.T) {
	s := aboutWaveTestSettings(t)
	s.aboutUseWaveScreen = true
	s.detailHeight = 30

	for _, width := range []int{40, 80, 120} {
		body := s.renderAboutWaveSection(width, newManagerChrome(width, CatppuccinMocha, false))
		for _, field := range []settingsField{sfAboutSupport, sfAboutRepo, sfAboutIssues} {
			if _, ok := body.anchors[field]; !ok {
				t.Fatalf("width %d: expected an anchor for field %v", width, field)
			}
		}
		if len(body.lines) == 0 {
			t.Fatalf("width %d: expected rendered lines", width)
		}
	}
}
