package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// openTopAndRead opens the top row in the reader with Mark read on open and
// lets the server confirm the read.
func openTopAndRead(t *testing.T, m Model, entered chan struct{}, release chan error) Model {
	t.Helper()
	m.cfg.Display.MarkReadOnOpen = true
	m.messageCursor = 0
	next, cmd := m.focusPane(paneContent)
	m = next.(Model)
	if m.focused != paneContent || cmd == nil {
		t.Fatal("open did not mark read")
	}
	done := readRemoteResults(cmd)
	awaitReadStore(t, entered)
	release <- nil
	return finishRead(t, m, done)
}

func leaveReader(t *testing.T, m Model) Model {
	t.Helper()
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = settle(t, next.(Model), cmd)
	if m.focused == paneContent {
		t.Fatal("esc did not leave the reader")
	}
	return m
}

func TestHeldReadKeepsUnreadFirstPositionUntilReaderLosesFocus(t *testing.T) {
	for _, threaded := range []bool{false, true} {
		name := "flat"
		if threaded {
			name = "threaded"
		}
		t.Run(name, func(t *testing.T) {
			m, entered, release := readPendingModel(t)
			m.cfg.Display.UnreadFirst = true
			m.cfg.Display.ThreadedConversations = threaded
			// One read message sorts below the unread block.
			if err := m.db.MarkRead(m.messages[2].ID, true); err != nil {
				t.Fatal(err)
			}
			m = settle(t, m, m.loadMailboxMessagesCmd(m.messages[0].MailboxID))
			top, second := m.rowMessage(0).ID, m.rowMessage(1).ID

			m = openTopAndRead(t, m, entered, release)
			if cur := m.currentRowMessage(); m.messageCursor != 0 || cur == nil || cur.ID != top || !cur.Read {
				t.Fatalf("read message moved while open: cursor %d", m.messageCursor)
			}
			if m.contentMessageID != top {
				t.Fatal("reader no longer shows the opened message")
			}

			m = leaveReader(t, m)
			if m.heldReadID != 0 {
				t.Fatal("hold not released")
			}
			if cur := m.currentRowMessage(); m.messageCursor != 0 || cur == nil || cur.ID != second {
				t.Fatal("cursor did not stay on the next unread message")
			}
			if idx := m.indexOfFilteredMessage(top); idx != 1 {
				t.Fatalf("read message at %d, want first in the read block", idx)
			}
		})
	}
}

func TestHeldReadStaysListedInUnreadOnly(t *testing.T) {
	m, entered, release := readPendingModel(t)
	m.showUnreadOnly = true
	m.applyFilter()
	top, second := m.rowMessage(0).ID, m.rowMessage(1).ID
	m = openTopAndRead(t, m, entered, release)
	if m.indexOfFilteredMessage(top) != 0 || m.messageCursor != 0 {
		t.Fatal("read message left the unread-only list while open")
	}
	m = leaveReader(t, m)
	if m.indexOfFilteredMessage(top) >= 0 {
		t.Fatal("read message still listed after leaving the reader")
	}
	if cur := m.currentRowMessage(); cur == nil || cur.ID != second || m.messageCursor != 0 {
		t.Fatal("cursor did not land on the next unread message")
	}
}

func TestHeldReadReleasedWhenAnotherMessageOpens(t *testing.T) {
	m, entered, release := readPendingModel(t)
	m.cfg.Display.UnreadFirst = true
	top := m.rowMessage(0).ID
	m = openTopAndRead(t, m, entered, release)
	m.heldReadID = top
	// Show another message in the reader without leaving it.
	m.messageCursor = 1
	m.setViewportForCurrentRow()
	next, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = settle(t, next.(Model), cmd)
	if m.heldReadID != 0 || m.indexOfFilteredMessage(top) != 2 {
		t.Fatal("previous message kept its unread position")
	}
}

func TestNoHoldWithoutUnreadOrdering(t *testing.T) {
	m, entered, release := readPendingModel(t)
	top := m.rowMessage(0).ID
	m = openTopAndRead(t, m, entered, release)
	if m.heldReadID != 0 {
		t.Fatal("hold set in chronological order")
	}
	m = leaveReader(t, m)
	if cur := m.currentRowMessage(); cur == nil || cur.ID != top {
		t.Fatal("chronological cursor moved")
	}
}
