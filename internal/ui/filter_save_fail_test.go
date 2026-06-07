package ui

import (
	"strings"
	"testing"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	"github.com/allisonhere/tide/internal/filter"
	tea "github.com/charmbracelet/bubbletea"
)

// A failed save must not be reported as "saved", must stay in review, and must
// not run the rule.
func TestFilterSaveFailureStaysInReview(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}

	accountID, _ := database.AddAccount("Personal", "")
	database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "INBOX"})

	m := NewModel(database, config.DefaultConfig(), "dev", false)
	next, _ := m.Update(AccountsLoadedMsg{
		Accounts:  []db.Account{{ID: accountID, Name: "Personal"}},
		Mailboxes: mustListMailboxes(t, database, accountID),
	})
	m = next.(Model)
	m.filterManager = m.newFilterManager()
	m.overlay = overlayFilterManager
	m.filterManager.mode = fmReview
	m.filterManager.draftEn = "x"
	m.filterManager.draftAcct = accountID
	m.filterManager.draft = filter.Rule{
		Match:      filter.MatchAll,
		Conditions: []filter.Condition{{Field: filter.FieldFrom, Op: filter.OpContains, Value: "x"}},
		Action:     filter.Action{Type: filter.ActionMarkRead},
	}

	// Force UpsertRule to fail.
	database.Close()

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = next.(Model)

	if m.filterManager.mode != fmReview {
		t.Fatalf("expected to stay in review on save failure, mode=%v", m.filterManager.mode)
	}
	if !strings.Contains(m.filterManager.status, "save failed") {
		t.Fatalf("expected save-failed status, got %q", m.filterManager.status)
	}
	if cmd != nil {
		t.Fatalf("expected no command on save failure")
	}
}
