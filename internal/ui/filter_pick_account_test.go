package ui

import (
	"context"
	"testing"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	"github.com/allisonhere/tide/internal/filter"
	tea "github.com/charmbracelet/bubbletea"
)

func pickAccountModel(t *testing.T) (Model, int64, int64) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	accA, _ := database.AddAccount("A", "")
	accB, _ := database.AddAccount("B", "")
	database.UpsertMailbox(db.Mailbox{AccountID: accA, Name: "INBOX", Delimiter: "/"})
	database.UpsertMailbox(db.Mailbox{AccountID: accB, Name: "INBOX", Delimiter: "/"})
	cfg := config.DefaultConfig()
	cfg.Accounts = []config.AccountConfig{{Name: "A"}, {Name: "B"}}
	m := NewModel(database, cfg, "dev", false)
	next, _ := m.Update(AccountsLoadedMsg{
		Accounts:  []db.Account{{ID: accA, Name: "A"}, {ID: accB, Name: "B"}},
		Mailboxes: append(mustListMailboxes(t, database, accA), mustListMailboxes(t, database, accB)...),
	})
	return next.(Model), accA, accB
}

func TestPickAccountSetsDraftScope(t *testing.T) {
	m, _, accB := pickAccountModel(t)
	m.filterManager = m.newFilterManager()
	m.overlay = overlayFilterManager

	// n -> account picker
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = next.(Model)
	if m.filterManager.mode != fmPickAccount {
		t.Fatalf("expected account picker, got mode %v", m.filterManager.mode)
	}

	// move to "B" (index 0 = All, 1 = A, 2 = B) and choose
	for m.filterManager.acctCursor < 2 {
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
		m = next.(Model)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if m.filterManager.mode != fmInput {
		t.Fatalf("expected input mode after choosing account, got %v", m.filterManager.mode)
	}
	if m.filterManager.draftAcct != accB {
		t.Fatalf("draftAcct = %d, want account B (%d)", m.filterManager.draftAcct, accB)
	}
}

func TestPickAllAccountsSetsZeroScope(t *testing.T) {
	m, _, _ := pickAccountModel(t)
	m.filterManager = m.newFilterManager()
	m.overlay = overlayFilterManager

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = next.(Model)
	// move cursor to top (All accounts = index 0)
	for m.filterManager.acctCursor > 0 {
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
		m = next.(Model)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if m.filterManager.draftAcct != 0 {
		t.Fatalf("expected All-accounts scope (0), got %d", m.filterManager.draftAcct)
	}
}

// A move rule may recreate its destination in any account covered by its scope.
func TestMoveCreatesFolderWhenMissing(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()
	accB, _ := database.AddAccount("B", "")
	bInbox, _ := database.UpsertMailbox(db.Mailbox{AccountID: accB, Name: "INBOX", Delimiter: "/"})
	msg := db.Message{MailboxID: bInbox, UID: 1, From: "newsletter@substack.com", Subject: "Weekly"}
	database.UpsertMessage(msg)
	stored, _ := database.ListMessages(bInbox)

	action := filter.Action{Type: filter.ActionMove, Target: "Reading"} // B has no Reading
	mailbox, _ := database.GetMailbox(bInbox)
	acted, err := applyFilterAction(context.Background(), database, nil, mailbox, stored[0], action)
	if err != nil {
		t.Fatalf("applyFilterAction: %v", err)
	}
	if !acted {
		t.Fatalf("expected the move to create its folder and act")
	}
	found := false
	mbs, _ := database.ListMailboxes(accB)
	for _, mb := range mbs {
		if mb.Name == "Reading" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Reading should have been created on account B")
	}
	if msgs, _ := database.ListMessages(bInbox); len(msgs) != 0 {
		t.Fatalf("message should have left INBOX, got %d", len(msgs))
	}
}

// An account-scoped move also creates a missing folder.
func TestScopedMoveCreatesFolder(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()
	acc, _ := database.AddAccount("A", "")
	inbox, _ := database.UpsertMailbox(db.Mailbox{AccountID: acc, Name: "INBOX", Delimiter: "/"})
	database.UpsertMessage(db.Message{MailboxID: inbox, UID: 1, From: "x@substack.com"})
	stored, _ := database.ListMessages(inbox)
	mailbox, _ := database.GetMailbox(inbox)

	acted, err := applyFilterAction(context.Background(), database, nil, mailbox, stored[0], filter.Action{Type: filter.ActionMove, Target: "Reading"})
	if err != nil {
		t.Fatalf("applyFilterAction: %v", err)
	}
	if !acted {
		t.Fatalf("expected the move to act (create + move)")
	}
	found := false
	mbs, _ := database.ListMailboxes(acc)
	for _, mb := range mbs {
		if mb.Name == "Reading" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Reading should have been created")
	}
}
