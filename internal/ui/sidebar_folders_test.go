package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tidemail/internal/db"
)

func nestedFolderModel(t *testing.T) Model {
	t.Helper()
	m := newStatusBarModel(t)
	m.accounts = []db.Account{{ID: 1, Name: "Personal"}}
	m.mailboxes = []db.Mailbox{
		{ID: 10, AccountID: 1, Name: "INBOX", Delimiter: "/"},
		{ID: 11, AccountID: 1, Name: "Work", Delimiter: "/", UnreadCount: 1},
		{ID: 12, AccountID: 1, Name: "Work/Projects", Delimiter: "/", UnreadCount: 2},
		{ID: 13, AccountID: 1, Name: "Work/Projects/Alpha", Delimiter: "/", UnreadCount: 4},
		{ID: 14, AccountID: 1, Name: "Zeta", Delimiter: "/"},
	}
	m.focused = paneAccounts
	m.rebuildSidebar()
	return m
}

func folderRowIDs(m Model) []int64 {
	var ids []int64
	for _, r := range m.sidebarRows {
		if r.kind == rowKindMailbox {
			ids = append(ids, r.mailboxID)
		}
	}
	return ids
}

func equalIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSidebarNestsFoldersByHierarchy(t *testing.T) {
	m := nestedFolderModel(t)
	if got := folderRowIDs(m); !equalIDs(got, []int64{10, 11, 12, 13, 14}) {
		t.Fatalf("folder order = %v, want parents followed by their subfolders", got)
	}
	depth := map[int64]int{}
	for _, r := range m.sidebarRows {
		if r.kind == rowKindMailbox {
			depth[r.mailboxID] = r.depth
		}
	}
	if depth[11] != 0 || depth[12] != 1 || depth[13] != 2 || depth[14] != 0 {
		t.Fatalf("depths = %v", depth)
	}
	for _, r := range m.sidebarRows {
		if r.mailboxID == 12 && (r.label != "Projects" || !r.hasChildren) {
			t.Fatalf("Work/Projects row = %+v, want title Projects with children", r)
		}
	}
}

func TestSidebarFolderWithoutListedParentHangsFromAncestor(t *testing.T) {
	m := nestedFolderModel(t)
	m.mailboxes = []db.Mailbox{
		{ID: 10, AccountID: 1, Name: "INBOX", Delimiter: "/"},
		{ID: 11, AccountID: 1, Name: "Work", Delimiter: "/"},
		{ID: 13, AccountID: 1, Name: "Work/Projects/Alpha", Delimiter: "/"},
		{ID: 15, AccountID: 1, Name: "Lonely/Child", Delimiter: "/"},
	}
	m.rebuildSidebar()
	for _, r := range m.sidebarRows {
		switch r.mailboxID {
		case 13:
			if r.depth != 1 || r.label != "Projects/Alpha" {
				t.Fatalf("Alpha = %+v, want depth 1 titled Projects/Alpha", r)
			}
		case 15:
			if r.depth != 0 || r.label != "" {
				t.Fatalf("Lonely/Child = %+v, want a top-level row", r)
			}
		}
	}
}

func TestSidebarDovecotInboxPrefixIsNotAParentFolder(t *testing.T) {
	m := nestedFolderModel(t)
	m.mailboxes = []db.Mailbox{
		{ID: 10, AccountID: 1, Name: "INBOX", Delimiter: "."},
		{ID: 11, AccountID: 1, Name: "INBOX.Work", Delimiter: "."},
		{ID: 12, AccountID: 1, Name: "INBOX.Work.Projects", Delimiter: "."},
	}
	m.rebuildSidebar()
	for _, r := range m.sidebarRows {
		if r.mailboxID == 11 && r.depth != 0 {
			t.Fatalf("INBOX.Work depth = %d, want top level", r.depth)
		}
		if r.mailboxID == 12 && r.depth != 1 {
			t.Fatalf("INBOX.Work.Projects depth = %d, want 1", r.depth)
		}
	}
}

func cursorOnMailbox(t *testing.T, m *Model, id int64) {
	t.Helper()
	for i, r := range m.sidebarRows {
		if r.kind == rowKindMailbox && r.mailboxID == id {
			m.sidebarCursor = i
			return
		}
	}
	t.Fatalf("mailbox %d not in the sidebar", id)
}

func TestEnterCollapsesAndExpandsAFolderAndRemembersIt(t *testing.T) {
	m := nestedFolderModel(t)
	cursorOnMailbox(t, &m, 11)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if got := folderRowIDs(m); !equalIDs(got, []int64{10, 11, 14}) {
		t.Fatalf("after collapse = %v", got)
	}
	if m.sidebarRows[m.sidebarCursor].mailboxID != 11 {
		t.Fatalf("cursor left the folder it collapsed")
	}
	if !m.collapsedSections[folderCollapseKey(m.mailboxes[1])] {
		t.Fatal("collapse state not recorded")
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if got := folderRowIDs(m); !equalIDs(got, []int64{10, 11, 12, 13, 14}) {
		t.Fatalf("after expand = %v", got)
	}
}

func TestLeftCollapsesThenJumpsToParentAndRightExpands(t *testing.T) {
	m := nestedFolderModel(t)
	cursorOnMailbox(t, &m, 13) // a leaf

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = next.(Model)
	if got := m.sidebarRows[m.sidebarCursor].mailboxID; got != 12 {
		t.Fatalf("Left from a leaf should land on its parent, got %d", got)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft}) // collapses 12
	m = next.(Model)
	if got := folderRowIDs(m); !equalIDs(got, []int64{10, 11, 12, 14}) {
		t.Fatalf("Left on an open parent should collapse it, rows = %v", got)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft}) // jumps to 11
	m = next.(Model)
	if got := m.sidebarRows[m.sidebarCursor].mailboxID; got != 11 {
		t.Fatalf("Left on a collapsed child should jump to its parent, got %d", got)
	}
	cursorOnMailbox(t, &m, 12)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(Model)
	if got := folderRowIDs(m); !equalIDs(got, []int64{10, 11, 12, 13, 14}) {
		t.Fatalf("Right on a collapsed parent should expand it, rows = %v", got)
	}
	if m.focused != paneAccounts {
		t.Fatalf("expanding must not move focus off the sidebar")
	}
}

func TestCollapsedFolderShowsRolledUpUnreadCount(t *testing.T) {
	m := nestedFolderModel(t)
	cursorOnMailbox(t, &m, 11)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	row := m.sidebarRows[m.sidebarCursor]
	view := ansi.Strip(m.renderSidebarMailboxRow(m.mailboxes[1], row, false, 40))
	if !strings.Contains(view, "(7)") {
		t.Fatalf("collapsed Work should show 1+2+4 unread, got %q", view)
	}
	if !strings.Contains(view, "Work") {
		t.Fatalf("row lost its title: %q", view)
	}
}
