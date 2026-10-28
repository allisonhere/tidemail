package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
)

func assertReaderPanesMatchFresh(t *testing.T, m Model) {
	t.Helper()
	cached := m.View()
	fresh := m
	fresh.readerPanes = nil
	if cached != fresh.View() {
		t.Fatal("cached panes changed the complete rendered screen")
	}
}

func TestReaderPaneCacheNavigationAndLiveStatus(t *testing.T) {
	m := readerScrollModel(1024, 120, 40, true)
	accounts, messages := m.readerPanes.accounts, m.readerPanes.messages
	for _, event := range []tea.Msg{
		tea.KeyMsg{Type: tea.KeyDown}, tea.KeyMsg{Type: tea.KeyUp},
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}},
		tea.MouseMsg{X: m.width - 2, Y: m.mainHeight() - 2, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown},
	} {
		next, _ := m.Update(event)
		m = next.(Model)
		if !m.readerPanes.valid || m.readerPanes.accounts != accounts || m.readerPanes.messages != messages {
			t.Fatal("reader-only movement discarded unchanged panes")
		}
		assertReaderPanesMatchFresh(t, m)
	}
	m.setStatus("live reader status", false)
	if !strings.Contains(m.View(), "live reader status") {
		t.Fatal("status was cached")
	}
	assertReaderPanesMatchFresh(t, m)
}

func TestReaderPaneCacheInvalidation(t *testing.T) {
	for _, name := range []string{"resize", "mail", "error", "theme", "settings", "focus", "list-wheel", "selection"} {
		t.Run(name, func(t *testing.T) {
			m := readerScrollModel(1024, 120, 40, true)
			var event tea.Msg
			switch name {
			case "resize":
				event = tea.WindowSizeMsg{Width: 80, Height: 24}
			case "mail":
				event = MessagesLoadedMsg{MailboxID: 7, Messages: append([]db.Message{arrival(7)}, m.messages...)}
			case "error":
				event = ErrMsg{Err: errors.New("background update")}
			case "theme":
				m.setStyles(BuildStyles(CatppuccinMocha, "comfortable", "round"))
			case "settings":
				// A settings/config change must refresh surrounding pane output.
				m.cfg.Display.ShowSender = !m.cfg.Display.ShowSender
				event = ErrMsg{Err: errors.New("settings refresh")}
			case "focus":
				event = tea.KeyMsg{Type: tea.KeyTab}
			case "list-wheel":
				event = tea.MouseMsg{X: m.width - 2, Y: 3, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown}
			case "selection":
				event = tea.KeyMsg{Type: tea.KeyCtrlN}
			}
			if event != nil {
				next, _ := m.Update(event)
				m = next.(Model)
			}
			if m.readerPanes.valid {
				t.Fatal("state change retained stale reader panes")
			}
			assertReaderPanesMatchFresh(t, m)
		})
	}
}

func TestReaderPaneCacheOverlayTransitions(t *testing.T) {
	m := readerScrollModel(1024, 120, 40, true)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	m = next.(Model)
	if m.overlay == overlayNone || m.readerPanes.valid {
		t.Fatal("opening an overlay retained cached reader panes")
	}
	assertReaderPanesMatchFresh(t, m)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.readerPanes.valid {
		t.Fatal("closing an overlay retained cached reader panes")
	}
	assertReaderPanesMatchFresh(t, m)
}
