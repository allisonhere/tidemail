package ui

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/filter"
)

// Mail that a rule moves/deletes/etc. on arrival must be dropped from the
// "new messages" set so it is neither counted nor notified about.
func TestApplyRulesOnArrivalFiltersNotifications(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	accountID, _ := database.AddAccount("Personal", "")
	inboxID, _ := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "INBOX", Delimiter: "/"})
	readingID, _ := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "Reading", Delimiter: "/"})

	match := db.Message{MailboxID: inboxID, UID: 1, From: "newsletter@substack.com", Subject: "Weekly"}
	keep := db.Message{MailboxID: inboxID, UID: 2, From: "boss@work.com", Subject: "Report"}
	database.UpsertMessage(match)
	database.UpsertMessage(keep)

	rule := filter.Rule{
		Match:      filter.MatchAll,
		Conditions: []filter.Condition{{Field: filter.FieldFrom, Op: filter.OpContains, Value: "substack.com"}},
		Action:     filter.Action{Type: filter.ActionMove, Target: "Reading"},
	}
	blob, _ := json.Marshal(rule)
	database.UpsertRule(db.RuleRecord{AccountID: accountID, Enabled: true, Name: "substack", JSON: string(blob)})

	mailbox, _ := database.GetMailbox(inboxID)
	// client nil => local-only (no IMAP), so the move happens in the DB.
	survivors, err := applyRulesOnArrival(context.Background(), database, nil, mailbox, []db.Message{match, keep})
	if err != nil {
		t.Fatalf("applyRulesOnArrival: %v", err)
	}
	if len(survivors) != 1 || survivors[0].UID != 2 {
		t.Fatalf("expected only the non-matching message to survive, got %+v", survivors)
	}

	// The matched message must actually be in Reading now, not INBOX.
	readingMsgs, _ := database.ListMessages(readingID)
	if len(readingMsgs) != 1 || readingMsgs[0].UID != 1 {
		t.Fatalf("matched message should have moved to Reading, got %+v", readingMsgs)
	}
}
