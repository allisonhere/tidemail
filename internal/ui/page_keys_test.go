package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func pagePress(m Model, k tea.KeyMsg) Model {
	next, _ := m.Update(k)
	return next.(Model)
}

var (
	keyPgDn   = tea.KeyMsg{Type: tea.KeyPgDown}
	keyPgUp   = tea.KeyMsg{Type: tea.KeyPgUp}
	keyShiftJ = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("J")}
	keyShiftK = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("K")}
)

func TestPageKeysMoveMessageListByAScreen(t *testing.T) {
	for name, keys := range map[string][2]tea.KeyMsg{
		"pgup/pgdn": {keyPgDn, keyPgUp},
		"shift+j/k": {keyShiftJ, keyShiftK},
	} {
		t.Run(name, func(t *testing.T) {
			m, _ := newMailboxListModel(7, 80)
			m.focused = paneMessages
			step := max(1, m.articleRowsVisible()-1)
			if step < 2 {
				t.Fatalf("test needs a list taller than one row, step=%d", step)
			}
			m = pagePress(m, keys[0])
			if m.messageCursor != step {
				t.Fatalf("down: cursor = %d, want %d", m.messageCursor, step)
			}
			if m.messageCursor < m.listOffset || m.messageCursor >= m.listOffset+m.articleRowsVisible() {
				t.Fatalf("cursor %d scrolled out of view (offset %d)", m.messageCursor, m.listOffset)
			}
			m = pagePress(m, keys[1])
			if m.messageCursor != 0 {
				t.Fatalf("up: cursor = %d, want 0", m.messageCursor)
			}
			m = pagePress(m, keys[1]) // already at the top
			if m.messageCursor != 0 {
				t.Fatalf("up at top moved the cursor to %d", m.messageCursor)
			}
		})
	}
}

func TestPageDownClampsToTheLastRow(t *testing.T) {
	m, _ := newMailboxListModel(7, 12)
	m.focused = paneMessages
	// Stop at the last row: a further page down behaves like Down there and
	// asks the database for older mail, which this DB-less model does not have.
	for i := 0; i < 10 && m.messageCursor < 11; i++ {
		m = pagePress(m, keyPgDn)
		if m.messageCursor > 11 {
			t.Fatalf("cursor %d ran past the last row", m.messageCursor)
		}
	}
	if m.messageCursor != 11 {
		t.Fatalf("cursor = %d, want the last row (11)", m.messageCursor)
	}
}

// The sidebar keeps Shift+J/K for reordering and has no paging.
func TestPageKeysDoNothingInTheSidebar(t *testing.T) {
	m, _ := newMailboxListModel(7, 40)
	m.focused = paneAccounts
	m.sidebarCursor = 0 // the account row, where Shift+J has no folder to move
	for _, k := range []tea.KeyMsg{keyPgDn, keyPgUp, keyShiftJ, keyShiftK} {
		after := pagePress(m, k)
		if after.messageCursor != 0 || after.sidebarCursor != 0 {
			t.Fatalf("%q moved message cursor to %d, sidebar to %d", k.String(), after.messageCursor, after.sidebarCursor)
		}
	}
}

func pageContentModel(t *testing.T, focusLine bool) Model {
	t.Helper()
	m := newFocusLineModel(t)
	m.cfg.Display.FocusLine = focusLine
	var lines []string
	for i := 0; i < 200; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	m.viewport.Height = 20
	m.viewport.SetContent(strings.Join(lines, "\n"))
	m.contentLineCount = len(lines)
	m.focused = paneContent
	return m
}

func TestPageKeysScrollTheContentViewport(t *testing.T) {
	m := pageContentModel(t, false)
	m = pagePress(m, keyPgDn)
	if got := m.viewport.YOffset; got != 19 {
		t.Fatalf("pgdn offset = %d, want 19 (a screen minus one line of overlap)", got)
	}
	m = pagePress(m, keyShiftJ)
	if got := m.viewport.YOffset; got != 38 {
		t.Fatalf("shift+j offset = %d, want 38", got)
	}
	m = pagePress(m, keyShiftK)
	m = pagePress(m, keyPgUp)
	if got := m.viewport.YOffset; got != 0 {
		t.Fatalf("paging back up left offset %d, want 0", got)
	}
}

func TestPageKeysMoveTheFocusLine(t *testing.T) {
	m := pageContentModel(t, true)
	m.contentFocusable = make([]bool, m.contentLineCount)
	for i := range m.contentFocusable {
		m.contentFocusable[i] = true
	}
	m.contentFocusLine = 0
	m = pagePress(m, keyPgDn)
	if m.contentFocusLine != 19 {
		t.Fatalf("focus line = %d, want 19", m.contentFocusLine)
	}
	m = pagePress(m, keyPgUp)
	if m.contentFocusLine != 0 {
		t.Fatalf("focus line = %d after paging back, want 0", m.contentFocusLine)
	}
}
