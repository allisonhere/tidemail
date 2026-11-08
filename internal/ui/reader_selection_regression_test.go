package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func readerSelectionEvent(m Model, action tea.MouseAction, x, y int) Model {
	next, _ := m.Update(tea.MouseMsg{Action: action, Button: tea.MouseButtonLeft, X: x, Y: y})
	return next.(Model)
}

func readerSelectionLines(focus bool, width, height int, lines []string) Model {
	m := readerScrollModel(512, width, height, focus)
	m.contentLines = lines
	m.contentLineCount = len(lines)
	m.viewport.SetContent(strings.Join(lines, "\n"))
	m.viewport.SetYOffset(0)
	return m
}

func TestReaderMouseSelectionTextAndClipboard(t *testing.T) {
	original := clipboardWriteCmd
	defer func() { clipboardWriteCmd = original }()
	copied := ""
	clipboardWriteCmd = func(text string) tea.Cmd { copied = text; return nil }
	for _, focus := range []bool{false, true} {
		for _, geometry := range []struct {
			width        int
			headers      bool
			readingWidth int
		}{{40, false, 18}, {80, true, 30}, {160, false, 60}} {
			t.Run(fmt.Sprintf("focus=%v/width=%d/headers=%v", focus, geometry.width, geometry.headers), func(t *testing.T) {
				m := readerSelectionLines(focus, geometry.width, 35, []string{"abcdef", "", "ghijkl", "\x1b[31m界e\u0301X\x1b[0m", "\x1b]8;;https://example.com\x1b\\linked\x1b]8;;\x1b\\"})
				m.cfg.Display.ShowPaneHeaders = geometry.headers
				m.cfg.Display.ReadingWidth = geometry.readingWidth
				x, y, _, _ := m.readerMouseBounds()
				for _, tc := range []struct {
					name string
					a, b readerTextPoint
					want string
				}{
					{"partial", readerTextPoint{0, 1}, readerTextPoint{0, 4}, "bcd"},
					{"blank-line", readerTextPoint{0, 2}, readerTextPoint{2, 3}, "cdef\n\nghi"},
					{"reverse", readerTextPoint{2, 3}, readerTextPoint{0, 2}, "cdef\n\nghi"},
					{"unicode", readerTextPoint{3, 2}, readerTextPoint{3, 4}, "e\u0301X"},
					{"wide-glyph", readerTextPoint{3, 1}, readerTextPoint{3, 2}, "界"},
					{"osc8", readerTextPoint{4, 1}, readerTextPoint{4, 4}, "ink"},
				} {
					t.Run(tc.name, func(t *testing.T) {
						m = readerSelectionEvent(m, tea.MouseActionPress, x+tc.a.col, y+tc.a.row)
						m = readerSelectionEvent(m, tea.MouseActionMotion, x+tc.b.col, y+tc.b.row)
						m = readerSelectionEvent(m, tea.MouseActionRelease, x+tc.b.col, y+tc.b.row)
						if got := m.readerMouseText(); got != tc.want {
							t.Fatalf("selected %q, want %q", got, tc.want)
						}
						if m.readerMouse.dragging {
							t.Fatal("release retained capture")
						}
						copied = ""
						next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
						m = next.(Model)
						if copied != tc.want || m.readerMouseText() != tc.want {
							t.Fatalf("copy=%q selection=%q", copied, m.readerMouseText())
						}
						if strings.ContainsRune(copied, '\x1b') {
							t.Fatal("clipboard contains ANSI")
						}
					})
				}
				m = readerSelectionEvent(m, tea.MouseActionPress, x, y)
				m = readerSelectionEvent(m, tea.MouseActionRelease, x, y)
				if m.readerMouse.moved || m.readerMouseText() != "" {
					t.Fatal("ordinary click did not clear selection")
				}
				// A drag that returns exactly to its anchor is an empty mouse selection,
				// not permission to fall back to copying the keyboard focus line.
				m = readerSelectionEvent(m, tea.MouseActionPress, x+1, y)
				m = readerSelectionEvent(m, tea.MouseActionMotion, x+3, y)
				m = readerSelectionEvent(m, tea.MouseActionRelease, x+1, y)
				copied = ""
				next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
				m = next.(Model)
				if copied != "" {
					t.Fatalf("empty mouse range copied unrelated text: %q", copied)
				}
			})
		}
	}
}

func TestReaderMouseSelectionWrappedHTML(t *testing.T) {
	for _, focus := range []bool{false, true} {
		m := readerScrollModel(512, 80, 40, focus)
		m.cfg.Display.ReadingWidth = 24
		msg := m.messages[0]
		msg.BodyHTML = "<p>alpha beta gamma delta epsilon zeta eta theta iota kappa lambda</p>"
		m.setViewportMessage(msg)
		row := -1
		for i, line := range m.contentLines {
			if strings.Contains(line, "alpha beta") {
				row = i
				break
			}
		}
		if row < 0 || row+1 >= len(m.contentLines) {
			t.Fatal("missing wrapped paragraph")
		}
		m.viewport.SetYOffset(max(0, row-1))
		x, y, _, _ := m.readerMouseBounds()
		y += row - m.viewport.YOffset
		m = readerSelectionEvent(m, tea.MouseActionPress, x+3, y)
		m = readerSelectionEvent(m, tea.MouseActionMotion, x+6, y+1)
		m = readerSelectionEvent(m, tea.MouseActionRelease, x+6, y+1)
		want := ansi.Cut(m.contentLines[row], 3, ansi.StringWidth(m.contentLines[row])) + "\n" + ansi.Cut(m.contentLines[row+1], 0, 6)
		if got := m.readerMouseText(); got != want || !strings.Contains(got, "\n") {
			t.Fatalf("wrapped copy=%q want=%q", got, want)
		}
	}
}

func TestReaderMouseSelectionCaptureAndLinks(t *testing.T) {
	for _, focus := range []bool{false, true} {
		requests, apply := recordReaderActivations(t)
		m := readerLinksModel(t, "<p>Ordinary <a href=\"https://example.com\">body hyperlink</a> text.</p>", "")
		m.cfg.Display.MouseSupport = true
		m.cfg.Display.FocusLine = focus
		x, y, _, height := m.readerMouseBounds()
		row := m.contentLinkRows[0]
		m.viewport.SetYOffset(max(0, row-2))
		linkY := y + row - m.viewport.YOffset
		m = readerSelectionEvent(m, tea.MouseActionPress, x+3, linkY)
		m = readerSelectionEvent(m, tea.MouseActionRelease, x+3, linkY)
		if m.contentLinkIdx != 0 {
			t.Fatal("complete click did not select LINKS entry")
		}
		if len(*requests) != 0 {
			t.Fatal("click opened a link")
		}
		m = apply(m, readerOpenKey)
		if len(*requests) != 1 {
			t.Fatal("explicit link activation lost")
		}
		m.viewport.SetYOffset(0)
		m = readerSelectionEvent(m, tea.MouseActionPress, x+2, y+1)
		originalCursor, sidebar := m.messageCursor, m.sidebarCursor
		m = readerSelectionEvent(m, tea.MouseActionMotion, 0, 0)
		// A secondary-button release must not end the captured left gesture.
		next, _ := m.Update(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonRight, X: 0, Y: 0})
		m = next.(Model)
		if !m.readerMouse.dragging {
			t.Fatal("secondary release stole capture")
		}
		m = readerSelectionEvent(m, tea.MouseActionMotion, m.width+20, y+height+20)
		m = readerSelectionEvent(m, tea.MouseActionRelease, m.width+20, y+height+20)
		if m.focused != paneContent || m.messageCursor != originalCursor || m.sidebarCursor != sidebar || len(*requests) != 1 {
			t.Fatal("drag escaped reader or activated hyperlink")
		}
		before := m.viewport.YOffset
		next, _ = m.Update(readerWheel(m, false))
		m = next.(Model)
		if m.viewport.YOffset <= before && !m.viewport.AtBottom() {
			t.Fatal("normal wheel scrolling lost")
		}
	}
}

func TestReaderMouseSelectionKeyboardRegression(t *testing.T) {
	originalCopy, originalSave := clipboardWriteCmd, configSave
	defer func() { clipboardWriteCmd = originalCopy; configSave = originalSave }()
	configSave = func(config.Config) error { return nil }
	copied := ""
	clipboardWriteCmd = func(text string) tea.Cmd { copied = text; return nil }
	for _, focus := range []bool{false, true} {
		m := readerSelectionLines(focus, 100, 35, strings.Split(strings.Repeat("alpha\nbeta\ngamma\n", 20), "\n"))
		baseline := m
		baseline.readerPanes = nil
		x, y, _, _ := m.readerMouseBounds()
		m = readerSelectionEvent(m, tea.MouseActionPress, x+1, y)
		m = readerSelectionEvent(m, tea.MouseActionRelease, x+3, y+1)
		for _, event := range []tea.KeyMsg{
			{Type: tea.KeyRunes, Runes: []rune{'v'}}, {Type: tea.KeyDown}, {Type: tea.KeyRunes, Runes: []rune{'y'}},
			{Type: tea.KeyRunes, Runes: []rune{'V'}}, {Type: tea.KeyCtrlC}, {Type: tea.KeyDown}, {Type: tea.KeyUp},
			{Type: tea.KeyShiftRight}, {Type: tea.KeyShiftLeft}, {Type: tea.KeyShiftDown}, {Type: tea.KeyShiftUp},
		} {
			copied = ""
			next, _ := m.Update(event)
			m = next.(Model)
			gotCopy := copied
			copied = ""
			next, _ = baseline.Update(event)
			baseline = next.(Model)
			if gotCopy != copied || m.contentSelectionActive != baseline.contentSelectionActive || m.contentSelectionAll != baseline.contentSelectionAll || m.contentFocusLine != baseline.contentFocusLine || m.viewport.YOffset != baseline.viewport.YOffset || m.cfg.Display.AccountsWidthPercent != baseline.cfg.Display.AccountsWidthPercent || m.cfg.Display.MessagesHeightPercent != baseline.cfg.Display.MessagesHeightPercent {
				t.Fatalf("focus=%v key=%s changed keyboard behavior", focus, event.String())
			}
			if m.readerMouse.moved {
				t.Fatal("keyboard navigation retained mouse selection")
			}
		}
	}
}

func TestReaderMouseSelectionContentCancellation(t *testing.T) {
	for _, replace := range []string{"message", "draft", "thread", "clear", "resize", "overlay", "mouse-disabled"} {
		t.Run(replace, func(t *testing.T) {
			m := readerSelectionLines(false, 100, 35, strings.Split(strings.Repeat("line\n", 50), "\n"))
			x, y, _, h := m.readerMouseBounds()
			m = readerSelectionEvent(m, tea.MouseActionPress, x+1, y+1)
			m = readerSelectionEvent(m, tea.MouseActionMotion, x+2, y+h)
			generation := m.readerMouse.generation
			switch replace {
			case "message":
				m.setViewportMessage(db.Message{ID: 2, BodyText: "replacement"})
			case "draft":
				m.setViewportDraft(db.Draft{ID: 2, BodyText: "replacement"})
			case "thread":
				m.setViewportThread(messageThread{Key: "other", Representative: db.Message{ID: 3, BodyText: "replacement"}})
			case "clear":
				m.clearViewportMessage()
			case "resize":
				next, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
				m = next.(Model)
			case "overlay":
				next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
				m = next.(Model)
			case "mouse-disabled":
				m.cfg.Display.MouseSupport = false
				next, _ := m.Update(tea.MouseMsg{})
				m = next.(Model)
			}
			offset := m.viewport.YOffset
			next, cmd := m.Update(readerSelectionTick(generation))
			m = next.(Model)
			if m.readerMouse.dragging || m.readerMouse.moved || m.readerMouse.pending || m.viewport.YOffset != offset || cmd != nil {
				t.Fatal("replacement did not cancel selection and stale timer")
			}
		})
	}
}

func TestReaderMouseSelectionAutoscrollBoundaries(t *testing.T) {
	for _, focus := range []bool{false, true} {
		m := readerSelectionLines(focus, 100, 35, strings.Split(strings.Repeat("a long displayed line\n", 60), "\n"))
		x, y, _, height := m.readerMouseBounds()
		m = readerSelectionEvent(m, tea.MouseActionPress, x+1, y+1)
		m = readerSelectionEvent(m, tea.MouseActionMotion, x+4, y-1)
		if m.readerMouse.pending {
			t.Fatal("top boundary scheduled timer")
		}
		m = readerSelectionEvent(m, tea.MouseActionMotion, x+4, y+height)
		if !m.readerMouse.pending {
			t.Fatal("bottom autoscroll not scheduled")
		}
		generation := m.readerMouse.generation
		for i := 0; i < 3; i++ {
			before := m.viewport.YOffset
			next, cmd := m.Update(readerSelectionTick(generation))
			m = next.(Model)
			if m.viewport.YOffset != before+1 || cmd == nil || !m.readerMouse.pending {
				t.Fatal("stationary pointer failed to continue scrolling")
			}
			if m.readerMouseScrollCmd() != nil {
				t.Fatal("duplicate timer")
			}
		}
		m = readerSelectionEvent(m, tea.MouseActionMotion, x+4, y-1)
		next, cmd := m.Update(readerSelectionTick(generation))
		m = next.(Model)
		if m.viewport.YOffset != 2 || cmd == nil {
			t.Fatal("autoscroll did not reverse")
		}
		for !m.viewport.AtTop() {
			next, _ = m.Update(readerSelectionTick(generation))
			m = next.(Model)
		}
		if m.readerMouse.pending {
			t.Fatal("top boundary kept timer alive")
		}
		m.viewport.GotoBottom()
		m = readerSelectionEvent(m, tea.MouseActionMotion, x+4, y+height)
		if m.readerMouse.pending {
			t.Fatal("bottom boundary kept timer alive")
		}
		// Re-entering a scrollable direction schedules one timer again.
		m = readerSelectionEvent(m, tea.MouseActionMotion, x+4, y-1)
		if !m.readerMouse.pending {
			t.Fatal("reversal did not restart timer")
		}
		m = readerSelectionEvent(m, tea.MouseActionRelease, x+4, y-1)
		before := m.viewport.YOffset
		next, cmd = m.Update(readerSelectionTick(generation))
		m = next.(Model)
		if cmd != nil || m.readerMouse.pending || m.readerMouse.dragging || m.viewport.YOffset != before {
			t.Fatal("release did not cancel timer")
		}
	}
}
