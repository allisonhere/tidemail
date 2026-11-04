package ui

import (
	"fmt"
	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestShortTerminalNavigationAndResizeKeepCursorInList(t *testing.T) {
	for _, space := range []bool{false, true} {
		t.Run(fmt.Sprint(space), func(t *testing.T) {
			m := cacheTestModel(t)
			m.styles.Density = "comfortable"
			m.cfg.Display.ThreadedConversations = false
			m.cfg.Display.MarkReadOnFocus = false
			m.focused = paneMessages
			m.messages = nil
			for i := 1; i <= 20; i++ {
				m.messages = append(m.messages, db.Message{ID: int64(i), UID: uint32(i), Subject: fmt.Sprint(i)})
			}
			m.filteredMessages = m.messages
			next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 3})
			m = next.(Model)
			if m.articleRowsVisible() != 0 {
				t.Fatal("fixture must have zero complete visible rows")
			}
			key := tea.KeyMsg{Type: tea.KeyDown}
			if space {
				key = tea.KeyMsg{Type: tea.KeySpace}
			}
			for range 4 {
				next, _ = m.Update(key)
				m = next.(Model)
				if m.listOffset > m.messageCursor {
					t.Fatalf("offset %d passed cursor %d", m.listOffset, m.messageCursor)
				}
			}
			if m.messageCursor != 4 {
				t.Fatalf("cursor did not advance: %d", m.messageCursor)
			}
			// Also recover an offset saved by an older version before resizing.
			m.listOffset = m.messageCursor + 1
			next, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
			m = next.(Model)
			if m.listOffset > m.messageCursor || m.messageCursor >= m.listOffset+max(1, m.articleRowsVisible()) {
				t.Fatal("resize left cursor outside list viewport")
			}
		})
	}
}

func TestDraftResizeUsesDraftCount(t *testing.T) {
	m := draftsPreviewModel(t)
	m.messageCursor = 1
	m.listOffset = 10
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = next.(Model)
	if m.messageCursor != 1 || m.listOffset > 1 {
		t.Fatalf("draft viewport reset using hidden messages: cursor=%d offset=%d", m.messageCursor, m.listOffset)
	}
}

func TestSingleAccountComposeAcceptsTypingImmediately(t *testing.T) {
	for _, accounts := range [][]config.AccountConfig{nil, {{Name: "Only", User: "me@example.com"}}} {
		c := NewCompose(config.AccountConfig{Name: "Only", User: "me@example.com"}, accounts, nil)
		if c.focusedField != composeFieldTo {
			t.Fatalf("initial focus %v", c.focusedField)
		}
		c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("to@example.com")}, DefaultKeys)
		if c.toInput.Value() != "to@example.com" {
			t.Fatalf("initial typing lost: %q", c.toInput.Value())
		}
	}
}

func TestIndentedMessageBodyKeepsEveryCharacterAtFullWidth(t *testing.T) {
	m := cacheTestModel(t)
	m.styles.PlainUI = true
	m.cfg.Display.FilterLinks = false
	m.cfg.Display.ReadingWidth = 0
	for _, width := range []int{1, 12, 40, m.contentBodyWidth()} {
		payloads := []string{strings.Repeat("abcdefghij", 12), "https://example.com/" + strings.Repeat("abcdef", 25)}
		if width > 1 {
			payloads = append(payloads, strings.Repeat("日本語", 40))
		}
		for _, payload := range payloads {
			result := m.renderMessageForDisplay(db.Message{BodyText: payload}, width)
			lines := strings.Split(result.body, "\n")
			view := clampView(result.body, width, len(lines), m.styles.Theme.Bg)
			got := strings.Join(strings.Fields(ansi.Strip(view)), "")
			if got != payload {
				t.Fatalf("width=%d clipped body: got %q want %q", width, got, payload)
			}
		}
	}
}
