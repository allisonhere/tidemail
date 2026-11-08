package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestReaderMouseSelection(t *testing.T) {
	for _, focus := range []bool{false, true} {
		m := readerScrollModel(1024, 100, 35, focus)
		m.contentLines = []string{"abcdef", "ghijkl", "mnopqr"}
		m.contentLineCount = 3
		m.viewport.SetContent("abcdef\nghijkl\nmnopqr")
		m.viewport.SetYOffset(0)
		x, y, _, _ := m.readerMouseBounds()
		event := func(action tea.MouseAction, x, y int) {
			next, _ := m.Update(tea.MouseMsg{Action: action, Button: tea.MouseButtonLeft, X: x, Y: y})
			m = next.(Model)
		}
		event(tea.MouseActionPress, x+2, y)
		event(tea.MouseActionMotion, x+3, y+1)
		event(tea.MouseActionRelease, x+3, y+1)
		if got := m.readerMouseText(); got != "cdef\nghi" {
			t.Fatalf("focus=%v copy=%q", focus, got)
		}
		next, copyCmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
		m = next.(Model)
		if copyCmd == nil || m.readerMouseText() != "cdef\nghi" {
			t.Fatal("ctrl+c lost selection or copy command")
		}
		if m.readerMouse.dragging {
			t.Fatal("release retained drag")
		}
		event(tea.MouseActionPress, x+3, y+1)
		event(tea.MouseActionMotion, x+2, y)
		event(tea.MouseActionRelease, x+2, y)
		if got := m.readerMouseText(); got != "cdef\nghi" {
			t.Fatalf("backward copy=%q", got)
		}
		event(tea.MouseActionPress, x+1, y)
		event(tea.MouseActionRelease, x+1, y)
		if m.readerMouseText() != "" {
			t.Fatal("click retained selection")
		}
		event(tea.MouseActionPress, x+1, y)
		event(tea.MouseActionMotion, 0, 0)
		generation := m.readerMouse.generation
		if m.readerMouse.pending {
			t.Fatal("autoscroll scheduled at top boundary")
		}
		if cmd := m.readerMouseScrollCmd(); cmd != nil {
			t.Fatal("duplicate autoscroll")
		}
		event(tea.MouseActionRelease, 0, 0)
		next, cmd := m.Update(readerSelectionTick(generation))
		m = next.(Model)
		if cmd != nil || m.readerMouse.dragging {
			t.Fatal("stale tick restarted drag")
		}
		if m.focused != paneContent {
			t.Fatal("drag escaped reader")
		}
	}
}

func TestReaderMouseSelectionAutoscroll(t *testing.T) {
	m := readerScrollModel(8192, 100, 35, false)
	x, y, _, height := m.readerMouseBounds()
	m.readerSelectionMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x + 2, Y: y + 1})
	_, cmd := m.readerSelectionMouse(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: x + 4, Y: y + height})
	if cmd == nil {
		t.Fatal("missing tick")
	}
	generation := m.readerMouse.generation
	next, cmd := m.handleReaderSelectionTick(readerSelectionTick(generation))
	m = next.(Model)
	if m.viewport.YOffset != 1 || cmd == nil {
		t.Fatal("tick did not scroll and continue")
	}
	if m.readerMouse.end.row != m.viewport.YOffset+height-1 {
		t.Fatal("endpoint did not follow viewport")
	}
	m.readerSelectionMouse(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: x + 4, Y: y + height})
	if m.readerMouseText() == "" {
		t.Fatal("autoscroll selection empty")
	}
}
