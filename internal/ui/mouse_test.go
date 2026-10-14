package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
)

// mouseTestModel builds a ready Model with Mouse support enabled and a
// window size set, so the pane-geometry math handleMouseClick relies on
// (feedsPaneWidth, paneHeaderHeight, ListItemLineStride, ...) is populated
// exactly as it would be in the running app.
func mouseTestModel(t *testing.T) Model {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Display.MouseSupport = true
	m := NewModel(nil, cfg, "dev", false)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	return next.(Model)
}

func leftClick(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
}

func TestMouseClickIgnoredWhenMouseSupportDisabled(t *testing.T) {
	cfg := config.DefaultConfig() // MouseSupport is false by default
	m := NewModel(nil, cfg, "dev", false)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = next.(Model)
	m.focused = paneMessages
	m.filteredMessages = []db.Message{{ID: 1, Subject: "One"}, {ID: 2, Subject: "Two"}}
	m.messageCursor = 0

	next, _ = m.Update(leftClick(50, 10))
	got := next.(Model)
	if got.focused != paneMessages || got.messageCursor != 0 {
		t.Fatalf("expected a click to do nothing while mouse support is off, got focused=%v cursor=%d", got.focused, got.messageCursor)
	}
}

func TestMouseClickSelectsSidebarRow(t *testing.T) {
	m := mouseTestModel(t)
	m.focused = paneMessages // start elsewhere, so the click must change focus too
	m.sidebarRows = []sidebarRow{
		{kind: rowKindUnified},
		{kind: rowKindNeedsYou},
		{kind: rowKindWaiting},
	}
	m.sidebarCursor = 0
	m.sidebarOffset = 0

	interiorTop := 1 + m.paneHeaderHeight()
	stride := m.styles.ListItemLineStride()
	// Click the second row (index 1).
	y := interiorTop + stride
	x := m.feedsPaneWidth() - 1

	next, _ := m.Update(leftClick(x, y))
	got := next.(Model)
	if got.focused != paneAccounts {
		t.Fatalf("expected click to focus the sidebar, got %v", got.focused)
	}
	if got.sidebarCursor != 1 {
		t.Fatalf("expected sidebarCursor 1, got %d", got.sidebarCursor)
	}
}

func TestMouseClickSelectsMessageRow(t *testing.T) {
	m := mouseTestModel(t)
	m.focused = paneAccounts // start elsewhere, so the click must change focus too
	m.filteredMessages = []db.Message{
		{ID: 1, Subject: "First"},
		{ID: 2, Subject: "Second"},
		{ID: 3, Subject: "Third"},
	}
	m.messageCursor = 0
	m.listOffset = 0

	interiorTop := 1 + m.paneHeaderHeight() + m.messageSearchRowHeight()
	stride := m.styles.ListItemLineStride()
	// Click the second row (index 1).
	y := interiorTop + stride
	x := m.feedsPaneWidth() + 1

	next, _ := m.Update(leftClick(x, y))
	got := next.(Model)
	if got.focused != paneMessages {
		t.Fatalf("expected click to focus the messages pane, got %v", got.focused)
	}
	if got.messageCursor != 1 {
		t.Fatalf("expected messageCursor 1, got %d", got.messageCursor)
	}
}

func TestMouseClickFocusesContentPaneOnNonLinkLine(t *testing.T) {
	m := mouseTestModel(t)
	m.focused = paneAccounts
	m.filteredMessages = []db.Message{{ID: 1, Subject: "Hi"}}
	m.contentLines = []string{"Greetings,", "No link on this line.", "Bye"}
	m.contentLineCount = len(m.contentLines)
	m.contentFocusLine = 0

	paneTop := m.articlesPaneOuterHeight()
	interiorTop := paneTop + 1 + m.paneHeaderHeight()
	x := m.feedsPaneWidth() + 1
	y := interiorTop + 1 // the second content line, "No link on this line."

	next, cmd := m.Update(leftClick(x, y))
	got := next.(Model)
	if got.focused != paneContent {
		t.Fatalf("expected click to focus the content pane, got %v", got.focused)
	}
	if got.contentFocusLine != 1 {
		t.Fatalf("expected contentFocusLine 1, got %d", got.contentFocusLine)
	}
	if cmd != nil {
		t.Fatalf("expected no open-browser command for a line without a link")
	}
}

func TestMouseClickOpensContentLink(t *testing.T) {
	m := mouseTestModel(t)
	m.focused = paneAccounts
	m.filteredMessages = []db.Message{{ID: 1, Subject: "Hi"}}
	m.contentLines = []string{"Greetings,", "See https://example.com/article for details.", "Bye"}
	m.contentLineCount = len(m.contentLines)
	m.contentFocusLine = 0

	paneTop := m.articlesPaneOuterHeight()
	interiorTop := paneTop + 1 + m.paneHeaderHeight()
	x := m.feedsPaneWidth() + 1
	y := interiorTop + 1 // the link line

	next, cmd := m.Update(leftClick(x, y))
	got := next.(Model)
	if got.focused != paneContent {
		t.Fatalf("expected click to focus the content pane, got %v", got.focused)
	}
	if got.contentFocusLine != 1 {
		t.Fatalf("expected contentFocusLine 1, got %d", got.contentFocusLine)
	}
	if cmd == nil {
		t.Fatalf("expected clicking a link line to return an open-browser command")
	}
}

func TestMouseWheelAndRightClickAreNoOps(t *testing.T) {
	m := mouseTestModel(t)
	m.focused = paneMessages
	m.filteredMessages = []db.Message{{ID: 1, Subject: "One"}, {ID: 2, Subject: "Two"}}
	m.messageCursor = 0

	wheel := tea.MouseMsg{X: 50, Y: 10, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp}
	next, _ := m.Update(wheel)
	got := next.(Model)
	if got.messageCursor != 0 {
		t.Fatalf("expected wheel events to be a no-op, got messageCursor=%d", got.messageCursor)
	}

	rightClick := tea.MouseMsg{X: 50, Y: 10, Action: tea.MouseActionPress, Button: tea.MouseButtonRight}
	next, _ = got.Update(rightClick)
	got = next.(Model)
	if got.messageCursor != 0 {
		t.Fatalf("expected right-click to be a no-op, got messageCursor=%d", got.messageCursor)
	}
}
