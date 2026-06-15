package editor

import (
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"
)

// Clipboard is the system clipboard the editor uses for copy, cut, and paste.
// Provide one with SetClipboard; without it those keys are inert. Keeping it an
// interface lets the editor own its clipboard keybindings without depending on
// any particular OS-clipboard implementation.
type Clipboard interface {
	Read() (string, error)
	Write(text string) error
}

// CopiedMsg reports the result of a copy or cut. A nil Err means the selection
// reached the clipboard; the host may surface a failure.
type CopiedMsg struct{ Err error }

// PasteMsg carries clipboard text back into Update after a ctrl+v read, so it is
// inserted as a single atomic, sanitized edit. The host must route it to Update
// — forwarding every message to the focused editor is enough.
type PasteMsg struct {
	Text string
	Err  error
}

type Options struct {
	Cursor      string
	Selected    func(string) string
	Placeholder func(string) string
}

type Model struct {
	text      []rune
	cursor    int
	selAnchor int
	width     int
	height    int
	viewport  int
	goalCol   int

	undo         []snapshot
	redo         []snapshot
	coalesceKind editKind

	blurred     bool // when true the cursor is not rendered
	placeholder string
	clipboard   Clipboard
}

func New() Model {
	return Model{width: 80, height: 24, selAnchor: -1}
}

func (m Model) Value() string {
	return string(m.text)
}

// SetValue replaces the whole document and resets undo/redo history — loading
// fresh content is a new starting point, not an undoable edit.
func (m *Model) SetValue(s string) {
	m.text = []rune(sanitizeInput(s))
	m.cursor = len(m.text)
	m.selAnchor = -1
	m.undo = nil
	m.redo = nil
	m.coalesceKind = kindNone
	m.clamp()
	m.ensureCursorVisible()
}

// InsertString inserts text as a single, atomic undo unit. Use it for
// programmatic inserts (e.g. paste from the UI layer); interactive typing
// routes through UpdateKey, which coalesces consecutive runes.
func (m *Model) InsertString(s string) {
	m.pushUndo(kindBoundary)
	m.insertRunes(s)
}

func (m *Model) insertRunes(s string) {
	m.deleteSelection()
	r := []rune(sanitizeInput(s))
	next := make([]rune, 0, len(m.text)+len(r))
	next = append(next, m.text[:m.cursor]...)
	next = append(next, r...)
	next = append(next, m.text[m.cursor:]...)
	m.text = next
	m.cursor += len(r)
	m.selAnchor = -1
	m.goalCol = -1
	m.ensureCursorVisible()
}

func (m Model) SelectedText() string {
	start, end, ok := m.selectionRange()
	if !ok {
		return ""
	}
	return string(m.text[start:end])
}

func (m *Model) DeleteSelection() {
	if _, _, ok := m.selectionRange(); ok {
		m.pushUndo(kindBoundary)
	}
	m.deleteSelection()
	m.ensureCursorVisible()
}

func (m *Model) SelectAll() {
	m.selAnchor = 0
	m.cursor = len(m.text)
	m.goalCol = -1
	m.coalesceKind = kindNone
	m.ensureCursorVisible()
}

func (m *Model) ClearSelection() {
	m.selAnchor = -1
	m.coalesceKind = kindNone
}

func (m *Model) SetSize(width, height int) {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	m.width = width
	m.height = height
	m.ensureCursorVisible()
}

func (m Model) CursorIndex() int {
	return m.cursor
}

func (m Model) ViewportTop() int {
	return m.viewport
}

// Focus makes the editor render its cursor. Editors are focused by default.
func (m *Model) Focus() { m.blurred = false }

// Blur stops the editor from rendering its cursor (e.g. when focus moves to
// another field). Editing state is otherwise unchanged.
func (m *Model) Blur() { m.blurred = true }

// SetPlaceholder sets hint text shown only while the document is empty.
func (m *Model) SetPlaceholder(s string) { m.placeholder = s }

// SetClipboard wires the system clipboard used by copy/cut/paste. Pass nil to
// disable those keys (the editor then leaves the clipboard untouched).
func (m *Model) SetClipboard(c Clipboard) { m.clipboard = c }

// Update applies a message and returns the updated editor plus any command it
// produced. Keys that touch the clipboard (copy/cut/paste) emit a tea.Cmd so
// the content change and the clipboard side effect travel back together — the
// host batches them via the standard (model, cmd) return rather than wiring the
// clipboard itself. A PasteMsg (the result of a ctrl+v read) is inserted here.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)
	case PasteMsg:
		if msg.Err == nil && msg.Text != "" {
			m.InsertString(stripCombiningMarks(msg.Text))
		}
		return m, nil
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg.Type {
	case tea.KeyRunes:
		// A bracketed paste is one atomic undo unit; typed runes coalesce.
		if msg.Paste {
			m.pushUndo(kindBoundary)
		} else {
			m.pushUndo(kindInsertRune)
		}
		m.insertRunes(string(msg.Runes))
	case tea.KeySpace:
		m.pushUndo(kindBoundary)
		m.insertRunes(" ")
	case tea.KeyEnter:
		m.pushUndo(kindBoundary)
		m.insertRunes("\n")
	case tea.KeyBackspace:
		_, _, hasSel := m.selectionRange()
		if hasSel {
			m.pushUndo(kindBoundary)
		} else if m.cursor > 0 {
			m.pushUndo(kindBackspace)
		}
		if !m.deleteSelection() && m.cursor > 0 {
			m.text = append(m.text[:m.cursor-1], m.text[m.cursor:]...)
			m.cursor--
		}
		m.goalCol = -1
	case tea.KeyDelete:
		_, _, hasSel := m.selectionRange()
		if hasSel {
			m.pushUndo(kindBoundary)
		} else if m.cursor < len(m.text) {
			m.pushUndo(kindDelete)
		}
		if !m.deleteSelection() && m.cursor < len(m.text) {
			m.text = append(m.text[:m.cursor], m.text[m.cursor+1:]...)
		}
		m.goalCol = -1
	case tea.KeyCtrlZ:
		m.Undo()
	case tea.KeyCtrlY:
		m.Redo()
	case tea.KeyCtrlA:
		m.SelectAll()
	case tea.KeyCtrlC:
		cmd = m.copyCmd(m.SelectedText())
	case tea.KeyCtrlX:
		if sel := m.SelectedText(); sel != "" {
			m.DeleteSelection()
			cmd = m.copyCmd(sel)
		}
	case tea.KeyCtrlV:
		cmd = m.pasteCmd()
	case tea.KeyLeft, tea.KeyShiftLeft:
		m.moveHorizontal(-1, isSelectionKey(msg.Type))
	case tea.KeyRight, tea.KeyShiftRight:
		m.moveHorizontal(1, isSelectionKey(msg.Type))
	case tea.KeyCtrlLeft, tea.KeyCtrlShiftLeft:
		m.moveWord(-1, isSelectionKey(msg.Type))
	case tea.KeyCtrlRight, tea.KeyCtrlShiftRight:
		m.moveWord(1, isSelectionKey(msg.Type))
	case tea.KeyUp, tea.KeyShiftUp:
		m.moveVertical(-1, isSelectionKey(msg.Type))
	case tea.KeyDown, tea.KeyShiftDown:
		m.moveVertical(1, isSelectionKey(msg.Type))
	case tea.KeyHome, tea.KeyShiftHome:
		m.moveLineBoundary(false, isSelectionKey(msg.Type))
	case tea.KeyEnd, tea.KeyShiftEnd:
		m.moveLineBoundary(true, isSelectionKey(msg.Type))
	case tea.KeyCtrlHome, tea.KeyCtrlShiftHome:
		m.moveTo(0, isSelectionKey(msg.Type))
	case tea.KeyCtrlEnd, tea.KeyCtrlShiftEnd:
		m.moveTo(len(m.text), isSelectionKey(msg.Type))
	// Bubble Tea v1.3 has no KeyShiftPgUp/KeyShiftPgDown, so page-up/down
	// selection variants cannot be wired up cleanly; page motion clears any
	// selection like an unshifted arrow.
	case tea.KeyPgUp:
		m.moveVertical(-m.height, false)
	case tea.KeyPgDown:
		m.moveVertical(m.height, false)
	}

	m.clamp()
	m.ensureCursorVisible()
	return m, cmd
}

// copyCmd writes text to the clipboard. It returns nil when there is no
// clipboard wired or nothing selected, so copying an empty selection is inert.
func (m Model) copyCmd(text string) tea.Cmd {
	if m.clipboard == nil || text == "" {
		return nil
	}
	cb := m.clipboard
	return func() tea.Msg { return CopiedMsg{Err: cb.Write(text)} }
}

// pasteCmd reads the clipboard and returns a PasteMsg for Update to insert.
func (m Model) pasteCmd() tea.Cmd {
	if m.clipboard == nil {
		return nil
	}
	cb := m.clipboard
	return func() tea.Msg {
		text, err := cb.Read()
		return PasteMsg{Text: text, Err: err}
	}
}

func (m Model) View(opts Options) string {
	// The cursor is only drawn while focused; a blurred editor renders its
	// glyphs unobscured.
	cursor := ""
	if !m.blurred {
		cursor = opts.Cursor
		if cursor == "" {
			cursor = " "
		}
	}
	if len(m.text) == 0 && m.placeholder != "" {
		return m.renderPlaceholder(cursor, opts.Placeholder)
	}
	lines := m.visualLines()
	if len(lines) == 0 {
		lines = []visualLine{{start: 0, end: 0}}
	}
	if m.viewport > len(lines)-1 {
		m.viewport = max(0, len(lines)-1)
	}
	cursorRow, _ := m.visualPosition(m.cursor)
	var out []string
	for row := 0; row < m.height; row++ {
		lineIdx := m.viewport + row
		if lineIdx >= len(lines) {
			out = append(out, "")
			continue
		}
		vl := lines[lineIdx]
		lineCursor := ""
		if lineIdx == cursorRow {
			lineCursor = cursor
		}
		out = append(out, m.renderVisualLine(vl, lineCursor, opts.Selected))
	}
	return strings.Join(out, "\n")
}

func (m Model) renderVisualLine(vl visualLine, cursor string, selected func(string) string) string {
	selStart, selEnd, hasSelection := m.selectionRange()
	var out strings.Builder
	flush := func(buf *[]rune, mark bool) {
		if len(*buf) == 0 {
			return
		}
		text := string(*buf)
		if mark && selected != nil {
			text = selected(text)
		}
		out.WriteString(text)
		*buf = (*buf)[:0]
	}
	var buf []rune
	bufSelected := false
	for idx := vl.start; idx <= vl.end; idx++ {
		if cursor != "" && idx == m.cursor {
			flush(&buf, bufSelected)
			out.WriteString(cursor)
			if idx < vl.end {
				continue
			}
		}
		if idx == vl.end {
			break
		}
		marked := hasSelection && idx >= selStart && idx < selEnd
		if len(buf) > 0 && marked != bufSelected {
			flush(&buf, bufSelected)
		}
		bufSelected = marked
		buf = append(buf, m.text[idx])
	}
	flush(&buf, bufSelected)
	return out.String()
}

// renderPlaceholder draws the placeholder hint for an empty document. It honors
// the configured width (wrapping) and height (exact line count), styles the
// text via style (if non-nil), and overlays the cursor on the first cell when
// focused — mirroring the editor's block-cursor model.
func (m Model) renderPlaceholder(cursor string, style func(string) string) string {
	ph := []rune(m.placeholder)
	lines := appendWrapped(nil, ph, 0, len(ph), max(1, m.width))
	out := make([]string, 0, m.height)
	for row := 0; row < m.height; row++ {
		if row >= len(lines) {
			if row == 0 && cursor != "" {
				out = append(out, cursor)
			} else {
				out = append(out, "")
			}
			continue
		}
		seg := ph[lines[row].start:lines[row].end]
		if row == 0 && cursor != "" {
			rest := ""
			if len(seg) > 0 {
				rest = string(seg[1:])
			}
			if style != nil {
				rest = style(rest)
			}
			out = append(out, cursor+rest)
			continue
		}
		text := string(seg)
		if style != nil {
			text = style(text)
		}
		out = append(out, text)
	}
	return strings.Join(out, "\n")
}

// Undo reverts the most recent edit group, restoring the prior text, cursor,
// and selection. It returns false when there is nothing to undo.
func (m *Model) Undo() bool {
	if len(m.undo) == 0 {
		return false
	}
	cur := m.currentSnapshot()
	prev := m.undo[len(m.undo)-1]
	m.undo = m.undo[:len(m.undo)-1]
	m.redo = append(m.redo, cur)
	m.restore(prev)
	m.coalesceKind = kindNone
	return true
}

// Redo re-applies the most recently undone edit group. It returns false when
// there is nothing to redo.
func (m *Model) Redo() bool {
	if len(m.redo) == 0 {
		return false
	}
	cur := m.currentSnapshot()
	next := m.redo[len(m.redo)-1]
	m.redo = m.redo[:len(m.redo)-1]
	m.undo = append(m.undo, cur)
	m.restore(next)
	m.coalesceKind = kindNone
	return true
}

// pushUndo records the pre-edit state before a mutation. Consecutive edits of
// the same coalescing kind (typing, backspace, delete) share one undo unit;
// every push invalidates the redo stack.
func (m *Model) pushUndo(kind editKind) {
	m.redo = nil
	if kind.coalesces() && m.coalesceKind == kind && len(m.undo) > 0 {
		return
	}
	m.undo = append(m.undo, m.currentSnapshot())
	if len(m.undo) > maxUndoLevels {
		m.undo = m.undo[len(m.undo)-maxUndoLevels:]
	}
	m.coalesceKind = kind
}

func (m Model) currentSnapshot() snapshot {
	return snapshot{text: string(m.text), cursor: m.cursor, anchor: m.selAnchor}
}

func (m *Model) restore(s snapshot) {
	m.text = []rune(s.text)
	m.cursor = s.cursor
	m.selAnchor = s.anchor
	m.goalCol = -1
	m.clamp()
	m.ensureCursorVisible()
}

func (m *Model) deleteSelection() bool {
	start, end, ok := m.selectionRange()
	if !ok {
		return false
	}
	m.text = append(m.text[:start], m.text[end:]...)
	m.cursor = start
	m.selAnchor = -1
	return true
}

func (m Model) selectionRange() (int, int, bool) {
	if m.selAnchor < 0 || m.selAnchor == m.cursor {
		return 0, 0, false
	}
	start, end := m.selAnchor, m.cursor
	if start > end {
		start, end = end, start
	}
	return start, end, true
}

func (m *Model) moveHorizontal(delta int, selecting bool) {
	m.moveTo(m.cursor+delta, selecting)
	m.goalCol = -1
}

func (m *Model) moveVertical(deltaRows int, selecting bool) {
	row, col := m.visualPosition(m.cursor)
	if m.goalCol >= 0 {
		col = m.goalCol
	} else {
		m.goalCol = col
	}
	m.moveTo(m.indexAtVisual(row+deltaRows, col), selecting)
}

// moveWord moves the cursor one word in dir (-1 left, +1 right). A word is a
// run of letters/digits/underscores; moving skips any intervening separators
// and then traverses the adjacent word, matching common editor "ctrl+arrow"
// behavior.
func (m *Model) moveWord(dir int, selecting bool) {
	pos := m.cursor
	if dir < 0 {
		for pos > 0 && !isWordRune(m.text[pos-1]) {
			pos--
		}
		for pos > 0 && isWordRune(m.text[pos-1]) {
			pos--
		}
	} else {
		for pos < len(m.text) && !isWordRune(m.text[pos]) {
			pos++
		}
		for pos < len(m.text) && isWordRune(m.text[pos]) {
			pos++
		}
	}
	m.moveTo(pos, selecting)
	m.goalCol = -1
}

// moveLineBoundary implements Home/End. We deliberately operate on the logical
// line (bounded by '\n' or the document edges), not the wrapped visual row, so
// the behavior is deterministic regardless of the current width.
func (m *Model) moveLineBoundary(end bool, selecting bool) {
	start, lineEnd := m.logicalLineRange(m.cursor)
	if end {
		m.moveTo(lineEnd, selecting)
	} else {
		m.moveTo(start, selecting)
	}
	m.goalCol = -1
}

func (m *Model) moveTo(pos int, selecting bool) {
	if pos < 0 {
		pos = 0
	}
	if pos > len(m.text) {
		pos = len(m.text)
	}
	if selecting {
		if m.selAnchor < 0 {
			m.selAnchor = m.cursor
		}
	} else {
		m.selAnchor = -1
	}
	m.cursor = pos
	// Any cursor movement ends the current edit group, so the next insert or
	// deletion starts a fresh undo unit rather than coalescing across a move.
	m.coalesceKind = kindNone
}

func (m *Model) clamp() {
	if m.width < 1 {
		m.width = 80
	}
	if m.height < 1 {
		m.height = 24
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor > len(m.text) {
		m.cursor = len(m.text)
	}
	if m.selAnchor > len(m.text) {
		m.selAnchor = len(m.text)
	}
}

func (m *Model) ensureCursorVisible() {
	row, _ := m.visualPosition(m.cursor)
	if row < m.viewport {
		m.viewport = row
	}
	if row >= m.viewport+m.height {
		m.viewport = row - m.height + 1
	}
	// Keep the viewport stable at the document edges: never scroll past the
	// last page (which would show only blank rows) and never go negative.
	maxTop := len(m.visualLines()) - m.height
	if maxTop < 0 {
		maxTop = 0
	}
	if m.viewport > maxTop {
		m.viewport = maxTop
	}
	if m.viewport < 0 {
		m.viewport = 0
	}
}

type visualLine struct {
	start int
	end   int
}

func (m Model) visualLines() []visualLine {
	width := max(1, m.width)
	var lines []visualLine
	lineStart := 0
	for i, r := range m.text {
		if r == '\n' {
			lines = appendWrapped(lines, m.text, lineStart, i, width)
			lineStart = i + 1
		}
	}
	lines = appendWrapped(lines, m.text, lineStart, len(m.text), width)
	return lines
}

func appendWrapped(lines []visualLine, text []rune, start, end, width int) []visualLine {
	if start == end {
		return append(lines, visualLine{start: start, end: end})
	}
	lineStart := start
	lineWidth := 0
	for i := start; i < end; i++ {
		rw := runeCellWidth(text[i])
		if lineWidth > 0 && lineWidth+rw > width {
			lines = append(lines, visualLine{start: lineStart, end: i})
			lineStart = i
			lineWidth = 0
		}
		lineWidth += rw
	}
	lines = append(lines, visualLine{start: lineStart, end: end})
	if lineWidth == width {
		lines = append(lines, visualLine{start: end, end: end})
	}
	return lines
}

func (m Model) visualPosition(index int) (int, int) {
	lines := m.visualLines()
	for row, line := range lines {
		if index >= line.start && index < line.end {
			return row, m.cellWidth(line.start, index)
		}
		if index == line.end {
			// At a line boundary. Only a soft wrap — where the next visual line
			// continues the same logical line, starting at this same index —
			// pushes the caret to that next line's start. A newline boundary
			// (next line starts past the '\n'), the final line, or an empty line
			// all keep the caret at the end of this line.
			if row+1 < len(lines) && lines[row+1].start == line.end {
				continue
			}
			return row, m.cellWidth(line.start, line.end)
		}
	}
	if len(lines) == 0 {
		return 0, 0
	}
	last := lines[len(lines)-1]
	return len(lines) - 1, m.cellWidth(last.start, last.end)
}

func (m Model) indexAtVisual(row, col int) int {
	lines := m.visualLines()
	if row < 0 {
		row = 0
	}
	if row >= len(lines) {
		row = len(lines) - 1
	}
	if len(lines) == 0 {
		return 0
	}
	line := lines[row]
	if col < 0 {
		col = 0
	}
	used := 0
	for i := line.start; i < line.end; i++ {
		next := used + runeCellWidth(m.text[i])
		if col < next {
			return i
		}
		used = next
	}
	return line.end
}

func (m Model) logicalLineRange(index int) (int, int) {
	if index < 0 {
		index = 0
	}
	if index > len(m.text) {
		index = len(m.text)
	}
	start := index
	for start > 0 && m.text[start-1] != '\n' {
		start--
	}
	end := index
	for end < len(m.text) && m.text[end] != '\n' {
		end++
	}
	return start, end
}

func isSelectionKey(t tea.KeyType) bool {
	switch t {
	case tea.KeyShiftLeft, tea.KeyShiftRight, tea.KeyShiftUp, tea.KeyShiftDown,
		tea.KeyShiftHome, tea.KeyShiftEnd, tea.KeyCtrlShiftHome, tea.KeyCtrlShiftEnd,
		tea.KeyCtrlShiftLeft, tea.KeyCtrlShiftRight:
		return true
	default:
		return false
	}
}

func (m Model) cellWidth(start, end int) int {
	if start < 0 {
		start = 0
	}
	if end > len(m.text) {
		end = len(m.text)
	}
	if end < start {
		end = start
	}
	width := 0
	for _, r := range m.text[start:end] {
		width += runeCellWidth(r)
	}
	return width
}

func runeCellWidth(r rune) int {
	width := runewidth.RuneWidth(r)
	if width < 1 {
		return 1
	}
	return width
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

// sanitizeInput normalizes text entering the buffer so it cannot corrupt
// terminal rendering. A stray carriage return moves the cursor to column 0,
// a tab jumps to the next tab stop, and other C0 controls (including ESC,
// which could inject ANSI) have no place in editable text. Newlines survive.
// stripCombiningMarks removes non-spacing and enclosing combining marks, which
// can corrupt terminal rendering when pasted in bulk. Control-character and
// line-ending normalization is left to sanitizeInput, which insertRunes runs.
func stripCombiningMarks(s string) string {
	if strings.IndexFunc(s, isCombiningMark) < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if isCombiningMark(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isCombiningMark(r rune) bool {
	return unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r)
}

func sanitizeInput(s string) string {
	needsWork := false
	for _, r := range s {
		if (r < 0x20 && r != '\n') || r == 0x7f {
			needsWork = true
			break
		}
	}
	if !needsWork {
		return s
	}
	// Normalize line endings first: CRLF and lone CR both become a single \n.
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteByte('\n')
		case r == '\t':
			b.WriteString("    ")
		case r < 0x20 || r == 0x7f:
			// drop other control characters (e.g. NUL, BEL, ESC)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// maxUndoLevels caps history growth; the oldest entries are dropped past this.
const maxUndoLevels = 256

type snapshot struct {
	text   string
	cursor int
	anchor int
}

// editKind classifies a mutation so consecutive edits of the same kind can
// coalesce into a single undo unit. Boundary edits (space, newline, paste,
// selection delete, programmatic insert) never coalesce.
type editKind int

const (
	kindNone editKind = iota
	kindInsertRune
	kindBackspace
	kindDelete
	kindBoundary
)

func (k editKind) coalesces() bool {
	switch k {
	case kindInsertRune, kindBackspace, kindDelete:
		return true
	default:
		return false
	}
}
