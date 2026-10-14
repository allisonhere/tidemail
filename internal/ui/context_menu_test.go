package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tidemail/internal/db"
)

func rightClickAt(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonRight}
}

func menuIDs(m Model) []string {
	ids := make([]string, 0, len(m.ctxMenu.items))
	for _, it := range m.ctxMenu.items {
		ids = append(ids, it.id)
	}
	return ids
}

func menuIndex(t *testing.T, m Model, id string) int {
	t.Helper()
	for i, it := range m.ctxMenu.items {
		if it.id == id {
			return i
		}
	}
	t.Fatalf("menu has no %q item: %v", id, menuIDs(m))
	return -1
}

func TestRightClickMessageOpensMessageMenu(t *testing.T) {
	m := messageListTestModel(t, 5)
	next, _ := m.Update(rightClickAt(m.feedsPaneWidth()+5, messageRowY(m, 2)))
	got := next.(Model)
	if got.messageCursor != 2 {
		t.Fatalf("right-click should select row 2, got %d", got.messageCursor)
	}
	if got.overlay != overlayContextMenu {
		t.Fatalf("expected the context menu, got overlay %v", got.overlay)
	}
	ids := strings.Join(menuIDs(got), ",")
	for _, want := range []string{"reply", "reply-all", "forward", "archive", "move", "delete", "toggle-read"} {
		if !strings.Contains(ids, want) {
			t.Fatalf("message menu missing %q: %s", want, ids)
		}
	}
	if strings.Contains(ids, "new-folder") || strings.Contains(ids, "outbox") {
		t.Fatalf("message menu should not carry folder or global commands: %s", ids)
	}
}

func TestRightClickFolderOpensFolderMenu(t *testing.T) {
	m := mouseTestModel(t)
	m.mailboxes = []db.Mailbox{{ID: 7, AccountID: 1, Name: "Receipts"}}
	m.sidebarRows = []sidebarRow{{kind: rowKindUnified}, {kind: rowKindMailbox, accountID: 1, mailboxID: 7}}
	m.sidebarCursor = 0
	m.sidebarOffset = 0

	y := 1 + m.paneHeaderHeight() + m.styles.ListItemLineStride()
	next, _ := m.Update(rightClickAt(m.feedsPaneWidth()-1, y))
	got := next.(Model)
	if got.overlay != overlayContextMenu || got.sidebarCursor != 1 {
		t.Fatalf("expected a menu on the folder row, overlay=%v cursor=%d", got.overlay, got.sidebarCursor)
	}
	want := "sync,new-folder,rename-folder,hide-folder,delete-folder"
	if ids := strings.Join(menuIDs(got), ","); ids != want {
		t.Fatalf("folder menu = %s; want %s", ids, want)
	}
	if got.ctxMenu.items[menuIndex(t, got, "hide-folder")].label != "Hide folder" {
		t.Fatalf("hide item label = %q", got.ctxMenu.items[menuIndex(t, got, "hide-folder")].label)
	}
}

func TestRightClickAccountAndVirtualRows(t *testing.T) {
	m := mouseTestModel(t)
	m.sidebarRows = []sidebarRow{{kind: rowKindUnified}, {kind: rowKindAccount, accountID: 1}}
	m.sidebarCursor = 0
	y := 1 + m.paneHeaderHeight() + m.styles.ListItemLineStride()
	next, _ := m.Update(rightClickAt(3, y))
	got := next.(Model)
	if ids := strings.Join(menuIDs(got), ","); ids != "new-folder,sync-all,accounts" {
		t.Fatalf("account menu = %s", ids)
	}

	y = 1 + m.paneHeaderHeight()
	next, _ = m.Update(rightClickAt(3, y))
	got = next.(Model)
	if ids := strings.Join(menuIDs(got), ","); ids != "sync-all" {
		t.Fatalf("unified inbox menu = %s", ids)
	}
}

func TestRightClickWithNothingToOfferOpensNoMenu(t *testing.T) {
	m := mouseTestModel(t) // no messages
	next, _ := m.Update(rightClickAt(m.feedsPaneWidth()+5, messageRowY(m, 0)))
	if got := next.(Model); got.overlay != overlayNone {
		t.Fatalf("expected no menu without a message, got overlay %v", got.overlay)
	}
}

func TestRightClickIgnoredWhenMouseSupportDisabled(t *testing.T) {
	m := messageListTestModel(t, 3)
	m.cfg.Display.MouseSupport = false
	next, _ := m.Update(rightClickAt(m.feedsPaneWidth()+5, messageRowY(m, 1)))
	if got := next.(Model); got.overlay != overlayNone || got.messageCursor != 0 {
		t.Fatalf("right-click should do nothing with mouse support off")
	}
}

func TestContextMenuStaysOnScreen(t *testing.T) {
	m := messageListTestModel(t, 3)
	// Far right edge on a message row, and a direct open at the very corner.
	next, _ := m.Update(rightClickAt(m.width-1, messageRowY(m, 0)))
	got := next.(Model)
	if got.overlay != overlayContextMenu {
		t.Fatalf("expected a menu at the right edge, got overlay %v", got.overlay)
	}
	corner := got
	corner.openContextMenu(m.width-1, m.height-1, got.ctxMenu.items)
	for _, cm := range []contextMenu{got.ctxMenu, corner.ctxMenu} {
		if cm.x < 0 || cm.y < 0 || cm.x+cm.w > m.width || cm.y+len(cm.items)+2 > m.height {
			t.Fatalf("menu escapes the %dx%d screen: %+v", m.width, m.height, cm)
		}
	}
	view := ansi.Strip(got.View())
	if !strings.Contains(view, "Archive") || !strings.Contains(view, "Mark as read") {
		t.Fatalf("menu not drawn in the view:\n%s", view)
	}
}

func TestContextMenuClickRunsItemAndOutsideClickCloses(t *testing.T) {
	m := messageListTestModel(t, 3)
	next, _ := m.Update(rightClickAt(m.feedsPaneWidth()+5, messageRowY(m, 1)))
	open := next.(Model)
	idx := menuIndex(t, open, "toggle-read")

	click := leftClick(open.ctxMenu.x+3, open.ctxMenu.y+1+idx)
	next, cmd := open.Update(click)
	ran := next.(Model)
	if ran.overlay != overlayNone || cmd == nil {
		t.Fatalf("clicking an item should close the menu and run it: overlay=%v cmd=%v", ran.overlay, cmd)
	}

	next, cmd = open.Update(leftClick(0, 0))
	closed := next.(Model)
	if closed.overlay != overlayNone || cmd != nil || closed.messageCursor != 1 {
		t.Fatalf("an outside click should only close the menu: overlay=%v cursor=%d", closed.overlay, closed.messageCursor)
	}
}

func TestContextMenuKeyboardNavigation(t *testing.T) {
	m := messageListTestModel(t, 3)
	next, _ := m.Update(rightClickAt(m.feedsPaneWidth()+5, messageRowY(m, 0)))
	open := next.(Model)
	if open.ctxMenu.cursor != 0 {
		t.Fatalf("cursor should start on the first enabled item, got %d", open.ctxMenu.cursor)
	}
	next, _ = open.Update(tea.KeyMsg{Type: tea.KeyDown})
	if c := next.(Model).ctxMenu.cursor; c != 1 {
		t.Fatalf("down moved to %d; want 1", c)
	}
	next, _ = open.Update(tea.KeyMsg{Type: tea.KeyUp})
	if c := next.(Model).ctxMenu.cursor; c != len(open.ctxMenu.items)-1 {
		t.Fatalf("up should wrap to the last item, got %d", c)
	}
	next, _ = open.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if got := next.(Model); got.overlay != overlayNone {
		t.Fatalf("esc should close the menu, overlay=%v", got.overlay)
	}
}

func TestContextMenuSkipsDisabledItems(t *testing.T) {
	m := mouseTestModel(t)
	m.openContextMenu(5, 5, []commandItem{
		{id: "a", label: "A", enabled: false},
		{id: "b", label: "B", enabled: true},
		{id: "c", label: "C", enabled: false},
		{id: "d", label: "D", enabled: true},
	})
	if m.ctxMenu.cursor != 1 {
		t.Fatalf("cursor should start on the first enabled item, got %d", m.ctxMenu.cursor)
	}
	m.stepContextMenu(1)
	if m.ctxMenu.cursor != 3 {
		t.Fatalf("step should skip the disabled item, got %d", m.ctxMenu.cursor)
	}
	next, cmd := m.runContextMenuItem(0)
	if next.(Model).overlay != overlayContextMenu || cmd != nil {
		t.Fatalf("a disabled item must not run or close the menu")
	}
}
