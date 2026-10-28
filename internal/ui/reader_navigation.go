package ui

import tea "github.com/charmbracelet/bubbletea"

// Fail closed: all events other than these reader-only movements invalidate
// surrounding panes, including async results, resize, themes and transitions.
func (m Model) readerOnlyNavigation(msg tea.Msg) bool {
	if m.overlay != overlayNone || m.focused != paneContent {
		return false
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return keyMatches(msg, m.keys.Up, m.keys.Down)
	case tea.MouseMsg:
		if !m.cfg.Display.MouseSupport || !tea.MouseEvent(msg).IsWheel() {
			return false
		}
		target, _, _, ok := m.paneAt(msg.X, msg.Y)
		return ok && target == paneContent
	}
	return false
}
