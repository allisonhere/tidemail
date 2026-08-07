package ui

import (
	"encoding/json"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/filter"
)

// modelWithTwoAccounts builds a model with accounts A (Dovecot-style "INBOX."
// namespace) and B (flat), each holding one inbox message from substack.
func modelWithTwoAccounts(t *testing.T) (Model, *db.DB, int64, int64) {
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
	aInbox, _ := database.UpsertMailbox(db.Mailbox{AccountID: accA, Name: "INBOX", DisplayName: "Inbox", Delimiter: "."})
	database.UpsertMailbox(db.Mailbox{AccountID: accA, Name: "INBOX.Sent", DisplayName: "Sent", Delimiter: "."})
	bInbox, _ := database.UpsertMailbox(db.Mailbox{AccountID: accB, Name: "INBOX", DisplayName: "Inbox", Delimiter: "/"})
	database.UpsertMessage(db.Message{MailboxID: aInbox, UID: 1, From: "newsletter@substack.com", Subject: "Weekly"})
	database.UpsertMessage(db.Message{MailboxID: bInbox, UID: 1, From: "newsletter@substack.com", Subject: "Weekly"})

	cfg := config.DefaultConfig()
	cfg.Accounts = []config.AccountConfig{{Name: "A"}, {Name: "B"}}
	m := NewModel(database, cfg, "dev", false)
	next, _ := m.Update(AccountsLoadedMsg{
		Accounts:  []db.Account{{ID: accA, Name: "A"}, {ID: accB, Name: "B"}},
		Mailboxes: append(mustListMailboxes(t, database, accA), mustListMailboxes(t, database, accB)...),
	})
	return next.(Model), database, accA, accB
}

func saveSubstackRule(t *testing.T, database *db.DB, accountID int64) int64 {
	t.Helper()
	rule := filter.Rule{
		Match:      filter.MatchAll,
		Conditions: []filter.Condition{{Field: filter.FieldFrom, Op: filter.OpContains, Value: "substack.com"}},
		Action:     filter.Action{Type: filter.ActionMove, Target: "Reading"},
	}
	blob, _ := json.Marshal(rule)
	id, err := database.UpsertRule(db.RuleRecord{AccountID: accountID, Enabled: true, Name: "substack", JSON: string(blob)})
	if err != nil {
		t.Fatalf("UpsertRule: %v", err)
	}
	return id
}

// An all-accounts rule runs against every account's inbox — the scope picked at
// creation — without needing a mailbox selected in the sidebar.
func TestRunAllAccountsRuleUsesRuleScope(t *testing.T) {
	m, database, _, _ := modelWithTwoAccounts(t)
	saveSubstackRule(t, database, 0) // 0 = all accounts
	m.reloadFilterRules()

	rule := m.selectedRule()
	if rule == nil {
		t.Fatal("expected the saved rule to be selected")
	}
	if got := len(m.filterRunMailboxIDs(rule)); got != 2 {
		t.Fatalf("all-accounts rule covers %d mailboxes, want 2 inboxes", got)
	}

	next, cmd := m.runSelectedRule(true)
	m = next.(Model)
	if cmd == nil {
		t.Fatalf("expected a run command, status was %q", m.filterManager.status)
	}
	run := cmd().(FilterRunMsg)
	if run.Err != nil {
		t.Fatalf("test run: %v", run.Err)
	}
	if run.Matched != 2 {
		t.Fatalf("matched %d, want 2 (one per account inbox)", run.Matched)
	}
}

// A rule scoped to one account only covers that account's inbox.
func TestRunScopedRuleCoversOneAccount(t *testing.T) {
	m, database, accA, _ := modelWithTwoAccounts(t)
	saveSubstackRule(t, database, accA)
	m.reloadFilterRules()

	rule := m.selectedRule()
	ids := m.filterRunMailboxIDs(rule)
	if len(ids) != 1 {
		t.Fatalf("scoped rule covers %d mailboxes, want 1", len(ids))
	}
	for _, mb := range m.mailboxes {
		if mb.ID == ids[0] && mb.AccountID != accA {
			t.Fatalf("scoped run reached account %d, want %d", mb.AccountID, accA)
		}
	}
}

// Only the highlighted rule runs, so its match count is not inflated by others.
func TestRunSelectedRuleIgnoresOtherRules(t *testing.T) {
	m, database, _, _ := modelWithTwoAccounts(t)
	first := saveSubstackRule(t, database, 0)

	// A second, broader rule that would match the same mail.
	other := filter.Rule{
		Match:      filter.MatchAll,
		Conditions: []filter.Condition{{Field: filter.FieldSubject, Op: filter.OpContains, Value: "Weekly"}},
		Action:     filter.Action{Type: filter.ActionMarkRead},
	}
	blob, _ := json.Marshal(other)
	database.UpsertRule(db.RuleRecord{AccountID: 0, Priority: 1, Enabled: true, Name: "weekly", JSON: string(blob)})
	m.reloadFilterRules()

	rule := m.ruleByID(first)
	if rule == nil {
		t.Fatal("saved rule not found")
	}
	cmd := m.applyRulesCmd(m.filterRunMailboxIDs(rule), true, first)
	run := cmd().(FilterRunMsg)
	if run.Matched != 2 {
		t.Fatalf("matched %d, want 2 from the selected rule alone", run.Matched)
	}
}

// A disabled rule reports why instead of silently doing nothing.
func TestRunDisabledRuleExplains(t *testing.T) {
	m, database, _, _ := modelWithTwoAccounts(t)
	id := saveSubstackRule(t, database, 0)
	database.SetRuleEnabled(id, false)
	m.reloadFilterRules()

	next, cmd := m.runSelectedRule(false)
	m = next.(Model)
	if cmd != nil {
		t.Fatal("a disabled rule should not run")
	}
	if m.filterManager.status == "" || m.filterManager.status == "no rule selected" {
		t.Fatalf("status = %q, want a disabled-rule explanation", m.filterManager.status)
	}
}
