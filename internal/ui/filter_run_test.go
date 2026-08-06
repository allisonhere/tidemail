package ui

import (
	"encoding/json"
	"testing"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	"github.com/allisonhere/tide/internal/filter"
)

func TestApplyRulesMovesMatchingMail(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	accountID, _ := database.AddAccount("Personal", "")
	inboxID, _ := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "INBOX", Delimiter: "/"})
	readingID, _ := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "Reading", Delimiter: "/"})

	// Two messages: one matches the rule (substack), one does not.
	if err := database.UpsertMessage(db.Message{MailboxID: inboxID, UID: 1, From: "newsletter@substack.com", Subject: "Weekly"}); err != nil {
		t.Fatalf("seed 1: %v", err)
	}
	if err := database.UpsertMessage(db.Message{MailboxID: inboxID, UID: 2, From: "boss@work.com", Subject: "Report"}); err != nil {
		t.Fatalf("seed 2: %v", err)
	}

	rule := filter.Rule{
		Match:      filter.MatchAll,
		Conditions: []filter.Condition{{Field: filter.FieldFrom, Op: filter.OpContains, Value: "substack.com"}},
		Action:     filter.Action{Type: filter.ActionMove, Target: "Reading"},
	}
	blob, _ := json.Marshal(rule)
	if _, err := database.UpsertRule(db.RuleRecord{AccountID: 0, Enabled: true, Name: "substack", JSON: string(blob)}); err != nil {
		t.Fatalf("save rule: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Accounts = []config.AccountConfig{{Name: "Personal"}} // no IMAPHost -> local-only
	m := NewModel(database, cfg, "dev", false)
	next, _ := m.Update(AccountsLoadedMsg{
		Accounts:  []db.Account{{ID: accountID, Name: "Personal"}},
		Mailboxes: mustListMailboxes(t, database, accountID),
	})
	m = next.(Model)

	// Dry run first: should match exactly one, change nothing.
	cmd := m.applyRulesCmd([]int64{inboxID}, true, 0)
	dry := cmd().(FilterRunMsg)
	if dry.Matched != 1 || dry.Applied != 0 {
		t.Fatalf("dry run: matched=%d applied=%d, want 1/0", dry.Matched, dry.Applied)
	}

	// Real run: should move the substack message to Reading.
	cmd = m.applyRulesCmd([]int64{inboxID}, false, 0)
	run := cmd().(FilterRunMsg)
	if run.Err != nil {
		t.Fatalf("run error: %v", run.Err)
	}
	if run.Applied != 1 {
		t.Fatalf("expected 1 applied, got %d", run.Applied)
	}

	inboxMsgs, _ := database.ListMessages(inboxID)
	if len(inboxMsgs) != 1 || inboxMsgs[0].UID != 2 {
		t.Fatalf("inbox should retain only the non-matching message, got %+v", inboxMsgs)
	}
	readingMsgs, _ := database.ListMessages(readingID)
	if len(readingMsgs) != 1 || readingMsgs[0].UID != 1 {
		t.Fatalf("reading should hold the moved message, got %+v", readingMsgs)
	}
}
