package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestRenderMessageContentAddsBlankLineAfterMessageID(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.width = 100
	m.contentShowHeaders = true

	view := ansi.Strip(m.renderMessageContent(db.Message{
		Subject:   "Hello",
		From:      "alice@example.com",
		To:        "bob@example.com",
		Date:      time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		MessageID: "<message-id@example.com>",
		BodyText:  "Body starts here.",
	}))

	messageIDLine := -1
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		if strings.Contains(line, "Message-ID:") {
			messageIDLine = i
			break
		}
	}
	if messageIDLine < 0 {
		t.Fatalf("expected Message-ID header in rendered content, got %q", view)
	}
	if messageIDLine+1 >= len(lines) || strings.TrimSpace(lines[messageIDLine+1]) != "" {
		t.Fatalf("expected blank line after Message-ID, got next line %q in %q", lines[messageIDLine+1], view)
	}
}

func TestRenderMessageTitleSpansPaneWhenReadingWidthIsCapped(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Display.ReadingWidth = 32
	m := NewModel(nil, cfg, "dev", false)
	m.width = 100

	view := m.renderMessageContent(db.Message{
		Subject:  "A message with a title bar",
		Date:     time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		BodyText: "Body starts here.",
	})
	firstLine := strings.Split(view, "\n")[0]

	if got, want := ansi.StringWidth(firstLine), m.articlesPaneWidth(); got != want {
		t.Fatalf("expected title line to span content pane width %d, got %d in %q", want, got, ansi.Strip(firstLine))
	}
}

func TestRenderHTMLBodyPreservesTablesAsReadableRows(t *testing.T) {
	got := ansi.Strip(renderHTMLBody(`<table><tr><th>Name</th><th>Status</th></tr><tr><td>Ada</td><td>Done</td></tr></table>`, 80, CatppuccinMocha, true))

	if !strings.Contains(got, "Name") || !strings.Contains(got, "Status") || !strings.Contains(got, "Ada") || !strings.Contains(got, "Done") {
		t.Fatalf("expected table cell text in rendered body, got %q", got)
	}
	if !strings.Contains(got, "Name | Status") || !strings.Contains(got, "Ada  | Done") {
		t.Fatalf("expected table to render as aligned readable rows, got %q", got)
	}
}

func TestRenderHTMLBodyKeepsLooseListItemContinuation(t *testing.T) {
	got := ansi.Strip(renderHTMLBody(`<ul><li><p>First line.</p><p>Second line.</p></li></ul>`, 80, CatppuccinMocha, true))

	if !strings.Contains(got, "First line.") || !strings.Contains(got, "Second line.") {
		t.Fatalf("expected loose list item continuation text, got %q", got)
	}
}

func TestContentFocusKeepsThreePaneLayout(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.width = 100
	m.height = 24
	m.viewport.Width = m.contentBodyWidth()
	m.viewport.Height = m.contentBodyHeight()
	m.focused = paneContent
	m.accounts = []db.Account{{ID: 1, Name: "Work Account"}}
	m.sidebarRows = []sidebarRow{{kind: rowKindAccount, accountID: 1}}
	m.filteredMessages = []db.Message{{
		ID:       1,
		Subject:  "Inbox List Subject",
		From:     "alice@example.com",
		Date:     time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		BodyText: "Only this message text should be selectable.",
	}}
	m.setViewportMessage(m.filteredMessages[0])

	view := ansi.Strip(m.View())
	for _, want := range []string{"Accounts", "Messages", "Work Account", "Only this message text should be selectable.", "Inbox List Subject"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected content focus to preserve three-pane text %q, got %q", want, view)
		}
	}
}

func newContentSelectionModel(t *testing.T) Model {
	t.Helper()
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.width = 100
	m.height = 24
	m.viewport.Width = m.contentBodyWidth()
	m.viewport.Height = m.contentBodyHeight()
	m.focused = paneContent
	m.filteredMessages = []db.Message{{
		ID:       1,
		Subject:  "Selectable Subject",
		From:     "alice@example.com",
		Date:     time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		BodyText: "alpha\n\nbeta\n\ngamma",
	}}
	m.setViewportMessage(m.filteredMessages[0])
	return m
}

func TestVisualSelectionYanksOnlyContentLines(t *testing.T) {
	m := newContentSelectionModel(t)
	m.contentFocusLine = indexContentLine(t, m, "alpha")
	var copied string
	restore := stubClipboardWrite(t, &copied)
	defer restore()

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	m = next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = next.(Model)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("expected yank to return clipboard command")
	}
	if msg := cmd(); msg != (ClipboardCopiedMsg{}) {
		t.Fatalf("expected successful clipboard message, got %#v", msg)
	}

	if !strings.Contains(copied, "alpha") || !strings.Contains(copied, "beta") {
		t.Fatalf("expected copied range to include selected content, got %q", copied)
	}
	for _, unwanted := range []string{"Accounts", "Messages"} {
		if strings.Contains(copied, unwanted) {
			t.Fatalf("expected copied text to exclude side-pane text %q, got %q", unwanted, copied)
		}
	}
	if m.contentSelectionActive {
		t.Fatal("expected yank to clear visual selection")
	}
}

func TestVisualLineYanksFullRenderedMessage(t *testing.T) {
	m := newContentSelectionModel(t)
	var copied string
	restore := stubClipboardWrite(t, &copied)
	defer restore()

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'V'}})
	m = next.(Model)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("expected yank to return clipboard command")
	}
	if msg := cmd(); msg != (ClipboardCopiedMsg{}) {
		t.Fatalf("expected successful clipboard message, got %#v", msg)
	}

	for _, want := range []string{"Selectable Subject", "alpha", "beta", "gamma"} {
		if !strings.Contains(copied, want) {
			t.Fatalf("expected full copied message to include %q, got %q", want, copied)
		}
	}
}

func TestCtrlCCopiesFocusedContentLineWithoutSelection(t *testing.T) {
	m := newContentSelectionModel(t)
	m.contentFocusLine = indexContentLine(t, m, "beta")
	var copied string
	restore := stubClipboardWrite(t, &copied)
	defer restore()

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected ctrl+c to copy focused content line")
	}
	if msg := cmd(); msg != (ClipboardCopiedMsg{}) {
		t.Fatalf("expected successful clipboard message, got %#v", msg)
	}
	if strings.TrimSpace(copied) != "beta" {
		t.Fatalf("expected focused line copy, got %q", copied)
	}
}

func TestEscCancelsContentSelectionBeforeLeavingContentPane(t *testing.T) {
	m := newContentSelectionModel(t)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	m = next.(Model)
	if !m.contentSelectionActive {
		t.Fatal("expected visual selection active")
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.contentSelectionActive {
		t.Fatal("expected esc to cancel selection")
	}
	if m.focused != paneContent {
		t.Fatalf("expected focus to remain in content pane after canceling selection, got %v", m.focused)
	}
}

func TestContentSelectionKeepsThreePaneLayout(t *testing.T) {
	m := newContentSelectionModel(t)
	m.accounts = []db.Account{{ID: 1, Name: "Work Account"}}
	m.sidebarRows = []sidebarRow{{kind: rowKindAccount, accountID: 1}}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	m = next.(Model)
	view := ansi.Strip(m.View())
	for _, want := range []string{"Accounts", "Messages", "Work Account", "alpha"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected visual selection to preserve three-pane text %q, got %q", want, view)
		}
	}
}

func indexContentLine(t *testing.T, m Model, needle string) int {
	t.Helper()
	for i, line := range m.contentLines {
		if strings.Contains(line, needle) {
			return i
		}
	}
	t.Fatalf("line containing %q not found in %#v", needle, m.contentLines)
	return 0
}

func stubClipboardWrite(t *testing.T, copied *string) func() {
	t.Helper()
	orig := clipboardWriteCmd
	clipboardWriteCmd = func(text string) tea.Cmd {
		return func() tea.Msg {
			*copied = text
			return ClipboardCopiedMsg{}
		}
	}
	return func() { clipboardWriteCmd = orig }
}
