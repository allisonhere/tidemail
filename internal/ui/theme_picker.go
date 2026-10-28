package ui

// The theme picker (T). The list, search, swatches and sample are tideui's
// ThemePicker; this file is the part that belongs to TideMail: translating
// TideMail's configurable key bindings into the keys the picker understands,
// previewing the highlighted theme across the whole app, and saving the one
// that is kept. Moving the cursor previews the theme; Enter keeps it and
// writes it to config, Esc puts the confirmed theme back.

import (
	"github.com/allisonhere/tideui"
	tea "github.com/charmbracelet/bubbletea"
)

// themePickerSize is the picker's width and the height it may use on a screen
// of the given size.
func themePickerSize(screenW, screenH int) (w, h int) {
	return min(screenW-4, 46), min(screenH-4, 26)
}

// newThemePicker builds the picker over every pickable theme, opened on name.
// match-omarchy takes its colors from the desktop at runtime, so the
// placeholder palette's swatch would be misleading and is left off.
func newThemePicker(name string) tideui.ThemePicker {
	picker := tideui.NewThemePicker(tideui.ThemePickerOptions{
		Themes:       PickableThemes(),
		InitialTheme: name,
		HideSwatch:   func(t Theme) bool { return isMatchOmarchy(t.Name) },
	})
	picker.Open(name)
	return picker
}

// openThemePicker shows the picker on the confirmed theme with no filter.
func (m *Model) openThemePicker() {
	m.overlay = overlayThemePicker
	m.themePicker = newThemePicker(pickableThemeNameAt(m.confirmedTheme))
}

// renderThemePicker draws the picker as a finished panel.
func (m Model) renderThemePicker(width, height int) string {
	renderer := tideui.NewRenderer(m.styles.Theme, tideui.StyleOptions{})
	return m.themePicker.SoftModal(renderer, width, height, "tidemail").Content
}

// themePickerKey turns a key press into the key the picker understands. While
// the search has focus every key is typed text and passes through untouched.
// Otherwise only TideMail's own bindings for up, down, confirm and cancel are
// forwarded, so a remapped binding works here and an unbound key does nothing.
// "/" starts the search.
func (m Model) themePickerKey(msg tea.KeyMsg) (tea.KeyMsg, bool) {
	if m.themePicker.Filtering() {
		return msg, true
	}
	switch {
	case msg.String() == "/":
		return msg, true
	case keyMatches(msg, m.keys.Up):
		return tea.KeyMsg{Type: tea.KeyUp}, true
	case keyMatches(msg, m.keys.Down):
		return tea.KeyMsg{Type: tea.KeyDown}, true
	case keyMatches(msg, m.keys.Confirm):
		return tea.KeyMsg{Type: tea.KeyEnter}, true
	case keyMatches(msg, m.keys.Cancel):
		return tea.KeyMsg{Type: tea.KeyEsc}, true
	}
	return msg, false
}

// previewTheme applies theme idx across the app without saving it.
func (m *Model) previewTheme(idx int) {
	m.activeTheme = idx
	m.setStyles(BuildStyles(MergedBuiltinThemeAtIndex(m.cfg, m.activeTheme), m.cfg.Display.Density, m.cfg.Display.PaneCorners))
	if m.activeMessageRowCount() > 0 {
		m.setViewportForCurrentRow()
	}
}

func (m Model) handleThemePickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key, ok := m.themePickerKey(msg)
	if !ok {
		return m, nil
	}
	prevTheme := m.activeTheme
	var themeCmd tea.Cmd

	switch m.themePicker.Update(key) {
	case tideui.ThemePickerConfirm:
		_, idx := ThemeByName(m.themePicker.ConfirmedTheme().Name)
		m.confirmedTheme = idx
		m.overlay = overlayNone
		m.cfg.Theme = pickableThemeNameAt(m.confirmedTheme)
		_ = m.saveConfig()
		themeCmd = m.startOmarchyWatchIfNeeded()
	case tideui.ThemePickerCancel:
		m.overlay = overlayNone
		m.previewTheme(m.confirmedTheme)
	default:
		// Navigation and search move the highlighted theme: preview it.
		if _, idx := ThemeByName(m.themePicker.PreviewTheme().Name); idx != m.activeTheme {
			m.previewTheme(idx)
		}
	}
	if m.activeTheme != prevTheme {
		return m, tea.Batch(themeCmd, setTermColorsCmd(m.styles.Theme.Fg, m.styles.Theme.Bg))
	}
	return m, themeCmd
}
