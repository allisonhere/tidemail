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

// Model is a multi-line text editor backed by [][]rune. Lines are soft-wrapped
// to the editor width. The viewport operates on visual (wrapped) rows.
type Model struct {
	lines     [][]rune
	cursor    Cursor
	selStart  *Cursor
	width     int
	height    int
	viewportY int // visual row index
	Dirty     bool
}

func New(width, height int) Model {
	return Model{
		lines:  [][]rune{{}},
		width:  max(1, width),
		height: max(1, height),
	}
}

// Value returns the full editor content.
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
	m.clampViewport()
}

func (m Model) TotalLines() int { return len(m.lines) }
func (m Model) Cursor() Cursor  { return m.cursor }

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

func (m *Model) Insert(ch rune) {
	m.ClearSelection()
	m.lines[m.cursor.Row] = insertRune(m.lines[m.cursor.Row], m.cursor.Col, ch)
	m.cursor.Col++
	m.Dirty = true
}

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

func (m *Model) MoveLeft() {
	if m.cursor.Col > 0 { m.cursor.Col--; return }
	if m.cursor.Row > 0 { m.cursor.Row--; m.cursor.Col = len(m.lines[m.cursor.Row]) }
}
func (m *Model) MoveRight() {
	if m.cursor.Col < len(m.lines[m.cursor.Row]) { m.cursor.Col++; return }
	if m.cursor.Row < len(m.lines)-1 { m.cursor.Row++; m.cursor.Col = 0 }
}
func (m *Model) MoveUp() {
	if m.cursor.Row > 0 { m.cursor.Row--; m.cursor.Col = min(m.cursor.Col, len(m.lines[m.cursor.Row])) }
}
func (m *Model) MoveDown() {
	if m.cursor.Row < len(m.lines)-1 { m.cursor.Row++; m.cursor.Col = min(m.cursor.Col, len(m.lines[m.cursor.Row])) }
}
func (m *Model) MoveHome()       { m.cursor.Col = 0 }
func (m *Model) MoveEnd()        { m.cursor.Col = len(m.lines[m.cursor.Row]) }
func (m *Model) MoveDocStart()   { m.cursor = Cursor{} }
func (m *Model) MoveDocEnd()     { m.cursor = Cursor{len(m.lines) - 1, len(m.lines[len(m.lines)-1])} }

func (m *Model) ScrollUp(n int)   { m.viewportY -= n; m.clampViewport() }
func (m *Model) ScrollDown(n int) { m.viewportY += n; m.clampViewport() }
func (m *Model) ClampViewport()   { m.clampViewport() }

func (m *Model) SetSize(w, h int) {
	m.width = max(1, w)
	m.height = max(1, h)
	m.clampViewport()
}

func (m Model) HasSelection() bool  { return m.selStart != nil && *m.selStart != m.cursor }
func (m *Model) StartSelection()    { c := m.cursor; m.selStart = &c }
func (m *Model) ClearSelection()    { m.selStart = nil }
func (m *Model) SelectAll() {
	m.selStart = &Cursor{}
	m.cursor = Cursor{len(m.lines) - 1, len(m.lines[len(m.lines)-1])}
}

func (m Model) SelectedText() string {
	if !m.HasSelection() { return "" }
	sel := m.selectionRange()
	var b strings.Builder
	for row := sel.start.Row; row <= sel.end.Row; row++ {
		line := m.lines[row]
		sc, ec := 0, len(line)
		if row == sel.start.Row { sc = sel.start.Col }
		if row == sel.end.Row   { ec = min(sel.end.Col, len(line)) }
		b.WriteString(string(line[sc:ec]))
		if row < sel.end.Row { b.WriteByte('\n') }
	}
	return b.String()
}

type selRange struct{ start, end Cursor }

func (m Model) selectionRange() selRange {
	if m.selStart == nil { return selRange{m.cursor, m.cursor} }
	a, b := *m.selStart, m.cursor
	if a.Row > b.Row || (a.Row == b.Row && a.Col > b.Col) { a, b = b, a }
	return selRange{a, b}
}

func (m *Model) DeleteSelection() {
	if !m.HasSelection() { return }
	sel := m.selectionRange()
	start := m.lines[sel.start.Row][:sel.start.Col]
	end := m.lines[sel.end.Row][sel.end.Col:]
	joined := append(append([]rune{}, start...), end...)
	var nl [][]rune
	nl = append(nl, m.lines[:sel.start.Row]...)
	nl = append(nl, joined)
	nl = append(nl, m.lines[sel.end.Row+1:]...)
	m.lines = nl
	m.cursor = Cursor{sel.start.Row, sel.start.Col}
	m.selStart = nil
	m.Dirty = true
}

// --- Soft-wrap rendering ---

type visualRow struct {
	logicalRow int
	colStart   int
	colEnd     int
}

func (m Model) visualRows() []visualRow {
	w := max(1, m.width)
	var rows []visualRow
	for li, line := range m.lines {
		if len(line) == 0 {
			rows = append(rows, visualRow{li, 0, 0})
			continue
		}
		for col := 0; col < len(line); col += w {
			end := col + w
			if end > len(line) { end = len(line) }
			rows = append(rows, visualRow{li, col, end})
		}
	}
	return rows
}

func (m Model) cursorVisualRow(rows []visualRow) int {
	for i, vr := range rows {
		if vr.logicalRow == m.cursor.Row && m.cursor.Col >= vr.colStart && m.cursor.Col <= vr.colEnd {
			return i
		}
	}
	if len(rows) > 0 { return len(rows) - 1 }
	return 0
}

func (m *Model) clampViewport() {
	rows := m.visualRows()
	vr := m.cursorVisualRow(rows)
	if vr < m.viewportY { m.viewportY = vr }
	if vr >= m.viewportY+m.height { m.viewportY = vr - m.height + 1 }
	maxY := max(0, len(rows)-m.height)
	if m.viewportY > maxY { m.viewportY = maxY }
	m.viewportY = max(0, m.viewportY)
}

func (m Model) View(selStyle lipgloss.Style) string {
	rows := m.visualRows()
	start := m.viewportY
	if start > len(rows)-m.height { start = max(0, len(rows)-m.height) }
	end := min(start+m.height, len(rows))
	sel := m.selectionRange()
	hasSel := m.HasSelection()
	var out strings.Builder
	for i := start; i < end; i++ {
		if i > start { out.WriteByte('\n') }
		vr := rows[i]
		text := string(m.lines[vr.logicalRow][vr.colStart:vr.colEnd])
		if hasSel {
			out.WriteString(m.highlightSelection(text, vr, sel, selStyle))
		} else {
			out.WriteString(text)
		}
	}
	return out.String()
}

func (m Model) highlightSelection(text string, vr visualRow, sel selRange, selStyle lipgloss.Style) string {
	if vr.logicalRow < sel.start.Row || vr.logicalRow > sel.end.Row { return text }
	ls := vr.colStart
	sr := sel.start.Col
	er := sel.end.Col
	if vr.logicalRow == sel.start.Row && vr.colEnd <= sr { return text }
	if vr.logicalRow == sel.end.Row && vr.colStart >= er { return text }
	si, ei := 0, len([]rune(text))
	if vr.logicalRow == sel.start.Row && sr > ls { si = sr - ls }
	if vr.logicalRow == sel.end.Row { ei = min(er-ls, ei) }
	if si >= ei { return text }
	runes := []rune(text)
	si = max(0, min(si, len(runes)))
	ei = max(si, min(ei, len(runes)))
	return string(runes[:si]) + selStyle.Render(string(runes[si:ei])) + string(runes[ei:])
}
