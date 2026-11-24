package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/allisonhere/tidemail/internal/db"
)

func ffTypeText(t *testing.T, m Model, text string) Model {
	t.Helper()
	for _, r := range text {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(Model)
	}
	return m
}

func ffPress(m Model, k tea.KeyType) Model {
	next, _ := m.Update(tea.KeyMsg{Type: k})
	return next.(Model)
}

func TestMovePickerSlashFiltersAllFoldersByFuzzyPath(t *testing.T) {
	m := movePickerTestModel()
	m.mailboxes = append(m.mailboxes,
		db.Mailbox{ID: 3, AccountID: 10, Name: "Projects", Delimiter: "/"},
		db.Mailbox{ID: 4, AccountID: 10, Name: "Projects/Acme", Delimiter: "/"},
		db.Mailbox{ID: 5, AccountID: 10, Name: "Projects/Acme/Invoices", Delimiter: "/"},
		db.Mailbox{ID: 6, AccountID: 20, Name: "Other/Invoices", Delimiter: "/"},
	)
	m.openMovePicker(m.movePickerMessages())
	m = ffTypeText(t, m, "/")
	if !m.movePicker.filtering {
		t.Fatal("/ did not start the filter")
	}
	m = ffTypeText(t, m, "acin")
	if len(m.movePicker.entries) != 1 || m.movePicker.entries[0].mailboxID != 5 {
		t.Fatalf("entries = %+v", m.movePicker.entries)
	}
	if got := m.movePicker.entries[0].label; got != "Projects/Acme/Invoices" {
		t.Fatalf("label = %q", got)
	}
	m = ffPress(m, tea.KeyEsc)
	if m.movePicker.filtering || m.overlay != overlayMoveMessage {
		t.Fatalf("esc should leave the filter and keep the picker open: filtering=%v overlay=%v", m.movePicker.filtering, m.overlay)
	}
	// The source folder (INBOX) is never offered.
	m = ffTypeText(t, m, "/inb")
	for _, e := range m.movePicker.entries {
		if e.mailboxID == 1 {
			t.Fatal("source folder offered as a move target")
		}
	}
}

func gotoTestModel() Model {
	m := Model{
		keys:              DefaultKeys,
		focused:           paneMessages,
		accounts:          []db.Account{{ID: 10, Name: "Personal"}},
		collapsedAccounts: map[int64]bool{},
		collapsedSections: map[string]bool{},
		selectedMessages:  make(map[int64]bool),
	}
	m.mailboxes = []db.Mailbox{
		{ID: 1, AccountID: 10, Name: "INBOX", Delimiter: "/"},
		{ID: 2, AccountID: 10, Name: "Projects", Delimiter: "/"},
		{ID: 3, AccountID: 10, Name: "Projects/Acme", Delimiter: "/"},
		{ID: 4, AccountID: 10, Name: "Projects/Acme/Invoices", Delimiter: "/"},
	}
	m.collapsedSections[folderCollapseKey(m.mailboxes[1])] = true
	m.rebuildSidebar()
	return m
}

func TestGotoFolderExpandsCollapsedParentsAndSelects(t *testing.T) {
	m := gotoTestModel()
	if m.sidebarRowIndex(4) >= 0 {
		t.Fatal("fixture: Invoices should start hidden under a collapsed folder")
	}
	m = ffTypeText(t, m, "G")
	if m.overlay != overlayGotoFolder {
		t.Fatalf("G opened overlay %v", m.overlay)
	}
	m = ffTypeText(t, m, "acin")
	m = ffPress(m, tea.KeyEnter)
	if m.overlay != overlayNone {
		t.Fatalf("overlay = %v after enter", m.overlay)
	}
	if got := m.selectedMailbox(); got == nil || got.ID != 4 {
		t.Fatalf("selected mailbox = %+v", got)
	}
	if m.focused != paneAccounts {
		t.Fatalf("focus = %v, want sidebar", m.focused)
	}
}

func TestGotoFolderEscCancelsWithoutMoving(t *testing.T) {
	m := gotoTestModel()
	before := m.sidebarCursor
	m = ffTypeText(t, m, "Gacin")
	m = ffPress(m, tea.KeyEsc)
	if m.overlay != overlayNone || m.sidebarCursor != before {
		t.Fatalf("overlay=%v cursor %d -> %d", m.overlay, before, m.sidebarCursor)
	}
}
