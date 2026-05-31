package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/allisonhere/tide/internal/db"
)

func TestBuildMoveEntriesUsesSameAccountAndExcludesSource(t *testing.T) {
	mailboxes := []db.Mailbox{
		{ID: 1, AccountID: 10, Name: "INBOX", Delimiter: "/"},
		{ID: 2, AccountID: 10, Name: "Archive", Delimiter: "/"},
		{ID: 3, AccountID: 20, Name: "Work", Delimiter: "/"},
	}

	entries := buildMoveEntries(mailboxes, 10, "", map[int64]bool{1: true})

	labels := moveEntryLabels(entries)
	if strings.Contains(labels, "INBOX") {
		t.Fatalf("expected source mailbox to be excluded, got %q", labels)
	}
	if strings.Contains(labels, "Work") {
		t.Fatalf("expected other account mailbox to be excluded, got %q", labels)
	}
	if !strings.Contains(labels, "Archive") {
		t.Fatalf("expected same-account target, got %q", labels)
	}
}

func TestBuildMoveEntriesAllowsNoselectParentNavigationOnly(t *testing.T) {
	mailboxes := []db.Mailbox{
		{ID: 1, AccountID: 10, Name: "INBOX", Delimiter: "/"},
		{ID: 2, AccountID: 10, Name: "Projects", Delimiter: "/", Flags: []string{"\\Noselect"}},
		{ID: 3, AccountID: 10, Name: "Projects/Client", Delimiter: "/"},
	}

	root := moveEntryLabels(buildMoveEntries(mailboxes, 10, "", map[int64]bool{1: true}))
	if !strings.Contains(root, "Projects") {
		t.Fatalf("expected noselect parent with children to be navigable, got %q", root)
	}

	entries := buildMoveEntries(mailboxes, 10, "Projects", map[int64]bool{1: true})
	labels := moveEntryLabels(entries)
	if strings.Contains(labels, "move here") {
		t.Fatalf("expected noselect current folder not to be confirmable, got %q", labels)
	}
	if !strings.Contains(labels, "Client") {
		t.Fatalf("expected child target under noselect parent, got %q", labels)
	}
}

func TestMoveKeyOpensMovePickerAndUpperMOpensAccountManager(t *testing.T) {
	m := movePickerTestModel()

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	moved := next.(Model)
	if moved.overlay != overlayMoveMessage {
		t.Fatalf("expected m to open move picker, got overlay %v", moved.overlay)
	}

	m = movePickerTestModel()
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}})
	accounts := next.(Model)
	if accounts.overlay != overlayAccountManager {
		t.Fatalf("expected M to open account manager, got overlay %v", accounts.overlay)
	}
}

func TestUpperMStillSavesMarkdownInSummaryOverlay(t *testing.T) {
	m := movePickerTestModel()
	m.overlay = overlaySummary
	m.summaryMessage = db.Message{ID: 9, Subject: "Summary", Summary: "Done"}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}})
	m = next.(Model)

	if cmd == nil {
		t.Fatal("expected M in summary overlay to save markdown")
	}
	if m.overlay != overlaySummary {
		t.Fatalf("expected summary overlay to stay active, got %v", m.overlay)
	}
}

func TestMovePickerRejectsMixedAccountSelection(t *testing.T) {
	m := movePickerTestModel()
	m.mailboxes = append(m.mailboxes, db.Mailbox{ID: 3, AccountID: 20, Name: "INBOX", Delimiter: "/"})
	m.messages = append(m.messages, db.Message{ID: 2, MailboxID: 3, Subject: "Work"})
	m.filteredMessages = m.messages
	m.selectedMessages = map[int64]bool{1: true, 2: true}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	m = next.(Model)

	if m.overlay == overlayMoveMessage {
		t.Fatal("expected mixed-account move selection not to open picker")
	}
	if !strings.Contains(m.statusMsg, "one account") {
		t.Fatalf("expected mixed-account status, got %q", m.statusMsg)
	}
}

func TestMovePickerConfirmReturnsMoveCommandAndClearsOverlay(t *testing.T) {
	m := movePickerTestModel()
	m.openMovePicker([]db.Message{m.messages[0]})
	m.movePicker.currentPath = "Archive"
	m.refreshMovePickerEntries()

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	if cmd == nil {
		t.Fatal("expected confirming move target to return command")
	}
	if m.overlay != overlayNone {
		t.Fatalf("expected move picker to close after confirm, got %v", m.overlay)
	}
}

func movePickerTestModel() Model {
	msg := db.Message{ID: 1, MailboxID: 1, UID: 11, Subject: "Hello"}
	m := Model{
		keys:             DefaultKeys,
		focused:          paneMessages,
		accounts:         []db.Account{{ID: 10, Name: "Personal"}},
		mailboxes:        []db.Mailbox{{ID: 1, AccountID: 10, Name: "INBOX", Delimiter: "/"}, {ID: 2, AccountID: 10, Name: "Archive", Delimiter: "/"}},
		messages:         []db.Message{msg},
		filteredMessages: []db.Message{msg},
		selectedMessages: make(map[int64]bool),
	}
	m.accountManager = m.newAccountManager()
	return m
}

func moveEntryLabels(entries []moveEntry) string {
	labels := make([]string, 0, len(entries))
	for _, entry := range entries {
		labels = append(labels, entry.label)
	}
	return strings.Join(labels, ",")
}
