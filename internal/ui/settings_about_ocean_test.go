package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tidemail/internal/config"
)

func aboutOceanTestSettings(t *testing.T) Settings {
	t.Helper()
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setActiveSection(ssAbout)
	s.setFocusedPane(settingsPaneDetail)
	return s
}

func oceanText(body settingsSectionBody) string {
	return ansi.Strip(strings.Join(body.lines, "\n"))
}

func TestAboutWaveTickAdvancesFrameAndRearms(t *testing.T) {
	s := aboutOceanTestSettings(t)

	next, cmd, _ := s.Update(settingsAboutWaveMsg{}, DefaultKeys)
	if next.aboutWaveFrame != 1 {
		t.Fatalf("expected aboutWaveFrame to advance to 1, got %d", next.aboutWaveFrame)
	}
	if cmd == nil {
		t.Fatalf("expected the tick to re-arm itself")
	}
}

func TestAboutWaveTickStopsWhenSectionChanges(t *testing.T) {
	s := aboutOceanTestSettings(t)
	s.aboutWaveFrame = 50
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
	s := aboutOceanTestSettings(t)
	s.aboutWaveFrame = settingsAboutWaveSettleFrame - 1

	next, cmd, _ := s.Update(settingsAboutWaveMsg{}, DefaultKeys)
	if cmd != nil {
		t.Fatalf("expected the tick loop to settle (stop re-arming) once past the idle threshold")
	}
	if next.aboutWaveFrame < settingsAboutWaveSettleFrame {
		t.Fatalf("expected aboutWaveFrame to reach the settle threshold, got %d", next.aboutWaveFrame)
	}
}

// A settled scene must replay its intro the next time About opens, rather
// than staying frozen at the settle frame.
func TestLeavingAboutResetsTheIntro(t *testing.T) {
	s := aboutOceanTestSettings(t)
	s.aboutWaveFrame = settingsAboutWaveSettleFrame
	s.setActiveSection(ssSupport)
	s.setActiveSection(ssAbout)

	if s.aboutWaveFrame != 0 {
		t.Fatalf("expected aboutWaveFrame reset after leaving About, got %d", s.aboutWaveFrame)
	}
	if cmd := s.decorativePulseCmd(); cmd == nil {
		t.Fatalf("expected re-entering About to start the animation again")
	}
}

func TestDecorativePulseCmdRunsBothAboutAnimations(t *testing.T) {
	s := aboutOceanTestSettings(t)

	msg := s.decorativePulseCmd()()
	batch, ok := msg.(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("expected the reveal pulse and the scene tick armed together, got %T %v", msg, msg)
	}
}

func TestAboutOceanFillsPaneAtEveryFrame(t *testing.T) {
	chrome := newManagerChrome(100, CatppuccinMocha, false)
	for _, size := range [][2]int{{40, 18}, {60, 22}, {80, 28}, {120, 40}} {
		for frame := 0; frame <= settingsAboutWaveSettleFrame; frame += 9 {
			s := aboutOceanTestSettings(t)
			s.detailHeight = size[1]
			s.aboutWaveFrame = frame
			body := s.renderAboutOceanSection(size[0], chrome)

			if len(body.lines) < size[1]-2 {
				t.Fatalf("size %v frame %d: expected at least %d lines, got %d", size, frame, size[1]-2, len(body.lines))
			}
			for i, line := range body.lines {
				if got := lipgloss.Width(line); got != size[0] {
					t.Fatalf("size %v frame %d line %d: width %d, want %d", size, frame, i, got, size[0])
				}
			}
			for _, f := range []settingsField{sfAboutHeart, sfAboutSupport, sfAboutRepo, sfAboutIssues} {
				if line, ok := body.anchors[f]; !ok || line < 0 || line >= len(body.lines) {
					t.Fatalf("size %v frame %d: anchor for %v missing or out of range (%d)", size, frame, f, line)
				}
			}
		}
	}
}

func TestAboutOceanShowsContentAfterIntro(t *testing.T) {
	s := aboutOceanTestSettings(t)
	s.detailHeight = 30
	s.aboutWaveFrame = 120
	body := s.renderAboutOceanSection(90, newManagerChrome(90, CatppuccinMocha, false))
	text := oceanText(body)

	for _, want := range []string{"your mail, your rules", "terminal information delivery engine", "Thanks for taking a look -allie", "❤", "Support Tidemail", "Repository", "Issues"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in the About scene, got:\n%s", want, text)
		}
	}
	if !strings.Contains(body.lines[body.anchors[sfAboutSupport]+1], "Support Tidemail") {
		t.Fatalf("expected the link anchor to point at the button row")
	}
}

func TestAboutOceanHeartFocusShowsHalo(t *testing.T) {
	s := aboutOceanTestSettings(t)
	s.detailHeight = 30
	chrome := newManagerChrome(90, CatppuccinMocha, false)

	if text := oceanText(s.renderAboutOceanSection(90, chrome)); strings.Contains(text, "✦ ❤ ✦") {
		t.Fatalf("expected no halo while the heart isn't focused")
	}
	s.setFocusedField(sfAboutHeart)
	body := s.renderAboutOceanSection(90, chrome)
	if !strings.Contains(ansi.Strip(body.lines[body.anchors[sfAboutHeart]]), "✦ ❤ ✦") {
		t.Fatalf("expected the halo on the heart's anchor line when focused")
	}
}

func TestAboutOceanRevealSpellsLoveIsLove(t *testing.T) {
	s := aboutOceanTestSettings(t)
	s.detailHeight = 30
	s.aboutRevealActive = true
	s.aboutRevealFrame = settingsAboutRevealStart
	text := oceanText(s.renderAboutOceanSection(90, newManagerChrome(90, CatppuccinMocha, false)))
	if !strings.Contains(text, "LOVE IS LOVE") {
		t.Fatalf("expected LOVE IS LOVE during the heart reveal, got:\n%s", text)
	}
}

func TestAboutOceanFocusedButtonIsFilled(t *testing.T) {
	s := aboutOceanTestSettings(t)
	s.detailHeight = 30
	s.setFocusedField(sfAboutRepo)
	body := s.renderAboutOceanSection(90, newManagerChrome(90, CatppuccinMocha, false))
	l, _ := oceanLayoutFor(90, 28)

	// Re-render the button row on its own canvas to inspect cell styles.
	c := newSceneCanvas(90, 3)
	oceanButtons(c, 0, sfAboutRepo, 1)
	filled := 0
	for x := range c.w {
		if p := c.at(x, 1); p != nil && p.bg == oceanAccent {
			filled++
		}
	}
	if filled == 0 {
		t.Fatalf("expected the focused button's interior to be filled with the accent color")
	}
	if !strings.Contains(ansi.Strip(body.lines[l.linksY+1]), "Repository") {
		t.Fatalf("expected the Repository label on the button row")
	}
}

func TestAboutPlainThemeKeepsOriginalScreen(t *testing.T) {
	s := aboutOceanTestSettings(t)
	s.detailHeight = 30
	body := s.viewSectionBody(90, newManagerChrome(90, CatppuccinMocha, true))
	text := oceanText(body)
	if strings.Contains(text, "▁") || strings.Contains(text, "▃") {
		t.Fatalf("expected the plain theme to skip the ocean scene, got:\n%s", text)
	}
	if !strings.Contains(text, "terminal information delivery engine") {
		t.Fatalf("expected the original About content on the plain theme, got:\n%s", text)
	}
}
