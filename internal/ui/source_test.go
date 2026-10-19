package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tidemail/internal/config"
)

// sourceModel is a message-list model whose messages can be fetched from a
// server: the account has an IMAP host and every message a UID.
func sourceModel(t *testing.T) Model {
	t.Helper()
	m, _ := newMailboxListModel(7, 3)
	m.cfg.Accounts = []config.AccountConfig{{Name: "Personal", IMAPHost: "mail.example.com"}}
	for i := range m.messages {
		m.messages[i].UID = uint32(100 + i)
	}
	m.applyFilter()
	return m
}

func openSourceOverlay(t *testing.T, m Model) Model {
	t.Helper()
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	m = next.(Model)
	if m.overlay != overlaySource {
		t.Fatalf("ctrl+o did not open the source overlay (overlay %v, status %q)", m.overlay, m.statusMsg)
	}
	if cmd == nil || !m.source.pending {
		t.Fatal("ctrl+o did not start fetching the source")
	}
	return m
}

func TestViewSourceShowsAndCopiesExactBytes(t *testing.T) {
	m := openSourceOverlay(t, sourceModel(t))
	id := m.source.messageID
	if id != m.filteredMessages[m.messageCursor].ID {
		t.Fatalf("source opened for message %d, want the current row", id)
	}

	raw := "From: a@example.com\r\nSubject: hi\r\n\r\nbody \x1b[31mred\x1b[0m\r\n"
	next, _ := m.Update(SourceFetchedMsg{MessageID: id, Raw: []byte(raw)})
	m = next.(Model)

	view := ansi.Strip(m.View())
	if !strings.Contains(view, "Subject: hi") || !strings.Contains(view, "body [31mred[0m") {
		t.Fatalf("source overlay does not show the message (escapes stripped to text):\n%s", view)
	}

	var copied string
	orig := clipboardWriteCmd
	clipboardWriteCmd = func(text string) tea.Cmd { copied = text; return nil }
	t.Cleanup(func() { clipboardWriteCmd = orig })
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = next.(Model)
	if copied != raw {
		t.Fatalf("copied %q, want the exact source %q", copied, raw)
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.overlay != overlayNone || m.source.raw != "" {
		t.Fatal("esc did not close the overlay and drop the source")
	}
}

func TestViewSourceFromReadingPane(t *testing.T) {
	m := sourceModel(t)
	m.focused = paneContent
	m.contentMessageID = m.filteredMessages[1].ID
	m = openSourceOverlay(t, m)
	if m.source.messageID != m.contentMessageID {
		t.Fatalf("source opened for message %d, want the open message %d", m.source.messageID, m.contentMessageID)
	}
}

func TestViewSourceIgnoresStaleFetch(t *testing.T) {
	m := openSourceOverlay(t, sourceModel(t))
	next, _ := m.Update(SourceFetchedMsg{MessageID: m.source.messageID + 1, Raw: []byte("other")})
	m = next.(Model)
	if !m.source.pending || m.source.raw != "" {
		t.Fatal("a fetch for another message replaced the open source")
	}
}

func TestViewSourceNeedsAServer(t *testing.T) {
	m, _ := newMailboxListModel(7, 1) // no IMAP account configured
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	m = next.(Model)
	if m.overlay == overlaySource {
		t.Fatal("source overlay opened for a message with no server to fetch it from")
	}
}

func TestSourceDisplayLines(t *testing.T) {
	got := sourceDisplayLines("A:\tb\r\n\x1b]52;c;evil\x07x\u202ey\r\nabcdefgh\r\n", 4)
	want := []string{"A:  ", "    ", "b", "]52;", "c;ev", "ilxy", "abcd", "efgh"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("sourceDisplayLines = %q, want %q", got, want)
	}
}
