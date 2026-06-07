package ui

import (
	"encoding/json"
	"testing"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	"github.com/allisonhere/tide/internal/filter"
)

// A generated rule must be saved scoped to the account whose folders validated
// its move target, not as a global (AccountID 0) rule.
func TestSaveDraftRuleScopesToGenerationAccount(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	accountID, _ := database.AddAccount("Personal", "")
	m := NewModel(database, config.DefaultConfig(), "dev", false)
	m.filterManager = m.newFilterManager()
	m.filterManager.draft = filter.Rule{
		Match:      filter.MatchAll,
		Conditions: []filter.Condition{{Field: filter.FieldFrom, Op: filter.OpContains, Value: "x"}},
		Action:     filter.Action{Type: filter.ActionMove, Target: "Reading"},
	}
	m.filterManager.draftEn = "move x to Reading"
	m.filterManager.draftAcct = accountID

	m.saveDraftRule()

	rules, _ := database.ListRules()
	if len(rules) != 1 {
		t.Fatalf("expected 1 saved rule, got %d", len(rules))
	}
	if rules[0].AccountID != accountID {
		t.Fatalf("rule AccountID = %d, want %d (scoped to generation account)", rules[0].AccountID, accountID)
	}
}

// A rule scoped to account A must not match (or create folders in) account B.
func TestScopedRuleDoesNotTouchOtherAccount(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	accA, _ := database.AddAccount("A", "")
	accB, _ := database.AddAccount("B", "")
	database.UpsertMailbox(db.Mailbox{AccountID: accA, Name: "INBOX", Delimiter: "/"})
	database.UpsertMailbox(db.Mailbox{AccountID: accA, Name: "Reading", Delimiter: "/"})
	bInbox, _ := database.UpsertMailbox(db.Mailbox{AccountID: accB, Name: "INBOX", Delimiter: "/"})
	// Matching mail, but in account B.
	database.UpsertMessage(db.Message{MailboxID: bInbox, UID: 1, From: "newsletter@substack.com", Subject: "Weekly"})

	rule := filter.Rule{
		Match:      filter.MatchAll,
		Conditions: []filter.Condition{{Field: filter.FieldFrom, Op: filter.OpContains, Value: "substack.com"}},
		Action:     filter.Action{Type: filter.ActionMove, Target: "Reading"},
	}
	blob, _ := json.Marshal(rule)
	// Scoped to account A only.
	database.UpsertRule(db.RuleRecord{AccountID: accA, Enabled: true, Name: "substack", JSON: string(blob)})

	cfg := config.DefaultConfig()
	cfg.Accounts = []config.AccountConfig{{Name: "A"}, {Name: "B"}}
	m := NewModel(database, cfg, "dev", false)
	next, _ := m.Update(AccountsLoadedMsg{
		Accounts:  []db.Account{{ID: accA, Name: "A"}, {ID: accB, Name: "B"}},
		Mailboxes: append(mustListMailboxes(t, database, accA), mustListMailboxes(t, database, accB)...),
	})
	m = next.(Model)

	run := m.applyRulesCmd([]int64{bInbox}, false)().(FilterRunMsg)
	if run.Matched != 0 || run.Applied != 0 {
		t.Fatalf("account-A rule should not act on account-B mail: matched=%d applied=%d", run.Matched, run.Applied)
	}
	// B must not have gained a "Reading" folder.
	bMbs, _ := database.ListMailboxes(accB)
	for _, mb := range bMbs {
		if mb.Name == "Reading" {
			t.Fatalf("account B should not have a Reading folder created")
		}
	}
}
