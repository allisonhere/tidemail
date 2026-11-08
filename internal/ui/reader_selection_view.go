package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// A drag changes only the reader's character ranges. Keep the normally rendered
// screen underneath them, including Focus Line styling, borders and status.
// Pointer ownership lets View reuse it through Bubble Tea's value receivers.
// Scrolling rebuilds the template; other updates invalidate it before dispatch.
type readerSelectionViewCache struct {
	valid                 bool
	width, height, offset int
	focused               pane
	lines                 []string
}

func (m *Model) invalidateReaderSelectionView() {
	if m.readerMouse.view != nil {
		m.readerMouse.view.valid = false
	}
}

func (m *Model) keepReaderSelectionView(msg tea.Msg) bool {
	if m.overlay != overlayNone || !m.cfg.Display.MouseSupport || !m.readerMouse.dragging {
		return false
	}
	switch event := msg.(type) {
	case readerSelectionTick:
		return true
	case tea.MouseMsg:
		return event.Action == tea.MouseActionMotion || event.Action == tea.MouseActionRelease || tea.MouseEvent(event).IsWheel()
	}
	return false
}

func (m Model) renderReaderSelectionView() string {
	cache := m.readerMouse.view
	if !cache.valid || cache.offset != m.viewport.YOffset || cache.width != m.width || cache.height != m.height || cache.focused != m.focused || (m.readerPanes != nil && !m.readerPanes.valid) {
		base := m
		base.readerMouse.dragging = false
		base.readerMouse.moved = false
		cache.lines = strings.Split(base.View(), "\n")
		cache.width, cache.height, cache.offset = m.width, m.height, m.viewport.YOffset
		cache.focused = m.focused
		cache.valid = true
	}
	if !m.readerMouse.moved {
		return strings.Join(cache.lines, "\n")
	}
	lines := append([]string(nil), cache.lines...)
	a, b := m.readerMouseRange()
	left, top, width, height := m.readerMouseBounds()
	for i := 0; i < height && top+i < len(lines); i++ {
		row := m.viewport.YOffset + i
		if row < a.row || row > b.row {
			continue
		}
		line := cache.lines[top+i]
		if lineHasImagePlaceholder(ansi.Cut(line, left, left+width)) {
			continue
		}
		lo, hi := 0, width
		if row == a.row {
			lo = a.col
		}
		if row == b.row {
			hi = b.col
		}
		hi = min(hi, width, m.width-left)
		if hi <= lo {
			continue
		}
		lo += left
		hi += left
		lines[top+i] = ansi.Cut(line, 0, lo) + m.styles.ContentFocusLine.Render(ansi.Strip(ansi.Cut(line, lo, hi))) + ansi.Cut(line, hi, m.width)
	}
	return strings.Join(lines, "\n")
}
