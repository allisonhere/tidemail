package editor

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/text/unicode/norm"
)

// Cursor is a zero-based position: Row is the logical line index and Col is
// the rune offset within that line.
type Cursor struct {
	Row int
	Col int
}

// Model is a multi-line text editor backed by [][]rune. It handles cursor
// movement, selection, and viewport rendering. Key handling and clipboard
// belong to the caller (compose) to avoid circular imports.
type Model struct {
	lines     [][]rune
	cursor    Cursor
	selStart  *Cursor // nil = no selection
	width     int
	height    int
	viewportY int
	Dirty     bool
}

// New creates an empty editor with the given visible dimensions.
func New(width, height int) Model {
	return Model{
		lines:  [][]rune{{}},
		width:  max(1, width),
		height: max(1, height),
	}
}

// --- Value access ---

// Value returns the full editor content as a plain string.
func (m Model) Value() string {
	var b strings.Builder
	for i, line := range m.lines {
		b.WriteString(string(line))
		if i < len(m.lines)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// SetValue replaces all content with the given string. '\r' is stripped and
// empty content results in a single empty line. The cursor moves to the end.
func (m *Model) SetValue(s string) {
	s = strings.ReplaceAll(s, "\r", "")
	if s == "" {
		m.lines = [][]rune{{}}
		m.cursor = Cursor{}
		m.selStart = nil
		m.viewportY = 0
		m.Dirty = true
		return
	}
	raw := strings.Split(s, "\n")
	m.lines = make([][]rune, len(raw))
	for i, l := range raw {
		m.lines[i] = []rune(l)
	}
	m.cursor = Cursor{Row: len(m.lines) - 1, Col: len(m.lines[len(m.lines)-1])}
	m.selStart = nil
	m.viewportY = 0
	m.Dirty = true
}

// TotalLines returns the number of logical lines in the document.
func (m Model) TotalLines() int { return len(m.lines) }

// Cursor returns the current cursor position.
func (m Model) Cursor() Cursor { return m.cursor }

// --- Cursor helpers ---

func (m Model) clampCursor(c Cursor) Cursor {
	if c.Row < 0 {
		c.Row = 0
	}
	if c.Row >= len(m.lines) {
		c.Row = len(m.lines) - 1
	}
	if c.Col < 0 {
		c.Col = 0
	}
	if c.Col > len(m.lines[c.Row]) {
		c.Col = len(m.lines[c.Row])
	}
	return c
}

// --- Insert / Delete ---

// Insert adds a single rune at the cursor and advances.
func (m *Model) Insert(ch rune) {
	m.ClearSelection()
	m.lines[m.cursor.Row] = insertRune(m.lines[m.cursor.Row], m.cursor.Col, ch)
	m.cursor.Col++
	m.Dirty = true
}

// InsertString inserts a string into the buffer. '\n' splits lines and
// '\r' is stripped. Content is NFC-normalized to prevent combining-mark
// corruption in the text buffer.
func (m *Model) InsertString(s string) {
	s = strings.ReplaceAll(s, "\r", "")
	s = norm.NFC.String(s)
	for _, r := range s {
		if r == '\n' {
			m.Newline()
		} else {
			m.Insert(r)
		}
	}
}

// DeleteBeforeCursor removes the character before the cursor (backspace).
func (m *Model) DeleteBeforeCursor() {
	m.ClearSelection()
	if m.cursor.Col > 0 {
		m.cursor.Col--
		m.lines[m.cursor.Row] = removeRune(m.lines[m.cursor.Row], m.cursor.Col)
		m.Dirty = true
		return
	}
	if m.cursor.Row > 0 {
		prevLen := len(m.lines[m.cursor.Row-1])
		m.lines[m.cursor.Row-1] = append(m.lines[m.cursor.Row-1], m.lines[m.cursor.Row]...)
		m.lines = append(m.lines[:m.cursor.Row], m.lines[m.cursor.Row+1:]...)
		m.cursor.Row--
		m.cursor.Col = prevLen
		m.Dirty = true
	}
}

// DeleteAtCursor removes the character at the cursor.
func (m *Model) DeleteAtCursor() {
	m.ClearSelection()
	if m.cursor.Col < len(m.lines[m.cursor.Row]) {
		m.lines[m.cursor.Row] = removeRune(m.lines[m.cursor.Row], m.cursor.Col)
		m.Dirty = true
		return
	}
	if m.cursor.Row < len(m.lines)-1 {
		m.lines[m.cursor.Row] = append(m.lines[m.cursor.Row], m.lines[m.cursor.Row+1]...)
		m.lines = append(m.lines[:m.cursor.Row+1], m.lines[m.cursor.Row+2:]...)
		m.Dirty = true
	}
}

// Newline splits the current line at the cursor.
func (m *Model) Newline() {
	m.ClearSelection()
	left := make([]rune, m.cursor.Col)
	copy(left, m.lines[m.cursor.Row][:m.cursor.Col])
	right := make([]rune, len(m.lines[m.cursor.Row])-m.cursor.Col)
	copy(right, m.lines[m.cursor.Row][m.cursor.Col:])
	m.lines[m.cursor.Row] = left
	m.lines = append(m.lines[:m.cursor.Row+1], append([][]rune{right}, m.lines[m.cursor.Row+1:]...)...)
	m.cursor.Row++
	m.cursor.Col = 0
	m.Dirty = true
}

func insertRune(s []rune, idx int, ch rune) []rune {
	s = append(s, 0)
	copy(s[idx+1:], s[idx:])
	s[idx] = ch
	return s
}

func removeRune(s []rune, idx int) []rune {
	return append(s[:idx], s[idx+1:]...)
}

// --- Movement ---

// MoveLeft moves one rune left. At line start, moves to end of previous line.
func (m *Model) MoveLeft() {
	if m.cursor.Col > 0 {
		m.cursor.Col--
		return
	}
	if m.cursor.Row > 0 {
		m.cursor.Row--
		m.cursor.Col = len(m.lines[m.cursor.Row])
	}
}

// MoveRight moves one rune right. At line end, moves to start of next line.
func (m *Model) MoveRight() {
	if m.cursor.Col < len(m.lines[m.cursor.Row]) {
		m.cursor.Col++
		return
	}
	if m.cursor.Row < len(m.lines)-1 {
		m.cursor.Row++
		m.cursor.Col = 0
	}
}

// MoveUp moves up one line, preserving column position as closely as possible.
func (m *Model) MoveUp() {
	if m.cursor.Row > 0 {
		m.cursor.Row--
		m.cursor.Col = min(m.cursor.Col, len(m.lines[m.cursor.Row]))
	}
}

// MoveDown moves down one line.
func (m *Model) MoveDown() {
	if m.cursor.Row < len(m.lines)-1 {
		m.cursor.Row++
		m.cursor.Col = min(m.cursor.Col, len(m.lines[m.cursor.Row]))
	}
}

// MoveHome moves to the start of the current line.
func (m *Model) MoveHome() { m.cursor.Col = 0 }

// MoveEnd moves to the end of the current line.
func (m *Model) MoveEnd() {
	m.cursor.Col = len(m.lines[m.cursor.Row])
}

// MoveDocStart moves to the start of the document.
func (m *Model) MoveDocStart() { m.cursor = Cursor{} }

// MoveDocEnd moves to the end of the document.
func (m *Model) MoveDocEnd() {
	last := len(m.lines) - 1
	m.cursor = Cursor{last, len(m.lines[last])}
}

// --- Viewport ---

func (m *Model) clampViewport() {
	m.viewportY = max(0, m.viewportY)
	maxY := max(0, len(m.lines)-m.height)
	if m.viewportY > maxY {
		m.viewportY = maxY
	}
	if m.cursor.Row < m.viewportY {
		m.viewportY = m.cursor.Row
	}
	if m.cursor.Row >= m.viewportY+m.height {
		m.viewportY = m.cursor.Row - m.height + 1
	}
	m.viewportY = max(0, m.viewportY)
}

// ScrollUp scrolls the viewport up by n lines.
func (m *Model) ScrollUp(n int) {
	m.viewportY -= n
	m.clampViewport()
}

// ScrollDown scrolls the viewport down by n lines.
func (m *Model) ScrollDown(n int) {
	m.viewportY += n
	m.clampViewport()
}

// SetSize updates visible dimensions and clamps the viewport.
func (m *Model) SetSize(w, h int) {
	m.width = max(1, w)
	m.height = max(1, h)
	m.clampViewport()
}

// --- Selection ---

// HasSelection reports whether a text range is selected.
func (m Model) HasSelection() bool {
	return m.selStart != nil && *m.selStart != m.cursor
}

// StartSelection begins a selection at the current cursor position.
func (m *Model) StartSelection() {
	c := m.cursor
	m.selStart = &c
}

// ClearSelection removes the selection anchor.
func (m *Model) ClearSelection() { m.selStart = nil }

// SelectAll selects the entire document.
func (m *Model) SelectAll() {
	m.selStart = &Cursor{}
	m.cursor = Cursor{len(m.lines) - 1, len(m.lines[len(m.lines)-1])}
}

// SelectedText returns the current selection, or empty string if nothing is
// selected.
func (m Model) SelectedText() string {
	if !m.HasSelection() {
		return ""
	}
	sel := m.selectionRange()
	var b strings.Builder
	for row := sel.start.Row; row <= sel.end.Row; row++ {
		line := m.lines[row]
		startCol := 0
		if row == sel.start.Row {
			startCol = sel.start.Col
		}
		endCol := len(line)
		if row == sel.end.Row {
			endCol = min(sel.end.Col, len(line))
		}
		b.WriteString(string(line[startCol:endCol]))
		if row < sel.end.Row {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

type selRange struct{ start, end Cursor }

func (m Model) selectionRange() selRange {
	if m.selStart == nil {
		return selRange{m.cursor, m.cursor}
	}
	a, b := *m.selStart, m.cursor
	if a.Row > b.Row || (a.Row == b.Row && a.Col > b.Col) {
		a, b = b, a
	}
	return selRange{a, b}
}

// DeleteSelection removes the selected text and moves the cursor to the
// start of the deleted range.
func (m *Model) DeleteSelection() {
	if !m.HasSelection() {
		return
	}
	sel := m.selectionRange()
	startLine := m.lines[sel.start.Row][:sel.start.Col]
	endLine := m.lines[sel.end.Row][sel.end.Col:]
	joined := make([]rune, 0, len(startLine)+len(endLine))
	joined = append(joined, startLine...)
	joined = append(joined, endLine...)
	var newLines [][]rune
	newLines = append(newLines, m.lines[:sel.start.Row]...)
	newLines = append(newLines, joined)
	newLines = append(newLines, m.lines[sel.end.Row+1:]...)
	m.lines = newLines
	m.cursor = Cursor{sel.start.Row, sel.start.Col}
	m.selStart = nil
	m.Dirty = true
}

// --- Rendering ---

// View renders the visible portion of the editor. Selected text gets inverse
// highlighting via selStyle. The caller handles background fill — View() does
// NOT pad lines to width.
func (m Model) View(selStyle lipgloss.Style) string {
	m.clampViewport()
	sel := m.selectionRange()
	hasSel := m.HasSelection()
	var out strings.Builder
	for i := m.viewportY; i < min(m.viewportY+m.height, len(m.lines)); i++ {
		if i > m.viewportY {
			out.WriteByte('\n')
		}
		line := string(m.lines[i])
		if m.width > 0 && len(line) > m.width {
			startCol := m.hscrollForLine(i)
			if startCol+m.width <= len(line) {
				line = line[startCol : startCol+m.width]
			} else if startCol < len(line) {
				line = line[startCol:]
			} else {
				line = ""
			}
		}
		if hasSel && i >= sel.start.Row && i <= sel.end.Row {
			startCol := 0
			if i == sel.start.Row {
				startCol = sel.start.Col
			}
			endCol := len([]rune(line))
			if i == sel.end.Row {
				endCol = min(sel.end.Col, endCol)
			}
			if startCol < endCol {
				runes := []rune(line)
				pre := string(runes[:startCol])
				selPart := selStyle.Render(string(runes[startCol:endCol]))
				post := string(runes[endCol:])
				out.WriteString(pre + selPart + post)
				continue
			}
		}
		out.WriteString(line)
	}
	return out.String()
}

func (m Model) hscrollForLine(i int) int {
	if i != m.cursor.Row || m.width <= 0 {
		return 0
	}
	col := m.cursor.Col
	if col < m.width {
		return 0
	}
	return col - m.width + 1
}
