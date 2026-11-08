package ui

import (
	"errors"
	"fmt"
	"github.com/allisonhere/tidemail/internal/db"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/cellbuf"
	"github.com/muesli/termenv"
)

func assertSelectionScreen(t *testing.T, m Model) {
	t.Helper()
	got := m.View()
	fresh := m
	fresh.readerMouse.view = nil
	fresh.readerPanes = nil
	want := fresh.View()
	a, b := cellbuf.NewBuffer(m.width, m.height), cellbuf.NewBuffer(m.width, m.height)
	cellbuf.SetContent(a, got)
	cellbuf.SetContent(b, want)
	for y := 0; y < m.height; y++ {
		for x := 0; x < m.width; x++ {
			if !reflect.DeepEqual(a.Cell(x, y), b.Cell(x, y)) {
				t.Fatalf("screen differs at %d,%d: got=%+v want=%+v", x, y, a.Cell(x, y), b.Cell(x, y))
			}
		}
	}
}

func TestReaderSelectionViewMatchesNormal(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	for _, focus := range []bool{false, true} {
		for _, transparent := range []bool{false, true} {
			for _, headers := range []bool{false, true} {
				t.Run(fmt.Sprintf("focus=%v/transparent=%v/headers=%v", focus, transparent, headers), func(t *testing.T) {
					m := readerScrollModel(1024, 100, 35, focus)
					m.cfg.Display.Transparent = transparent
					m.cfg.Display.ShowPaneHeaders = headers
					m.readerPanes.valid = false
					m.contentLines = []string{"\x1b[31mStyled 界 e\u0301 text\x1b[0m", "\x1b]8;;https://example.com\x1b\\linked text\x1b]8;;\x1b\\", "third line", strings.Repeat("next line\n", 30)}
					// Keep viewport and indexed lines consistent, including short-line padding.
					m.contentLines = strings.Split(strings.Join(m.contentLines, "\n"), "\n")
					m.contentLineCount = len(m.contentLines)
					m.contentSearchMatches = []int{1}
					m.contentSearchIdx = 0
					m.viewport.SetContent(strings.Join(m.contentLines, "\n"))
					m.viewport.SetYOffset(0)
					x, y, _, height := m.readerMouseBounds()
					events := []tea.Msg{
						tea.MouseMsg{X: x + 2, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft},
						tea.MouseMsg{X: x + 9, Y: y + 2, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft},
						tea.MouseMsg{X: x + 4, Y: y, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft},
						tea.MouseMsg{X: x + 7, Y: y + height, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft},
					}
					for _, event := range events {
						next, _ := m.Update(event)
						m = next.(Model)
						assertSelectionScreen(t, m)
					}
					next, _ := m.Update(readerSelectionTick(m.readerMouse.generation))
					m = next.(Model)
					assertSelectionScreen(t, m)
					next, _ = m.Update(tea.MouseMsg{X: x + 7, Y: y + height, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
					m = next.(Model)
					assertSelectionScreen(t, m)
					next, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
					m = next.(Model)
					assertSelectionScreen(t, m)
				})
			}
		}
	}
}

func TestReaderSelectionViewInvalidation(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	m := readerScrollModel(4096, 100, 35, true)
	x, y, _, _ := m.readerMouseBounds()
	for _, event := range []tea.Msg{
		tea.MouseMsg{X: x + 2, Y: y + 3, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft},
		tea.MouseMsg{X: x + 10, Y: y + 5, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft},
	} {
		next, _ := m.Update(event)
		m = next.(Model)
	}
	assertSelectionScreen(t, m)
	cached := m.readerMouse.view
	for _, event := range []tea.Msg{
		ErrMsg{Err: fmt.Errorf("background refresh")},
		tea.MouseMsg{X: x + 12, Y: y + 6, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft},
		tea.MouseMsg{X: x + 12, Y: y + 6, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft},
		tea.KeyMsg{Type: tea.KeyCtrlC},
		tea.WindowSizeMsg{Width: 80, Height: 24},
	} {
		next, _ := m.Update(event)
		m = next.(Model)
		if _, ok := event.(ErrMsg); ok && cached.valid {
			t.Fatal("async update kept stale template")
		}
		assertSelectionScreen(t, m)
	}
	if m.readerMouse.view != nil {
		t.Fatal("resize retained template")
	}
}

func TestReaderSelectionViewDisplayChanges(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	for _, tc := range []struct {
		name   string
		change func(*Model)
	}{
		{"new-messages", func(m *Model) {
			next, _ := m.Update(MessagesLoadedMsg{MailboxID: 7, Messages: append([]db.Message{arrival(7)}, m.messages...)})
			*m = next.(Model)
		}},
		{"sync", func(m *Model) { next, _ := m.Update(MailboxSyncedMsg{MailboxID: 7, NewCount: 1}); *m = next.(Model) }},
		{"sidebar-unread", func(m *Model) {
			boxes := append([]db.Mailbox(nil), m.mailboxes...)
			boxes[0].UnreadCount = 37
			next, _ := m.Update(AccountsLoadedMsg{Accounts: m.accounts, Mailboxes: boxes})
			*m = next.(Model)
		}},
		{"message-list", func(m *Model) {
			messages := append([]db.Message(nil), m.messages...)
			messages[1].Subject = "updated list subject"
			next, _ := m.Update(MessagesLoadedMsg{MailboxID: 7, Messages: messages})
			*m = next.(Model)
		}},
		{"status-setter", func(m *Model) { m.setStatus("fresh drag status", false) }},
		{"status-event", func(m *Model) { next, _ := m.Update(ErrMsg{Err: errors.New("fresh error")}); *m = next.(Model) }},
		{"scroll", func(m *Model) { m.viewport.SetYOffset(1); m.reconcileContentFocus(1) }},
		{"message-switch", func(m *Model) {
			m.focused = paneMessages
			next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
			*m = next.(Model)
		}},
		{"resize-reflow", func(m *Model) { next, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 24}); *m = next.(Model) }},
		{"theme", func(m *Model) { m.setStyles(BuildStyles(BuiltinThemes[1], "compact", "square")) }},
		{"display-settings", func(m *Model) {
			m.cfg.Display.ShowSender = !m.cfg.Display.ShowSender
			m.cfg.Display.FocusLine = !m.cfg.Display.FocusLine
			next, _ := m.Update(ErrMsg{Err: errors.New("settings refreshed")})
			*m = next.(Model)
		}},
		{"focus", func(m *Model) { next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab}); *m = next.(Model) }},
		{"overlay-open-close", func(m *Model) {
			next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
			*m = next.(Model)
			next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
			*m = next.(Model)
		}},
		{"search", func(m *Model) {
			next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
			*m = next.(Model)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := readerScrollModel(4096, 100, 35, true)
			m.firstLoad = false
			x, y, _, _ := m.readerMouseBounds()
			m = readerSelectionEvent(m, tea.MouseActionPress, x+2, y+1)
			m = readerSelectionEvent(m, tea.MouseActionMotion, x+10, y+3)
			assertSelectionScreen(t, m)
			tc.change(&m)
			assertSelectionScreen(t, m)
			// Continue an active gesture after the display change, rather than testing
			// only the first invalidated frame.
			if m.readerMouse.dragging {
				m = readerSelectionEvent(m, tea.MouseActionMotion, x+12, y+4)
				assertSelectionScreen(t, m)
			}
		})
	}
}

func TestReaderSelectionViewReusesUnchangedTemplate(t *testing.T) {
	m := readerScrollModel(4096, 100, 35, true)
	x, y, _, _ := m.readerMouseBounds()
	m = readerSelectionEvent(m, tea.MouseActionPress, x+2, y+1)
	_ = m.View()
	cache := m.readerMouse.view
	template := &cache.lines[0]
	for _, action := range []tea.MouseAction{tea.MouseActionMotion, tea.MouseActionMotion, tea.MouseActionRelease} {
		m = readerSelectionEvent(m, action, x+8, y+3)
		_ = m.View()
		if !cache.valid || template != &cache.lines[0] {
			t.Fatal("selection-only event rebuilt screen template")
		}
	}
}

func TestReaderSelectionViewGeometryAndPlaceholders(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	for _, focus := range []bool{false, true} {
		for _, width := range []int{30, 80, 160} {
			m := readerSelectionLines(focus, width, 35, []string{"\x1b[31m界e\u0301 styled\x1b[0m", string(placeholderRune), "\x1b]8;;https://example.com\x1b\\linked\x1b]8;;\x1b\\", "last"})
			m.cfg.Display.ReadingWidth = 18
			x, y, w, _ := m.readerMouseBounds()
			m = readerSelectionEvent(m, tea.MouseActionPress, x, y)
			m = readerSelectionEvent(m, tea.MouseActionMotion, x+w-1, y+3)
			assertSelectionScreen(t, m)
			m = readerSelectionEvent(m, tea.MouseActionRelease, x+w-1, y+3)
			assertSelectionScreen(t, m)
		}
	}
}
