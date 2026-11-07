package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestTerminalDefaultBgIsNudgedUnlessTransparent(t *testing.T) {
	cases := map[lipgloss.Color]lipgloss.Color{
		"#000000": "#000001",
		"#0a0f1a": "#0a0f1b",
		"#ffffff": "#fffffe",
		"#1234ff": "#1234fe",
	}
	for in, want := range cases {
		if got := terminalDefaultBg(in, false); got != want {
			t.Errorf("terminalDefaultBg(%s) = %s, want %s", in, got, want)
		}
		if got := terminalDefaultBg(in, true); got != in {
			t.Errorf("transparent terminalDefaultBg(%s) = %s, want it unchanged", in, got)
		}
	}
	if got := terminalDefaultBg("", false); got != "" {
		t.Errorf("empty background became %q", got)
	}
}

// kitty makes every cell whose color equals the default background
// see-through, so the solid default must never set the exact theme color.
func TestTerminalColorSequencesKeepSolidThemesOpaque(t *testing.T) {
	for _, theme := range BuiltinThemes {
		solid, _ := TerminalColorSequences(theme.Name, false)
		if strings.Contains(solid, "\x1b]11;"+string(theme.Bg)+"\x07") {
			t.Errorf("%s: solid mode sets the default background to the exact theme color", theme.Name)
		}
		clear, _ := TerminalColorSequences(theme.Name, true)
		if !strings.Contains(clear, "\x1b]11;"+string(theme.Bg)+"\x07") {
			t.Errorf("%s: transparent mode must set the exact theme color", theme.Name)
		}
	}
}

func TestTransparentBackgroundDropsOnlyTheThemeBackground(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })

	bg, other := lipgloss.Color("#000000"), lipgloss.Color("#123456")
	view := lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("#ffffff")).Render("a") +
		lipgloss.NewStyle().Background(bg).Render("b") +
		lipgloss.NewStyle().Background(other).Render("c")
	got := transparentBackground(view, bg)
	if strings.Contains(got, themeBgParams(bg)) {
		t.Fatalf("theme background left in %q", got)
	}
	if !strings.Contains(got, themeBgParams(other)) {
		t.Fatalf("other surfaces must keep their background: %q", got)
	}
	if !strings.Contains(got, "49") {
		t.Fatalf("expected default-background resets in %q", got)
	}
}

func TestTransparentSettingRendersWithoutThemeBackground(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })

	f := newUndoTestFixture(t, 3)
	m := f.model
	m.width, m.height = 100, 30
	m.rebuildSidebar()
	params := themeBgParams(m.styles.Theme.Bg)
	if !strings.Contains(m.View(), params) {
		t.Fatal("solid mode must paint the theme background")
	}
	m.cfg.Display.Transparent = true
	if strings.Contains(m.View(), params) {
		t.Fatal("transparent mode left cells painted in the theme background")
	}
}

func TestTransparentSettingRoundTrips(t *testing.T) {
	f := newUndoTestFixture(t, 0)
	cfg := f.model.cfg
	cfg.Display.Transparent = true
	s := newSettings(cfg, settingsUpdateState{})
	if !s.transparent || !s.ApplyTo(cfg).Display.Transparent {
		t.Fatal("transparent setting lost between config and settings")
	}
	s.transparent = false
	if s.ApplyTo(cfg).Display.Transparent {
		t.Fatal("turning transparency off did not save")
	}
}
