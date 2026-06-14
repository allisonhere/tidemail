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

func TestInsertDeleteSelectAllAndSelectionText(t *testing.T) {
	var e Model

	ch := e.UpdateKey(keyRunes("hello"))
	if !ch.Content {
		t.Fatal("typing should report a content change")
	}
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
	e.UpdateKey(tea.KeyMsg{Type: tea.KeyBackspace})
	e.UpdateKey(tea.KeyMsg{Type: tea.KeyDelete})
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
	e.UpdateKey(tea.KeyMsg{Type: tea.KeyCtrlHome})
	for range len("hello") {
		e.UpdateKey(tea.KeyMsg{Type: tea.KeyShiftRight})
	}

	ch := e.UpdateKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("goodbye"), Paste: true})

	if !ch.Content || !ch.Selection {
		t.Fatalf("paste over selection should report content and selection changes: %+v", ch)
	}
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
	e.UpdateKey(tea.KeyMsg{Type: tea.KeyCtrlEnd})
	for range len(" gamma") {
		e.UpdateKey(tea.KeyMsg{Type: tea.KeyShiftLeft})
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

	e.UpdateKey(tea.KeyMsg{Type: tea.KeyCtrlHome})
	e.UpdateKey(tea.KeyMsg{Type: tea.KeyShiftDown})
	e.UpdateKey(tea.KeyMsg{Type: tea.KeyShiftRight})

	if got := e.SelectedText(); got != "first\ns" {
		t.Fatalf("selected text after shift movement = %q", got)
	}

	ch := e.UpdateKey(tea.KeyMsg{Type: tea.KeyEnd})
	if !ch.Selection || !ch.Cursor {
		t.Fatalf("clearing selection with movement should report selection and cursor change: %+v", ch)
	}
	if got := e.SelectedText(); got != "" {
		t.Fatalf("selection should clear after plain movement, got %q", got)
	}
}

func TestViewMarksSelectedText(t *testing.T) {
	var e Model
	e.SetSize(20, 2)
	e.SetValue("select me")
	e.UpdateKey(tea.KeyMsg{Type: tea.KeyCtrlHome})
	for range len("select") {
		e.UpdateKey(tea.KeyMsg{Type: tea.KeyShiftRight})
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
	e.UpdateKey(tea.KeyMsg{Type: tea.KeyCtrlHome})

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
	e.UpdateKey(tea.KeyMsg{Type: tea.KeyCtrlHome})

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
	e.UpdateKey(tea.KeyMsg{Type: tea.KeyCtrlHome})

	e.UpdateKey(tea.KeyMsg{Type: tea.KeyCtrlRight})
	if got := e.CursorIndex(); got != 3 {
		t.Fatalf("ctrl+right should land after first word, cursor = %d", got)
	}
	e.UpdateKey(tea.KeyMsg{Type: tea.KeyCtrlRight})
	if got := e.CursorIndex(); got != 7 {
		t.Fatalf("ctrl+right should land after second word, cursor = %d", got)
	}
	e.UpdateKey(tea.KeyMsg{Type: tea.KeyCtrlLeft})
	if got := e.CursorIndex(); got != 4 {
		t.Fatalf("ctrl+left should land at start of word, cursor = %d", got)
	}
}

func TestWordSelectionExtendsSelection(t *testing.T) {
	var e Model
	e.SetValue("foo bar baz")
	e.UpdateKey(tea.KeyMsg{Type: tea.KeyCtrlHome})

	ch := e.UpdateKey(tea.KeyMsg{Type: tea.KeyCtrlShiftRight})
	if !ch.Selection {
		t.Fatalf("ctrl+shift+right should report a selection change: %+v", ch)
	}
	if got := e.SelectedText(); got != "foo" {
		t.Fatalf("ctrl+shift+right should select first word, got %q", got)
	}
	e.UpdateKey(tea.KeyMsg{Type: tea.KeyCtrlShiftRight})
	if got := e.SelectedText(); got != "foo bar" {
		t.Fatalf("ctrl+shift+right should extend to second word, got %q", got)
	}
}

func TestBackspaceAndDeleteRemoveSelection(t *testing.T) {
	selectABC := func() Model {
		var e Model
		e.SetValue("abcdef")
		e.UpdateKey(tea.KeyMsg{Type: tea.KeyCtrlHome})
		for range 3 {
			e.UpdateKey(tea.KeyMsg{Type: tea.KeyShiftRight})
		}
		return e
	}

	bs := selectABC()
	bs.UpdateKey(tea.KeyMsg{Type: tea.KeyBackspace})
	if got := bs.Value(); got != "def" {
		t.Fatalf("backspace over selection = %q, want %q", got, "def")
	}

	del := selectABC()
	del.UpdateKey(tea.KeyMsg{Type: tea.KeyDelete})
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
	e.UpdateKey(tea.KeyMsg{Type: tea.KeyCtrlEnd})

	// Cursor sits on the second visual row; Home must jump to the logical
	// line start (index 0), not the wrapped row start (index 5).
	e.UpdateKey(tea.KeyMsg{Type: tea.KeyHome})
	if got := e.CursorIndex(); got != 0 {
		t.Fatalf("home should go to logical line start, cursor = %d", got)
	}
	e.UpdateKey(tea.KeyMsg{Type: tea.KeyEnd})
	if got := e.CursorIndex(); got != 10 {
		t.Fatalf("end should go to logical line end, cursor = %d", got)
	}
}

func TestViewportStableAtDocumentEdges(t *testing.T) {
	var e Model
	e.SetSize(4, 2)
	e.SetValue("a\nb\nc\nd\ne\nf") // six single-rune visual lines

	e.UpdateKey(tea.KeyMsg{Type: tea.KeyCtrlEnd})
	for range 3 { // over-page past the end
		e.UpdateKey(tea.KeyMsg{Type: tea.KeyPgDown})
	}
	if got := e.ViewportTop(); got != 4 { // 6 lines - height 2
		t.Fatalf("viewport should clamp to last page at end, top = %d", got)
	}

	e.UpdateKey(tea.KeyMsg{Type: tea.KeyCtrlHome})
	for range 3 { // over-page past the start
		e.UpdateKey(tea.KeyMsg{Type: tea.KeyPgUp})
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
	e.UpdateKey(tea.KeyMsg{Type: tea.KeyCtrlHome})
	for range 3 { // move past "abc"
		e.UpdateKey(tea.KeyMsg{Type: tea.KeyRight})
	}
	for range 2 { // select "de", leaving the cursor past the selection
		e.UpdateKey(tea.KeyMsg{Type: tea.KeyShiftRight})
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
		e.UpdateKey(keyRunes(string(r)))
	}
	e.UpdateKey(tea.KeyMsg{Type: tea.KeySpace})
	for _, r := range "bar" {
		e.UpdateKey(keyRunes(string(r)))
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
	e.UpdateKey(keyRunes("a"))
	e.UpdateKey(keyRunes("b")) // coalesces with "a" into one unit

	e.Undo()
	if got := e.Value(); got != "" {
		t.Fatalf("value after undo = %q", got)
	}
	e.UpdateKey(keyRunes("c"))
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
	e.UpdateKey(tea.KeyMsg{Type: tea.KeyBackspace}) // delete the selection

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
	ch := e.UpdateKey(tea.KeyMsg{Type: tea.KeyCtrlZ})
	if ch.Content {
		t.Fatalf("ctrl+z on a reset document should not change content: %+v", ch)
	}
	if got := e.Value(); got != "draft" {
		t.Fatalf("value should be unchanged, got %q", got)
	}
}

func TestUndoRedoViaKeys(t *testing.T) {
	var e Model
	e.UpdateKey(keyRunes("x"))

	ch := e.UpdateKey(tea.KeyMsg{Type: tea.KeyCtrlZ})
	if !ch.Content || e.Value() != "" {
		t.Fatalf("ctrl+z should undo: change=%+v value=%q", ch, e.Value())
	}
	ch = e.UpdateKey(tea.KeyMsg{Type: tea.KeyCtrlY})
	if !ch.Content || e.Value() != "x" {
		t.Fatalf("ctrl+y should redo: change=%+v value=%q", ch, e.Value())
	}
}

func TestWrappedVerticalMovementAndViewport(t *testing.T) {
	var e Model
	e.SetSize(5, 2)
	e.SetValue("abcdefghij\nklmno\npqrst")
	e.UpdateKey(tea.KeyMsg{Type: tea.KeyCtrlHome})

	e.UpdateKey(tea.KeyMsg{Type: tea.KeyDown})
	if got := e.CursorIndex(); got != 5 {
		t.Fatalf("cursor after moving down one wrapped row = %d", got)
	}

	e.UpdateKey(tea.KeyMsg{Type: tea.KeyPgDown})
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
