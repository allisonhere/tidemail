package editor

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func keyRunes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// apply drives Update in place so the sequential-key tests read naturally,
// returning any command the key produced (e.g. a clipboard write).
func apply(e *Model, msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	*e, cmd = e.Update(msg)
	return cmd
}

func TestInsertDeleteSelectAllAndSelectionText(t *testing.T) {
	var e Model

	apply(&e, keyRunes("hello"))
	e.InsertString("\nworld")

	if got := e.Value(); got != "hello\nworld" {
		t.Fatalf("value after insert = %q", got)
	}

	e.SelectAll()
	if got := e.SelectedText(); got != "hello\nworld" {
		t.Fatalf("selected text = %q", got)
	}

	e.DeleteSelection()
	if got := e.Value(); got != "" {
		t.Fatalf("value after deleting selection = %q", got)
	}

	e.InsertString("abc")
	apply(&e, tea.KeyMsg{Type: tea.KeyBackspace})
	apply(&e, tea.KeyMsg{Type: tea.KeyDelete})
	if got := e.Value(); got != "ab" {
		t.Fatalf("backspace/delete value = %q", got)
	}
}

func TestInsertStringReplacesSelection(t *testing.T) {
	var e Model
	e.SetValue("hello world")
	e.SelectAll()

	e.InsertString("replacement")

	if got := e.Value(); got != "replacement" {
		t.Fatalf("replacement value = %q", got)
	}
	if got := e.SelectedText(); got != "" {
		t.Fatalf("selection should clear after replacement, got %q", got)
	}
}

func TestPasteKeyReplacesSelection(t *testing.T) {
	var e Model
	e.SetValue("hello world")
	apply(&e, tea.KeyMsg{Type: tea.KeyCtrlHome})
	for range len("hello") {
		apply(&e, tea.KeyMsg{Type: tea.KeyShiftRight})
	}

	apply(&e, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("goodbye"), Paste: true})

	if got := e.Value(); got != "goodbye world" {
		t.Fatalf("value after paste over selection = %q", got)
	}
	if got := e.SelectedText(); got != "" {
		t.Fatalf("selection should clear after paste, got %q", got)
	}
}

func TestDeleteSelectionHandlesReversedSelection(t *testing.T) {
	var e Model
	e.SetValue("alpha beta gamma")
	apply(&e, tea.KeyMsg{Type: tea.KeyCtrlEnd})
	for range len(" gamma") {
		apply(&e, tea.KeyMsg{Type: tea.KeyShiftLeft})
	}

	e.DeleteSelection()

	if got := e.Value(); got != "alpha beta" {
		t.Fatalf("value after deleting reversed selection = %q", got)
	}
	if got := e.CursorIndex(); got != len("alpha beta") {
		t.Fatalf("cursor after deleting reversed selection = %d", got)
	}
}

func TestKeyboardSelectionAndMovement(t *testing.T) {
	var e Model
	e.SetValue("first\nsecond\nthird")

	apply(&e, tea.KeyMsg{Type: tea.KeyCtrlHome})
	apply(&e, tea.KeyMsg{Type: tea.KeyShiftDown})
	apply(&e, tea.KeyMsg{Type: tea.KeyShiftRight})

	if got := e.SelectedText(); got != "first\ns" {
		t.Fatalf("selected text after shift movement = %q", got)
	}

	apply(&e, tea.KeyMsg{Type: tea.KeyEnd})
	if got := e.SelectedText(); got != "" {
		t.Fatalf("selection should clear after plain movement, got %q", got)
	}
}

func TestViewMarksSelectedText(t *testing.T) {
	var e Model
	e.SetSize(20, 2)
	e.SetValue("select me")
	apply(&e, tea.KeyMsg{Type: tea.KeyCtrlHome})
	for range len("select") {
		apply(&e, tea.KeyMsg{Type: tea.KeyShiftRight})
	}

	view := e.View(Options{
		Cursor: "|",
		Selected: func(s string) string {
			return "[" + s + "]"
		},
	})

	if !strings.Contains(view, "[select]") {
		t.Fatalf("expected view to mark selected text, got %q", view)
	}
}

func TestViewDoesNotExceedConfiguredWidth(t *testing.T) {
	var e Model
	e.SetSize(5, 2)
	e.SetValue("abcde")
	apply(&e, tea.KeyMsg{Type: tea.KeyCtrlHome})

	view := e.View(Options{Cursor: "|"})
	for _, line := range strings.Split(view, "\n") {
		if got := len([]rune(line)); got > 5 {
			t.Fatalf("rendered line width = %d, want <= 5, view %q", got, view)
		}
	}
}

func TestViewDoesNotExceedConfiguredWidthWhenCursorIsAtLineEnd(t *testing.T) {
	var e Model
	e.SetSize(5, 2)
	e.SetValue("abcde")

	view := e.View(Options{Cursor: "|"})
	for _, line := range strings.Split(view, "\n") {
		if got := len([]rune(line)); got > 5 {
			t.Fatalf("rendered line width = %d, want <= 5, view %q", got, view)
		}
	}
}

func TestViewWrapsWideRunesByTerminalCellWidth(t *testing.T) {
	var e Model
	e.SetSize(4, 3)
	e.SetValue("界界界")
	apply(&e, tea.KeyMsg{Type: tea.KeyCtrlHome})

	view := e.View(Options{Cursor: "|"})

	lines := strings.Split(view, "\n")
	if len(lines) != 3 {
		t.Fatalf("view should render exactly height lines, got %d: %q", len(lines), view)
	}
	for _, line := range lines {
		if got := ansi.StringWidth(line); got > 4 {
			t.Fatalf("rendered line display width = %d, want <= 4, line %q in view %q", got, line, view)
		}
	}
	if !strings.Contains(lines[0], "界") || !strings.Contains(lines[1], "界") {
		t.Fatalf("wide runes should wrap across visual lines, got %q", view)
	}
}

func TestViewStyledSelectionStaysWithinConfiguredDisplayWidth(t *testing.T) {
	var e Model
	e.SetSize(5, 2)
	e.SetValue("abcde")
	e.SelectAll()

	view := e.View(Options{
		Cursor: "|",
		Selected: func(s string) string {
			return "\x1b[7m" + s + "\x1b[0m"
		},
	})

	for _, line := range strings.Split(view, "\n") {
		if got := ansi.StringWidth(line); got > 5 {
			t.Fatalf("styled rendered line display width = %d, want <= 5, line %q in view %q", got, line, view)
		}
	}
}

func TestWordMovementLeftRight(t *testing.T) {
	var e Model
	e.SetValue("foo bar baz")
	apply(&e, tea.KeyMsg{Type: tea.KeyCtrlHome})

	apply(&e, tea.KeyMsg{Type: tea.KeyCtrlRight})
	if got := e.CursorIndex(); got != 3 {
		t.Fatalf("ctrl+right should land after first word, cursor = %d", got)
	}
	apply(&e, tea.KeyMsg{Type: tea.KeyCtrlRight})
	if got := e.CursorIndex(); got != 7 {
		t.Fatalf("ctrl+right should land after second word, cursor = %d", got)
	}
	apply(&e, tea.KeyMsg{Type: tea.KeyCtrlLeft})
	if got := e.CursorIndex(); got != 4 {
		t.Fatalf("ctrl+left should land at start of word, cursor = %d", got)
	}
}

func TestWordSelectionExtendsSelection(t *testing.T) {
	var e Model
	e.SetValue("foo bar baz")
	apply(&e, tea.KeyMsg{Type: tea.KeyCtrlHome})

	apply(&e, tea.KeyMsg{Type: tea.KeyCtrlShiftRight})
	if got := e.SelectedText(); got != "foo" {
		t.Fatalf("ctrl+shift+right should select first word, got %q", got)
	}
	apply(&e, tea.KeyMsg{Type: tea.KeyCtrlShiftRight})
	if got := e.SelectedText(); got != "foo bar" {
		t.Fatalf("ctrl+shift+right should extend to second word, got %q", got)
	}
}

func TestBackspaceAndDeleteRemoveSelection(t *testing.T) {
	selectABC := func() Model {
		var e Model
		e.SetValue("abcdef")
		apply(&e, tea.KeyMsg{Type: tea.KeyCtrlHome})
		for range 3 {
			apply(&e, tea.KeyMsg{Type: tea.KeyShiftRight})
		}
		return e
	}

	bs := selectABC()
	apply(&bs, tea.KeyMsg{Type: tea.KeyBackspace})
	if got := bs.Value(); got != "def" {
		t.Fatalf("backspace over selection = %q, want %q", got, "def")
	}

	del := selectABC()
	apply(&del, tea.KeyMsg{Type: tea.KeyDelete})
	if got := del.Value(); got != "def" {
		t.Fatalf("delete over selection = %q, want %q", got, "def")
	}
	if got := del.SelectedText(); got != "" {
		t.Fatalf("selection should clear after delete, got %q", got)
	}
}

func TestHomeEndOperateOnLogicalNotWrappedLine(t *testing.T) {
	var e Model
	e.SetSize(5, 5)
	e.SetValue("abcdefghij") // wraps to "abcde" / "fghij"
	apply(&e, tea.KeyMsg{Type: tea.KeyCtrlEnd})

	// Cursor sits on the second visual row; Home must jump to the logical
	// line start (index 0), not the wrapped row start (index 5).
	apply(&e, tea.KeyMsg{Type: tea.KeyHome})
	if got := e.CursorIndex(); got != 0 {
		t.Fatalf("home should go to logical line start, cursor = %d", got)
	}
	apply(&e, tea.KeyMsg{Type: tea.KeyEnd})
	if got := e.CursorIndex(); got != 10 {
		t.Fatalf("end should go to logical line end, cursor = %d", got)
	}
}

func TestViewportStableAtDocumentEdges(t *testing.T) {
	var e Model
	e.SetSize(4, 2)
	e.SetValue("a\nb\nc\nd\ne\nf") // six single-rune visual lines

	apply(&e, tea.KeyMsg{Type: tea.KeyCtrlEnd})
	for range 3 { // over-page past the end
		apply(&e, tea.KeyMsg{Type: tea.KeyPgDown})
	}
	if got := e.ViewportTop(); got != 4 { // 6 lines - height 2
		t.Fatalf("viewport should clamp to last page at end, top = %d", got)
	}

	apply(&e, tea.KeyMsg{Type: tea.KeyCtrlHome})
	for range 3 { // over-page past the start
		apply(&e, tea.KeyMsg{Type: tea.KeyPgUp})
	}
	if got := e.ViewportTop(); got != 0 {
		t.Fatalf("viewport should clamp to 0 at start, top = %d", got)
	}
}

func TestViewRendersExactHeight(t *testing.T) {
	cases := map[string]string{
		"empty": "",
		"short": "hi",
		"long":  "one\ntwo\nthree\nfour\nfive\nsix\nseven",
	}
	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			var e Model
			e.SetSize(10, 3)
			e.SetValue(value)
			view := e.View(Options{Cursor: "|"})
			if got := len(strings.Split(view, "\n")); got != 3 {
				t.Fatalf("view should render exactly height lines, got %d: %q", got, view)
			}
		})
	}
}

func TestViewSelectionDoesNotLeakStyles(t *testing.T) {
	var e Model
	e.SetSize(20, 1)
	e.SetValue("abcdefgh")
	apply(&e, tea.KeyMsg{Type: tea.KeyCtrlHome})
	for range 3 { // move past "abc"
		apply(&e, tea.KeyMsg{Type: tea.KeyRight})
	}
	for range 2 { // select "de", leaving the cursor past the selection
		apply(&e, tea.KeyMsg{Type: tea.KeyShiftRight})
	}

	view := e.View(Options{
		Cursor: "|",
		Selected: func(s string) string {
			return "\x1b[7m" + s + "\x1b[0m"
		},
	})

	// The styled run must wrap exactly the selected text with a contained
	// reset — nothing before or after the selection may share the style.
	if !strings.Contains(view, "\x1b[7mde\x1b[0m") {
		t.Fatalf("selection should be wrapped by a self-contained style, got %q", view)
	}
	if strings.Count(view, "\x1b[7m") != 1 || strings.Count(view, "\x1b[0m") != 1 {
		t.Fatalf("style markers should appear exactly once, got %q", view)
	}
}

func TestUndoRedoRoundTrip(t *testing.T) {
	var e Model
	e.InsertString("hello")

	if !e.Undo() {
		t.Fatal("undo should report work was done")
	}
	if got := e.Value(); got != "" {
		t.Fatalf("value after undo = %q, want empty", got)
	}
	if !e.Redo() {
		t.Fatal("redo should report work was done")
	}
	if got := e.Value(); got != "hello" {
		t.Fatalf("value after redo = %q", got)
	}
}

func TestUndoCoalescesTypingButBreaksOnWordBoundary(t *testing.T) {
	var e Model
	for _, r := range "foo" {
		apply(&e, keyRunes(string(r)))
	}
	apply(&e, tea.KeyMsg{Type: tea.KeySpace})
	for _, r := range "bar" {
		apply(&e, keyRunes(string(r)))
	}
	if got := e.Value(); got != "foo bar" {
		t.Fatalf("typed value = %q", got)
	}

	e.Undo()
	if got := e.Value(); got != "foo " {
		t.Fatalf("first undo should drop the last word, got %q", got)
	}
	e.Undo()
	if got := e.Value(); got != "foo" {
		t.Fatalf("second undo should drop the space, got %q", got)
	}
	e.Undo()
	if got := e.Value(); got != "" {
		t.Fatalf("third undo should drop the first word, got %q", got)
	}
}

func TestNewEditInvalidatesRedo(t *testing.T) {
	var e Model
	apply(&e, keyRunes("a"))
	apply(&e, keyRunes("b")) // coalesces with "a" into one unit

	e.Undo()
	if got := e.Value(); got != "" {
		t.Fatalf("value after undo = %q", got)
	}
	apply(&e, keyRunes("c"))
	if e.Redo() {
		t.Fatal("redo should be invalidated after a new edit")
	}
	if got := e.Value(); got != "c" {
		t.Fatalf("value after new edit = %q", got)
	}
}

func TestUndoRestoresCursorAndSelection(t *testing.T) {
	var e Model
	e.SetValue("hello")
	e.SelectAll()
	apply(&e, tea.KeyMsg{Type: tea.KeyBackspace}) // delete the selection

	if got := e.Value(); got != "" {
		t.Fatalf("value after deleting selection = %q", got)
	}
	e.Undo()
	if got := e.Value(); got != "hello" {
		t.Fatalf("undo should restore deleted text, got %q", got)
	}
	if got := e.SelectedText(); got != "hello" {
		t.Fatalf("undo should restore the selection, got %q", got)
	}
}

func TestSetValueResetsHistory(t *testing.T) {
	var e Model
	e.SetValue("draft")

	if e.Undo() {
		t.Fatal("freshly loaded content should not be undoable")
	}
	apply(&e, tea.KeyMsg{Type: tea.KeyCtrlZ})
	if got := e.Value(); got != "draft" {
		t.Fatalf("ctrl+z on a reset document should leave content unchanged, got %q", got)
	}
}

func TestUndoRedoViaKeys(t *testing.T) {
	var e Model
	apply(&e, keyRunes("x"))

	apply(&e, tea.KeyMsg{Type: tea.KeyCtrlZ})
	if e.Value() != "" {
		t.Fatalf("ctrl+z should undo, value=%q", e.Value())
	}
	apply(&e, tea.KeyMsg{Type: tea.KeyCtrlY})
	if e.Value() != "x" {
		t.Fatalf("ctrl+y should redo, value=%q", e.Value())
	}
}

func TestBlurHidesCursorAndFocusRestoresIt(t *testing.T) {
	var e Model
	e.SetSize(20, 1)
	e.SetValue("hi")

	focused := e.View(Options{Cursor: "|"})
	if !strings.Contains(focused, "|") {
		t.Fatalf("focused editor should render the cursor, got %q", focused)
	}

	e.Blur()
	blurred := e.View(Options{Cursor: "|"})
	if strings.Contains(blurred, "|") {
		t.Fatalf("blurred editor should not render the cursor, got %q", blurred)
	}
	if !strings.Contains(blurred, "hi") {
		t.Fatalf("blurred editor should still render its text unobscured, got %q", blurred)
	}

	e.Focus()
	if got := e.View(Options{Cursor: "|"}); !strings.Contains(got, "|") {
		t.Fatalf("focus should restore the cursor, got %q", got)
	}
}

func TestPlaceholderShownOnlyWhenEmpty(t *testing.T) {
	var e Model
	e.SetSize(40, 2)
	e.SetPlaceholder("Write your message…")

	view := e.View(Options{Cursor: "|"})
	if !strings.Contains(view, "rite your message") {
		t.Fatalf("empty editor should show the placeholder, got %q", view)
	}
	if got := len(strings.Split(view, "\n")); got != 2 {
		t.Fatalf("placeholder render should honor height, got %d lines: %q", got, view)
	}
	if !strings.Contains(view, "|") {
		t.Fatalf("focused placeholder should still show the cursor, got %q", view)
	}

	// Blurred + empty: placeholder text, no cursor.
	e.Blur()
	if got := e.View(Options{Cursor: "|"}); strings.Contains(got, "|") {
		t.Fatalf("blurred placeholder should not show the cursor, got %q", got)
	}
	e.Focus()

	// Once there is content, the placeholder disappears.
	e.InsertString("x")
	if got := e.View(Options{Cursor: "|"}); strings.Contains(got, "rite your message") {
		t.Fatalf("placeholder should vanish once text is entered, got %q", got)
	}
}

func TestPlaceholderStyledRenderStaysWithinWidth(t *testing.T) {
	var e Model
	e.SetSize(6, 3)
	e.SetPlaceholder("abcdefghijkl") // wider than the editor; must wrap

	view := e.View(Options{
		Cursor:      "|",
		Placeholder: func(s string) string { return "\x1b[2m" + s + "\x1b[0m" },
	})
	lines := strings.Split(view, "\n")
	if len(lines) != 3 {
		t.Fatalf("placeholder should render exactly height lines, got %d: %q", len(lines), view)
	}
	for _, line := range lines {
		if got := ansi.StringWidth(line); got > 6 {
			t.Fatalf("placeholder line display width = %d, want <= 6, line %q", got, line)
		}
	}
}

func TestSanitizesControlCharactersOnInput(t *testing.T) {
	var e Model
	// CRLF and lone CR normalize to \n; ESC/NUL/BEL are dropped; tab → spaces.
	// ESC byte is dropped (neutralizing ANSI injection); the literal "[31m"
	// survives as harmless text.
	e.SetValue("a\r\nb\rc\x1b[31m\x00d\te")
	if got := e.Value(); got != "a\nb\nc[31md    e" {
		t.Fatalf("SetValue sanitized value = %q", got)
	}
	if strings.ContainsRune(e.Value(), '\r') {
		t.Fatalf("value must not retain carriage returns: %q", e.Value())
	}

	// Same via InsertString (paste path).
	var p Model
	p.InsertString("x\r\ny")
	if got := p.Value(); got != "x\ny" {
		t.Fatalf("InsertString sanitized value = %q", got)
	}

	// Rendered output must never contain a raw CR (which would carriage-return
	// the terminal cursor to column 0).
	var r Model
	r.SetSize(20, 3)
	r.SetValue("line one\r\nline two")
	if strings.ContainsRune(r.View(Options{Cursor: "|"}), '\r') {
		t.Fatalf("rendered view must not contain a carriage return")
	}
}

func TestVerticalMovementIsMonotonicAcrossLinesAndWraps(t *testing.T) {
	var e Model
	e.SetSize(10, 10)
	e.SetValue("first\nsecond line\nthird\nfourth wraps because it is long\nlast")
	apply(&e, tea.KeyMsg{Type: tea.KeyCtrlHome})

	// visualPosition must be monotonic over the whole buffer — the caret at a
	// line end must never report as a far-away row (the bug that made up/down
	// jump to the last line and get stuck).
	prevRow := -1
	for i := 0; i <= len([]rune(e.Value())); i++ {
		r, _ := e.visualPosition(i)
		if r < prevRow {
			t.Fatalf("index %d reports row %d, before previous row %d", i, r, prevRow)
		}
		prevRow = r
	}

	// Pressing Down repeatedly must never move the caret to an earlier row.
	apply(&e, tea.KeyMsg{Type: tea.KeyCtrlHome})
	last := -1
	for k := 0; k < 14; k++ {
		r, _ := e.visualPosition(e.CursorIndex())
		if r < last {
			t.Fatalf("Down moved caret up: row %d after row %d", r, last)
		}
		last = r
		apply(&e, tea.KeyMsg{Type: tea.KeyDown})
	}
	if last == 0 {
		t.Fatal("Down never advanced past the first row")
	}

	// Symmetrically, Up must never move to a later row.
	last = 1 << 30
	for k := 0; k < 14; k++ {
		r, _ := e.visualPosition(e.CursorIndex())
		if r > last {
			t.Fatalf("Up moved caret down: row %d after row %d", r, last)
		}
		last = r
		apply(&e, tea.KeyMsg{Type: tea.KeyUp})
	}
}

func TestWrappedVerticalMovementAndViewport(t *testing.T) {
	var e Model
	e.SetSize(5, 2)
	e.SetValue("abcdefghij\nklmno\npqrst")
	apply(&e, tea.KeyMsg{Type: tea.KeyCtrlHome})

	apply(&e, tea.KeyMsg{Type: tea.KeyDown})
	if got := e.CursorIndex(); got != 5 {
		t.Fatalf("cursor after moving down one wrapped row = %d", got)
	}

	apply(&e, tea.KeyMsg{Type: tea.KeyPgDown})
	if top := e.ViewportTop(); top == 0 {
		t.Fatalf("page down should move viewport for a short editor")
	}

	view := e.View(Options{Cursor: "|"})
	if lines := strings.Split(view, "\n"); len(lines) != 2 {
		t.Fatalf("view should render exactly height lines, got %d: %q", len(lines), view)
	}
	if !strings.Contains(view, "|") {
		t.Fatalf("view should render cursor marker, got %q", view)
	}
}
