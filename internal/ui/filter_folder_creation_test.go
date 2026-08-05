package ui

import (
	"strings"
	"testing"

	"github.com/allisonhere/tide/internal/db"
	"github.com/allisonhere/tide/internal/filter"
	tea "github.com/charmbracelet/bubbletea"
)

func moveFilterDraft(accountID int64) filterManager {
	return filterManager{
		mode:      fmReview,
		draftAcct: accountID,
		draftEn:   "move all AgentMail to a new folder named SystemStats",
		draft: filter.Rule{
			Match:      filter.MatchAll,
			Conditions: []filter.Condition{{Field: filter.FieldFrom, Op: filter.OpContains, Value: "agentmail"}},
			Action:     filter.Action{Type: filter.ActionMove, Target: "SystemStats"},
		},
	}
}

func hasMailboxNamed(t *testing.T, database interface {
	ListMailboxes(int64) ([]db.Mailbox, error)
}, accountID int64, name string) bool {
	t.Helper()
	mailboxes, err := database.ListMailboxes(accountID)
	if err != nil {
		t.Fatalf("ListMailboxes: %v", err)
	}
	for _, mailbox := range mailboxes {
		if strings.EqualFold(mailbox.Name, name) {
			return true
		}
	}
	return false
}

func TestSavingMoveFilterCreatesFolderInSelectedAccount(t *testing.T) {
	m, accA, accB := pickAccountModel(t)
	m.overlay = overlayFilterManager
	m.filterManager = moveFilterDraft(accA)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = next.(Model)
	if cmd == nil || !m.filterManager.saving {
		t.Fatal("expected save to wait for destination-folder preparation")
	}
	if rules, _ := m.db.ListRules(); len(rules) != 0 {
		t.Fatalf("rule saved before folder creation: %d rules", len(rules))
	}

	prepared := cmd().(FilterFoldersPreparedMsg)
	next, cmd = m.Update(prepared)
	m = next.(Model)
	if cmd != nil {
		t.Fatal("save-only should not start a filter run")
	}
	if !hasMailboxNamed(t, m.db, accA, "SystemStats") {
		t.Fatal("selected account did not gain SystemStats")
	}
	if hasMailboxNamed(t, m.db, accB, "SystemStats") {
		t.Fatal("unselected account gained SystemStats")
	}
	rules, _ := m.db.ListRules()
	if len(rules) != 1 || rules[0].AccountID != accA {
		t.Fatalf("saved rules = %+v, want one scoped to account %d", rules, accA)
	}
}

func TestSavingAllAccountsMoveCreatesFolderEverywhere(t *testing.T) {
	m, accA, accB := pickAccountModel(t)
	m.overlay = overlayFilterManager
	m.filterManager = moveFilterDraft(0)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = next.(Model)
	prepared := cmd().(FilterFoldersPreparedMsg)
	next, _ = m.Update(prepared)
	m = next.(Model)

	for _, accountID := range []int64{accA, accB} {
		if !hasMailboxNamed(t, m.db, accountID, "SystemStats") {
			t.Fatalf("account %d did not gain SystemStats", accountID)
		}
	}
	rules, _ := m.db.ListRules()
	if len(rules) != 1 || rules[0].AccountID != 0 {
		t.Fatalf("saved rules = %+v, want one all-accounts rule", rules)
	}
}

func TestSavingMoveFilterReusesFolderCaseInsensitively(t *testing.T) {
	m, accA, _ := pickAccountModel(t)
	existingID, err := m.db.UpsertMailbox(db.Mailbox{AccountID: accA, Name: "systemstats", Delimiter: "/"})
	if err != nil {
		t.Fatalf("UpsertMailbox: %v", err)
	}
	m.overlay = overlayFilterManager
	m.filterManager = moveFilterDraft(accA)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	prepared := cmd().(FilterFoldersPreparedMsg)
	if prepared.Err != nil {
		t.Fatalf("prepare folders: %v", prepared.Err)
	}
	if len(prepared.Mailboxes) != 1 || prepared.Mailboxes[0].ID != existingID {
		t.Fatalf("prepared mailboxes = %+v, want existing ID %d", prepared.Mailboxes, existingID)
	}
	mailboxes, _ := m.db.ListMailboxes(accA)
	if len(mailboxes) != 2 { // INBOX + existing systemstats
		t.Fatalf("expected no duplicate folder, got %+v", mailboxes)
	}
}

func TestMoveFilterFolderFailureLeavesRuleInReview(t *testing.T) {
	m, _, _ := pickAccountModel(t)
	m.accounts = nil
	m.overlay = overlayFilterManager
	m.filterManager = moveFilterDraft(0)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = next.(Model)
	prepared := cmd().(FilterFoldersPreparedMsg)
	next, _ = m.Update(prepared)
	m = next.(Model)

	if m.filterManager.mode != fmReview || m.filterManager.saving {
		t.Fatalf("expected editable review after failure, mode=%v saving=%v", m.filterManager.mode, m.filterManager.saving)
	}
	if !strings.Contains(m.filterManager.status, "save failed") {
		t.Fatalf("status = %q, want save failure", m.filterManager.status)
	}
	if rules, _ := m.db.ListRules(); len(rules) != 0 {
		t.Fatalf("folder failure saved %d rules", len(rules))
	}
}

func TestAllAccountsFolderPartialFailureIsRetryable(t *testing.T) {
	m, accA, accB := pickAccountModel(t)
	// Account A is a configured local-only account. Omitting B's configuration
	// makes preparation fail only after A has been created successfully.
	m.cfg.Accounts = m.cfg.Accounts[:1]
	m.overlay = overlayFilterManager
	m.filterManager = moveFilterDraft(0)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = next.(Model)
	prepared := cmd().(FilterFoldersPreparedMsg)
	if prepared.Err == nil || len(prepared.Mailboxes) != 1 {
		t.Fatalf("prepared = %+v, want one success followed by an error", prepared)
	}
	next, _ = m.Update(prepared)
	m = next.(Model)

	if !hasMailboxNamed(t, m.db, accA, "SystemStats") {
		t.Fatal("successful account folder was not retained")
	}
	if hasMailboxNamed(t, m.db, accB, "SystemStats") {
		t.Fatal("failed account unexpectedly gained the folder")
	}
	if m.mailboxByID(prepared.Mailboxes[0].ID) == nil {
		t.Fatal("successful partial result was not added to the live sidebar model")
	}
	if rules, _ := m.db.ListRules(); len(rules) != 0 {
		t.Fatalf("partial failure saved %d rules", len(rules))
	}
}

func TestSaveAndRunAllWaitsForFolderPreparation(t *testing.T) {
	m, _, _ := pickAccountModel(t)
	m.overlay = overlayFilterManager
	m.filterManager = moveFilterDraft(0)

	next, prepareCmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	m = next.(Model)
	if rules, _ := m.db.ListRules(); len(rules) != 0 {
		t.Fatalf("rule saved before preparation: %d rules", len(rules))
	}
	prepared := prepareCmd().(FilterFoldersPreparedMsg)
	next, runCmd := m.Update(prepared)
	m = next.(Model)
	if runCmd == nil {
		t.Fatal("save-and-run-all did not continue after folder preparation")
	}
	run := runCmd().(FilterRunMsg)
	if run.Err != nil || run.Matched != 0 {
		t.Fatalf("unexpected run result: %+v", run)
	}
	if rules, _ := m.db.ListRules(); len(rules) != 1 {
		t.Fatalf("expected one saved rule before run, got %d", len(rules))
	}
}
