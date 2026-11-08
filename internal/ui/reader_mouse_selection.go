package ui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Positions are terminal cell boundaries in the existing wrapped content.
// This state is independent of keyboard visual-line selection.
type readerTextPoint struct{ row, col int }

type readerMouseSelection struct {
	anchor, end              readerTextPoint
	dragging, moved, pending bool
	x, y                     int
	generation               uint64
	view                     *readerSelectionViewCache
}

type readerSelectionTick uint64

func (m *Model) clearReaderMouseSelection() {
	generation := m.readerMouse.generation + 1
	m.readerMouse = readerMouseSelection{generation: generation}
}

func (m *Model) readerMouseBounds() (int, int, int, int) {
	return m.feedsPaneWidth() + 1, m.articlesPaneOuterHeight() + 1 + m.paneHeaderHeight(), m.contentPaneContentWidth(), m.contentBodyHeight()
}

func (m *Model) readerMousePoint(x, y int) readerTextPoint {
	left, top, _, height := m.readerMouseBounds()
	row := clamp(m.viewport.YOffset+clamp(y-top, 0, height-1), 0, len(m.contentLines)-1)
	line := ansi.Strip(m.contentLines[row])
	col := clamp(x-left, 0, ansi.StringWidth(line))
	// Snap a boundary inside a wide glyph to its left edge.
	col = ansi.StringWidth(ansi.Truncate(line, col, ""))
	return readerTextPoint{row, col}
}

func (m *Model) readerMouseRange() (readerTextPoint, readerTextPoint) {
	a, b := m.readerMouse.anchor, m.readerMouse.end
	if a.row > b.row || (a.row == b.row && a.col > b.col) {
		a, b = b, a
	}
	return a, b
}

func (m *Model) readerMouseText() string {
	if !m.readerMouse.moved || len(m.contentLines) == 0 {
		return ""
	}
	a, b := m.readerMouseRange()
	lines := make([]string, 0, b.row-a.row+1)
	for row := a.row; row <= b.row; row++ {
		line := ansi.Strip(m.contentLines[row])
		lo, hi := 0, ansi.StringWidth(line)
		if row == a.row {
			lo = a.col
		}
		if row == b.row {
			hi = b.col
		}
		lines = append(lines, ansi.Cut(line, lo, hi))
	}
	return strings.Join(lines, "\n")
}

func (m *Model) readerMouseScrollCmd() tea.Cmd {
	_, top, _, height := m.readerMouseBounds()
	s := &m.readerMouse
	if !s.dragging || s.pending || (s.y >= top && s.y < top+height) ||
		(s.y < top && m.viewport.AtTop()) || (s.y >= top+height && m.viewport.AtBottom()) {
		return nil
	}
	s.pending = true
	generation := s.generation
	return tea.Tick(40*time.Millisecond, func(time.Time) tea.Msg { return readerSelectionTick(generation) })
}

func (m Model) handleReaderSelectionTick(t readerSelectionTick) (tea.Model, tea.Cmd) {
	if uint64(t) != m.readerMouse.generation {
		return m, nil
	}
	m.readerMouse.pending = false
	if !m.readerMouse.dragging || m.overlay != overlayNone || !m.cfg.Display.MouseSupport || len(m.contentLines) == 0 {
		return m, nil
	}
	_, top, _, height := m.readerMouseBounds()
	delta := 0
	if m.readerMouse.y < top {
		delta = -1
	}
	if m.readerMouse.y >= top+height {
		delta = 1
	}
	if delta != 0 {
		m.viewport.SetYOffset(m.viewport.YOffset + delta)
		m.reconcileContentFocus(delta)
		m.readerMouse.end = m.readerMousePoint(m.readerMouse.x, m.readerMouse.y)
		m.readerMouse.moved = true
	}
	return m, m.readerMouseScrollCmd()
}

func (m *Model) readerSelectionMouse(msg tea.MouseMsg) (bool, tea.Cmd) {
	s := &m.readerMouse
	if s.dragging {
		if tea.MouseEvent(msg).IsWheel() {
			return true, nil
		}
		if (msg.Action == tea.MouseActionMotion || msg.Action == tea.MouseActionRelease) &&
			(msg.Button == tea.MouseButtonLeft || msg.Button == tea.MouseButtonNone) {
			s.x, s.y = msg.X, msg.Y
			s.end = m.readerMousePoint(msg.X, msg.Y)
			s.moved = s.moved || s.end != s.anchor
			if msg.Action == tea.MouseActionRelease {
				s.dragging = false
				s.generation++
				s.pending = false
				if !s.moved {
					_, top, _, _ := m.readerMouseBounds()
					next, cmd := m.handleContentClick(msg.X, msg.Y, top-1-m.paneHeaderHeight())
					*m = next.(Model)
					return true, cmd
				}
				return true, nil
			}
			return true, m.readerMouseScrollCmd()
		}
		return true, nil
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return false, nil
	}
	m.clearReaderMouseSelection()
	left, top, width, height := m.readerMouseBounds()
	if m.overlay != overlayNone || len(m.contentLines) == 0 || msg.X < left || msg.X >= left+width || msg.Y < top || msg.Y >= top+height {
		return false, nil
	}
	next, _ := m.focusPane(paneContent)
	*m = next.(Model)
	m.clearContentLinkSelection()
	p := m.readerMousePoint(msg.X, msg.Y)
	m.readerMouse.anchor, m.readerMouse.end = p, p
	m.readerMouse.dragging = true
	m.readerMouse.view = &readerSelectionViewCache{}
	m.readerMouse.x, m.readerMouse.y = msg.X, msg.Y
	return true, nil
}

func (m *Model) renderReaderMouseSelection(body string) string {
	if !m.readerMouse.moved {
		return body
	}
	a, b := m.readerMouseRange()
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		row := m.viewport.YOffset + i
		if row < a.row || row > b.row || lineHasImagePlaceholder(line) {
			continue
		}
		lo, hi := 0, ansi.StringWidth(ansi.Strip(line))
		if row == a.row {
			lo = a.col
		}
		if row == b.row {
			hi = b.col
		}
		if hi <= lo {
			continue
		}
		lines[i] = ansi.Cut(line, 0, lo) + m.styles.ContentFocusLine.Render(ansi.Strip(ansi.Cut(line, lo, hi))) + ansi.Cut(line, hi, ansi.StringWidth(line))
	}
	return strings.Join(lines, "\n")
}
