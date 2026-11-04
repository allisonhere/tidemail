package ui

import "github.com/charmbracelet/lipgloss"

// TideMail's fixed alert palette: green for OK, yellow for caution and
// warnings, red for failures. Each has a bright value for dark themes and a
// deep value for light themes, so the meaning of a color stays the same no
// matter which theme is active.
const (
	alertRedDark     = lipgloss.Color("#DD6A6A")
	alertRedLight    = lipgloss.Color("#8C1A22")
	alertYellowDark  = lipgloss.Color("#FCD05F")
	alertYellowLight = lipgloss.Color("#AA7B18")
	alertGreenDark   = lipgloss.Color("#70C68E")
	alertGreenLight  = lipgloss.Color("#1A5E33")
)

// alertPill is a status bar pill's fill and the text color drawn on it.
type alertPill struct{ fill, text lipgloss.Color }

// The status bar pills use each pair exactly, on every theme: the bright color
// as the fill and the deep one as the text.
var (
	alertPillRed    = alertPill{fill: alertRedDark, text: alertRedLight}
	alertPillYellow = alertPill{fill: alertYellowDark, text: alertYellowLight}
	alertPillGreen  = alertPill{fill: alertGreenDark, text: alertGreenLight}
)

// withAlertColors returns t with its Error, Warning and Success roles replaced
// by TideMail's fixed alert palette. These roles color bare text on the theme
// background (error lines, badges, form messages), so dark themes get the
// bright value and light themes the deep one; the pills use both as a pair. The VT52 and VT100 retro themes are single-hue by
// design and keep their own colors.
func withAlertColors(t Theme) Theme {
	if t.Name == ThemeNameVT52 || t.Name == ThemeNameVT100 {
		return t
	}
	if isDark(t.Bg) {
		t.Error, t.Warning, t.Success = alertRedDark, alertYellowDark, alertGreenDark
	} else {
		t.Error, t.Warning, t.Success = alertRedLight, alertYellowLight, alertGreenLight
	}
	return t
}

// isAlertGreen reports whether c is one of the fixed alert greens, which are
// already green and must not be hue-shifted.
func isAlertGreen(c lipgloss.Color) bool {
	return c == alertGreenDark || c == alertGreenLight
}
