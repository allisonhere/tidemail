package ui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/allisonhere/tidemail/internal/omarchy"
)

// ThemeNameMatchOmarchy is the pseudo-theme that follows the current Omarchy
// desktop theme, contrast-corrected. It is not a member of BuiltinThemes — its
// colors are resolved at runtime — but it is offered by PickableThemes().
const ThemeNameMatchOmarchy = "match-omarchy"

// omarchyIndex is the virtual picker index for "match-omarchy": it sits one
// past the real built-in themes.
var omarchyIndex = len(BuiltinThemes)

// omarchyPlaceholderTheme is shown in the theme picker row and used as the
// preview/fallback base before (or when) the live Omarchy palette resolves. It
// deliberately reuses a known-good built-in palette.
var omarchyPlaceholderTheme = func() Theme {
	t := CatppuccinMocha
	t.Name = ThemeNameMatchOmarchy
	return t
}()

// isMatchOmarchy reports whether name selects the Omarchy-following theme.
func isMatchOmarchy(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), ThemeNameMatchOmarchy)
}

// omarchyTheme maps a raw Omarchy palette onto TideMail's Theme struct. The
// result still needs contrastCorrectTheme before use.
func omarchyTheme(p omarchy.Palette) Theme {
	c := func(s string) lipgloss.Color { return lipgloss.Color(s) }

	bg := c(p.Background)
	fg := c(p.Foreground)

	border := c(p.Muted)
	if p.Muted == "" {
		border = mixColors(fg, bg, 0.5)
	}
	accent := c(p.Accent)
	if p.Accent == "" {
		accent = fg
	}
	statusBg := c(p.StatusBg)
	if p.StatusBg == "" {
		if isDark(bg) {
			statusBg = adjustLightness(bg, 0.05)
		} else {
			statusBg = adjustLightness(bg, -0.05)
		}
	}
	unread := c(p.Ok)
	if p.Ok == "" {
		unread = accent
	}
	errColor := c(p.Error)
	if p.Error == "" {
		errColor = accent
	}

	return Theme{
		Name:          ThemeNameMatchOmarchy,
		Bg:            bg,
		Fg:            fg,
		Border:        border,
		BorderFocus:   accent,
		Selected:      accent,
		Unread:        unread,
		Dimmed:        border,
		StatusBar:     statusBg,
		StatusFg:      fg,
		Error:         errColor,
		Overlay:       statusBg,
		OverlayBorder: accent,
	}
}

// contrastCorrectTheme nudges each directly-consumed Theme field until it
// clears TideMail's readability floors against the theme background, so an
// arbitrary desktop palette can't produce an unreadable UI. It is
// light/dark-agnostic: the helpers branch on isDark(t.Bg) internally.
func contrastCorrectTheme(t Theme) Theme {
	out := t

	out.Fg = readableText(t.Fg, t.Bg, 4.5)
	out.Dimmed = mutedText(out.Fg, t.Bg)
	out.BorderFocus = accentReadableOn(t.BorderFocus, t.Bg, paneFocusMinContrast)
	out.Selected = accentReadableOn(t.Selected, t.Bg, 4.5)
	out.OverlayBorder = accentReadableOn(t.OverlayBorder, t.Bg, 4.5)
	out.Border = accentReadableOn(t.Border, t.Bg, 3.0)
	out.Unread = accentReadableOn(t.Unread, t.Bg, 3.0)
	out.Error = accentReadableOn(t.Error, t.Bg, 4.5)

	if contrastRatio(out.StatusBar, t.Bg) < 1.2 {
		out.StatusBar = selectionBgForRatio(t.Bg, 2.0)
		out.Overlay = out.StatusBar
	}
	out.StatusFg = readableText(t.StatusFg, out.StatusBar, 4.5)

	return out
}

// resolveOmarchyTheme reads the live Omarchy palette and returns a
// contrast-corrected Theme. ok is false when Omarchy isn't available.
func resolveOmarchyTheme() (Theme, bool) {
	p, ok := omarchy.CurrentPalette()
	if !ok {
		return Theme{}, false
	}
	return contrastCorrectTheme(omarchyTheme(p)), true
}

// currentOmarchyThemeName returns the active Omarchy theme slug for display in
// the settings hint row, or "" when Omarchy isn't available.
func currentOmarchyThemeName() string {
	if p, ok := omarchy.CurrentPalette(); ok {
		return p.Name
	}
	return ""
}

// omarchyThemeTickMsg drives the live-follow poll while the active theme is
// "match-omarchy".
type omarchyThemeTickMsg struct{}

const omarchyWatchInterval = 2 * time.Second

// omarchyWatchCmd schedules the next live-follow poll.
func omarchyWatchCmd() tea.Cmd {
	return tea.Tick(omarchyWatchInterval, func(time.Time) tea.Msg { return omarchyThemeTickMsg{} })
}

// omarchySignature is the cheap change-detection token for the active Omarchy
// theme (empty when unavailable).
func omarchySignature() string { return omarchy.CurrentSignature() }

// startOmarchyWatchIfNeeded records the current Omarchy signature and returns
// the live-follow poll command when the active theme is "match-omarchy" and no
// poll loop is already running; otherwise it returns nil.
func (m *Model) startOmarchyWatchIfNeeded() tea.Cmd {
	if !isMatchOmarchy(m.cfg.Theme) {
		return nil
	}
	m.omarchySig = omarchySignature()
	if m.omarchyWatching {
		return nil
	}
	m.omarchyWatching = true
	return omarchyWatchCmd()
}

// handleOmarchyThemeTick is the live-follow poll. While the active theme is
// "match-omarchy" it re-resolves the Omarchy palette whenever the desktop theme
// changed and re-arms itself; when the theme is no longer "match-omarchy" it
// stops (returns no command).
func (m Model) handleOmarchyThemeTick() (tea.Model, tea.Cmd) {
	if !isMatchOmarchy(m.cfg.Theme) {
		m.omarchyWatching = false
		return m, nil
	}
	m.omarchyWatching = true
	sig := omarchySignature()
	if sig == m.omarchySig {
		return m, omarchyWatchCmd()
	}
	m.omarchySig = sig
	merged, _ := MergedThemeFromConfig(m.cfg)
	m.styles = BuildStyles(merged, m.cfg.Display.Density, m.cfg.Display.PaneCorners)
	if m.activeMessageRowCount() > 0 {
		m.setViewportForCurrentRow()
	}
	return m, tea.Batch(omarchyWatchCmd(), setTermColorsCmd(merged.Fg, merged.Bg))
}
