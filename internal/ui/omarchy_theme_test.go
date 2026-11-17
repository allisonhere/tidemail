package ui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestOmarchyThemeLooksLikeOmarchy(t *testing.T) {
	theme, _ := ThemeByName(ThemeNameOmarchy)
	if theme.Name != ThemeNameOmarchy {
		t.Fatalf("theme not found: %q", theme.Name)
	}
	// Round corners requested, shadow on, round tag ends: the theme overrides all three.
	if s := BuildStyles(theme, "compact", "round"); s.RoundedCorners {
		t.Fatal("omarchy theme must keep sharp corners")
	}
	if s := BuildStyles(CatppuccinMocha, "compact", "round"); !s.RoundedCorners {
		t.Fatal("other themes must still honour pane_corners")
	}
}

func isGray(c lipgloss.Color) bool {
	r, g, b, ok := hexToRGB(c)
	return ok && r == g && g == b
}

func TestOmarchyThemeHasNoColor(t *testing.T) {
	theme, _ := ThemeByName(ThemeNameOmarchy)
	roles := map[string]lipgloss.Color{
		"Bg": theme.Bg, "Fg": theme.Fg, "Border": theme.Border, "BorderFocus": theme.BorderFocus,
		"Selected": theme.Selected, "Unread": theme.Unread, "Dimmed": theme.Dimmed,
		"StatusBar": theme.StatusBar, "StatusFg": theme.StatusFg, "Error": theme.Error,
		"Overlay": theme.Overlay, "OverlayBorder": theme.OverlayBorder, "Success": theme.Success,
		"Warning": theme.Warning, "Link": theme.Link, "Quote": theme.Quote, "Match": theme.Match,
		"Tag": theme.Tag, "Cursor": theme.Cursor,
	}
	for name, c := range roles {
		if !isGray(c) {
			t.Errorf("%s = %s is not a shade of gray", name, c)
		}
	}
	m := Model{styles: BuildStyles(theme, "compact", "round")}
	for _, p := range []alertPill{alertPillRed, alertPillYellow, alertPillGreen} {
		got := m.pillColors(p)
		if !isGray(got.fill) || !isGray(got.text) {
			t.Errorf("pill %+v is not gray: %+v", p, got)
		}
	}
	for _, key := range tagColorKeys() {
		c := themeTagColor(theme, key)
		if !isGray(lipgloss.Color(c.Bg)) || !isGray(lipgloss.Color(c.Fg)) {
			t.Errorf("tag %s is not gray: %+v", key, c)
		}
	}
}

func TestOmarchyThemeIgnoresAccountColors(t *testing.T) {
	f := newUndoTestFixture(t, 1)
	m := f.model
	m.accounts[0].Color = "#ff0000"
	if got := m.accountColor(m.accounts[0].ID); got == "" {
		t.Fatal("other themes must still use account colors")
	}
	theme, _ := ThemeByName(ThemeNameOmarchy)
	m.styles = BuildStyles(theme, "compact", "round")
	if got := m.accountColor(m.accounts[0].ID); got != "" {
		t.Fatalf("omarchy theme must ignore account colors, got %q", got)
	}
}
