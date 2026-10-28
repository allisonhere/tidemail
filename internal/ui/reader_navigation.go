package ui

import tea "github.com/charmbracelet/bubbletea"

// Fail closed: all events other than these reader-only movements invalidate
// surrounding panes, including async results, resize, themes and transitions.
func (m Model) readerOnlyNavigation(msg tea.Msg) bool {
	if m.overlay != overlayNone {
		return false
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.focused == paneContent && keyMatches(msg, m.keys.Up, m.keys.Down)
	case tea.MouseMsg:
		if !m.cfg.Display.MouseSupport || !tea.MouseEvent(msg).IsWheel() {
			return false
		}
		target, _, _, ok := m.paneAt(msg.X, msg.Y)
		return ok && target == paneContent
	}
	return false
}

// Reconcile position without scrolling or implying activation intent. Retain a
// visible focus; otherwise choose the closest focusable visible edge. A blank
// or header-only viewport keeps a visible navigation anchor, not stale focus.
func (m *Model) reconcileContentFocus(direction int) {
	if m.contentLineCount <= 0 {
		return
	}
	top := m.viewport.YOffset
	bottom := min(m.contentLineCount, top+m.contentBodyHeight())
	if !m.contentFocusPending && m.contentFocusLine >= top && m.contentFocusLine < bottom {
		return
	}
	m.clearContentLinkSelection()
	forward := m.contentFocusLine < top
	if m.contentFocusPending || (m.contentFocusLine >= top && m.contentFocusLine < bottom) {
		forward = direction >= 0
	}
	start, end, step := top, bottom, 1
	if !forward {
		start, end, step = bottom-1, top-1, -1
	}
	for row := start; row != end; row += step {
		if row >= 0 && row < len(m.contentFocusable) && m.contentFocusable[row] {
			m.contentFocusLine = row
			m.contentFocusPending = false
			return
		}
	}
	m.contentFocusLine = max(0, start)
	m.contentFocusPending = true
}
