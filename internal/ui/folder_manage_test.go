package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
)

// folderManageModel is a real DB with INBOX, Work, Work/Projects, Work/Projects/Alpha
// and Sent, and no IMAPHost, so the commands persist locally without a network call.
func folderManageModel(t *testing.T) (Model, *db.DB, int64) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	accountID, err := database.AddAccount("", "Personal", "")
	if err != nil {
		t.Fatalf("AddAccount: %v", err)
	}
	for _, mb := range []db.Mailbox{
		{Name: "INBOX"}, {Name: "Sent", Flags: []string{`\Sent`}}, {Name: "Work"},
		{Name: "Work/Projects"}, {Name: "Work/Projects/Alpha"},
	} {
		mb.AccountID, mb.Delimiter = accountID, "/"
		if _, err := database.UpsertMailbox(mb); err != nil {
			t.Fatalf("UpsertMailbox: %v", err)
		}
	}
	cfg := config.DefaultConfig()
	cfg.Accounts = []config.AccountConfig{{Name: "Personal"}}
	m := NewModel(database, cfg, "dev", false)
	next, _ := m.Update(AccountsLoadedMsg{
		Accounts:  []db.Account{{ID: accountID, Name: "Personal"}},
		Mailboxes: mustListMailboxes(t, database, accountID),
	})
	m = next.(Model)
	m.width, m.height = 100, 40
	m.focused = paneAccounts
	return m, database, accountID
}

func mailboxIDByName(t *testing.T, m Model, name string) int64 {
	t.Helper()
	for _, mb := range m.mailboxes {
		if mb.Name == name {
			return mb.ID
		}
	}
	t.Fatalf("no mailbox %q", name)
	return 0
}

func pressKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	next, cmd := m.Update(k)
	return next.(Model), cmd
}

func typeText(m Model, text string) Model {
	for _, r := range text {
		m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func applyCmd(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command")
	}
	next, _ := m.Update(cmd())
	return next.(Model)
}

func namesOf(m Model) map[string]bool {
	out := map[string]bool{}
	for _, mb := range m.mailboxes {
		out[mb.Name] = true
	}
	return out
}

func TestNewFolderKeyCreatesASubfolderOfTheFocusedFolder(t *testing.T) {
	m, database, accountID := folderManageModel(t)
	cursorOnMailbox(t, &m, mailboxIDByName(t, m, "Work"))

	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if !m.folderPrompt.active || m.folderPrompt.parent != "Work" {
		t.Fatalf("n should open a prompt under Work, got %+v", m.folderPrompt)
	}
	m = typeText(m, "Clients")
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.folderPrompt.active {
		t.Fatal("prompt should close on enter")
	}
	m = applyCmd(t, m, cmd)

	if !namesOf(m)["Work/Clients"] {
		t.Fatalf("Work/Clients not created: %v", namesOf(m))
	}
	if got := mustListMailboxes(t, database, accountID); len(got) != 6 {
		t.Fatalf("DB has %d mailboxes, want 6", len(got))
	}
}

func TestNewFolderOnAnAccountHeaderCreatesATopLevelFolder(t *testing.T) {
	m, _, _ := folderManageModel(t)
	for i, r := range m.sidebarRows {
		if r.kind == rowKindAccount {
			m.sidebarCursor = i
		}
	}
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = typeText(m, "Receipts")
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = applyCmd(t, m, cmd)
	if !namesOf(m)["Receipts"] {
		t.Fatalf("Receipts not created: %v", namesOf(m))
	}
}

func TestNewFolderRejectsClashesAndDelimiters(t *testing.T) {
	m, _, _ := folderManageModel(t)
	cursorOnMailbox(t, &m, mailboxIDByName(t, m, "Work"))
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})

	m = typeText(m, "projects") // clashes with Work/Projects, case-insensitively
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || !m.folderPrompt.active {
		t.Fatalf("a clashing name must keep the prompt open")
	}
	if !m.statusErr {
		t.Fatalf("expected an error status, got %q", m.statusMsg)
	}

	m.folderPrompt.input.SetValue("a/b")
	m, cmd = pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || !m.folderPrompt.active {
		t.Fatalf("a name containing the delimiter must be rejected")
	}

	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyEscape})
	if m.folderPrompt.active {
		t.Fatalf("esc should cancel the prompt")
	}
}

func TestRenameFolderRenamesItsSubfolders(t *testing.T) {
	m, database, accountID := folderManageModel(t)
	cursorOnMailbox(t, &m, mailboxIDByName(t, m, "Work"))
	alphaID := mailboxIDByName(t, m, "Work/Projects/Alpha")

	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if !m.folderPrompt.active || !m.folderPrompt.rename || m.folderPrompt.input.Value() != "Work" {
		t.Fatalf("r should open a rename prompt prefilled with the name, got %+v", m.folderPrompt)
	}
	m.folderPrompt.input.SetValue("Office")
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = applyCmd(t, m, cmd)

	names := namesOf(m)
	for _, want := range []string{"Office", "Office/Projects", "Office/Projects/Alpha"} {
		if !names[want] {
			t.Fatalf("%q missing after rename: %v", want, names)
		}
	}
	if names["Work"] || names["Work/Projects"] {
		t.Fatalf("old names should be gone: %v", names)
	}
	if got := m.mailboxByID(alphaID); got == nil || got.Name != "Office/Projects/Alpha" {
		t.Fatalf("Alpha kept its ID but not its new name: %+v", got)
	}
	persisted := map[string]bool{}
	for _, mb := range mustListMailboxes(t, database, accountID) {
		persisted[mb.Name] = true
	}
	if !persisted["Office/Projects/Alpha"] || persisted["Work"] {
		t.Fatalf("DB not renamed: %v", persisted)
	}
}

func TestRenameAndDeleteRefuseSystemFolders(t *testing.T) {
	m, _, _ := folderManageModel(t)
	for _, name := range []string{"INBOX", "Sent"} {
		cursorOnMailbox(t, &m, mailboxIDByName(t, m, name))
		next, _ := pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
		if next.folderPrompt.active {
			t.Fatalf("%s must not be renamable", name)
		}
		next, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
		if next.overlay != overlayNone {
			t.Fatalf("%s must not be deletable", name)
		}
		if !next.statusErr {
			t.Fatalf("%s: expected an explanatory error status", name)
		}
	}
}

func TestDeleteFolderConfirmsAndRemovesTheSubtree(t *testing.T) {
	m, database, accountID := folderManageModel(t)
	cursorOnMailbox(t, &m, mailboxIDByName(t, m, "Work"))

	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if m.overlay != overlayFolderDeleteConfirm {
		t.Fatalf("d should ask for confirmation, overlay = %v", m.overlay)
	}
	if got := m.folderDeleteConfirmText(); !strings.Contains(got, "2 subfolders") {
		t.Fatalf("confirmation should mention the subfolders, got %q", got)
	}

	// Any other key cancels and deletes nothing.
	cancelled, _ := pressKey(m, tea.KeyMsg{Type: tea.KeyEscape})
	if cancelled.overlay != overlayNone || len(cancelled.mailboxes) != 5 {
		t.Fatalf("esc must cancel without deleting")
	}

	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = applyCmd(t, m, cmd)
	names := namesOf(m)
	if names["Work"] || names["Work/Projects"] || names["Work/Projects/Alpha"] {
		t.Fatalf("subtree should be gone: %v", names)
	}
	if !names["INBOX"] || !names["Sent"] {
		t.Fatalf("unrelated folders were removed: %v", names)
	}
	if got := mustListMailboxes(t, database, accountID); len(got) != 2 {
		t.Fatalf("DB has %d mailboxes, want 2", len(got))
	}
}
