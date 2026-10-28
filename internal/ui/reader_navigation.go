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

// In viewport mode, only explicit selection enters LINKS navigation. Reaching
// the bottom is inert; a further Down selects the first entry. Direct shortcuts
// and clicks share this same selection state, styling and activation intent.
func (m *Model) scrollContentWithoutFocus(delta int) {
	if m.contentLinkIdx >= 0 && m.contentLinkIdx < len(m.contentLinks) {
		index := m.contentLinkIdx + delta
		if index < 0 {
			m.clearContentLinkSelection()
			return
		}
		if index < len(m.contentLinkRows) && index < len(m.contentLinks) {
			m.selectContentLinkAt(m.contentLinkRows[index])
			m.revealContentLink()
		}
		return
	}
	m.clearContentLinkSelection()
	if delta > 0 && m.viewport.AtBottom() && len(m.contentLinkRows) > 0 {
		if m.selectContentLinkAt(m.contentLinkRows[0]) {
			m.revealContentLink()
		}
		return
	}
	if delta < 0 {
		m.viewport.ScrollUp(1)
	} else {
		m.viewport.ScrollDown(1)
	}
	m.reconcileContentFocus(delta)
}
