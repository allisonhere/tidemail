package db

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

func outboxTestItem(t *testing.T, database *DB) int64 {
	t.Helper()
	draft := Draft{AccountName: "Personal", AccountUser: "alice@example.com", To: "bob@example.com", BCC: "hidden@example.com", BodyText: "body", Dirty: true,
		Attachments: []DraftAttachment{{Filename: "report.txt", Data: []byte("report")}}}
	id, err := database.SaveDraft(draft)
	if err != nil {
		t.Fatal(err)
	}
	draft.ID = id
	payload, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	id, err = database.EnqueueOutbox(OutboxItem{AccountName: draft.AccountName, AccountUser: draft.AccountUser, DraftID: id, MessageJSON: []byte(`{"Body":"body"}`), DraftJSON: payload, MaxAttempts: 3})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestOutboxSurvivesReopenAndHidesQueuedDraft(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mail.db")
	database, err := openSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.init(); err != nil {
		t.Fatal(err)
	}
	id := outboxTestItem(t, database)
	if n, err := database.DraftCount("Personal", "alice@example.com"); err != nil || n != 0 {
		t.Fatalf("queued draft visible: %d %v", n, err)
	}
	if drafts, err := database.ListDrafts("Personal", "alice@example.com"); err != nil || len(drafts) != 0 {
		t.Fatalf("drafts: %v %v", drafts, err)
	}
	database.Close()
	database, err = openSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.init(); err != nil {
		t.Fatal(err)
	}
	item, err := database.GetOutbox(id)
	if err != nil || item.State != OutboxQueued || item.MaxAttempts != 3 {
		t.Fatalf("restored queue: %+v %v", item, err)
	}
	var draft Draft
	if err := json.Unmarshal(item.DraftJSON, &draft); err != nil {
		t.Fatal(err)
	}
	if draft.BCC != "hidden@example.com" || len(draft.Attachments) != 1 || string(draft.Attachments[0].Data) != "report" {
		t.Fatalf("lost content: %+v", draft)
	}
}

func TestOutboxClaimLimitsAndRecovery(t *testing.T) {
	database := openDraftTestDB(t)
	id := outboxTestItem(t, database)
	for attempt := 1; attempt <= 3; attempt++ {
		if ok, err := database.ClaimOutbox(id); err != nil || !ok {
			t.Fatalf("claim: %v %v", ok, err)
		}
		if ok, err := database.ClaimOutbox(id); err != nil || ok {
			t.Fatalf("duplicate claim: %v %v", ok, err)
		}
		if _, err := database.EditOutbox(id); !errors.Is(err, ErrOutboxBusy) {
			t.Fatalf("edited sending: %v", err)
		}
		if err := database.DeleteOutbox(id); !errors.Is(err, ErrOutboxBusy) {
			t.Fatalf("deleted sending: %v", err)
		}
		if err := database.FinishOutbox(id, OutboxQueued, "temporary failure", 100); err != nil {
			t.Fatal(err)
		}
	}
	if ok, err := database.ClaimOutbox(id); err != nil || ok {
		t.Fatalf("exceeded limit: %v %v", ok, err)
	}
	if err := database.FailQueuedOutbox(id, "failed"); err != nil {
		t.Fatal(err)
	}
	if err := database.RetryOutbox(id, 2); err != nil {
		t.Fatal(err)
	}
	item, err := database.GetOutbox(id)
	if err != nil || item.Attempts != 0 || item.MaxAttempts != 2 {
		t.Fatalf("reset: %+v %v", item, err)
	}
	database.ClaimOutbox(id)
	if err := database.RecoverOutbox(); err != nil {
		t.Fatal(err)
	}
	item, err = database.GetOutbox(id)
	if err != nil || item.State != OutboxUncertain || item.Attempts != 1 {
		t.Fatalf("recovery: %+v %v", item, err)
	}
	if ok, err := database.ClaimOutbox(id); err != nil || ok {
		t.Fatalf("uncertain message sent automatically: %v %v", ok, err)
	}
}

func TestOutboxEditMovesToDraftAtomically(t *testing.T) {
	database := openDraftTestDB(t)
	id := outboxTestItem(t, database)
	draft, err := database.EditOutbox(id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.GetOutbox(id); err == nil {
		t.Fatal("edited item remains queued")
	}
	restored, err := database.GetDraft(draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.BCC != "hidden@example.com" || len(restored.Attachments) != 1 || string(restored.Attachments[0].Data) != "report" {
		t.Fatalf("lost content: %+v", restored)
	}
	if n, err := database.DraftCount("Personal", "alice@example.com"); err != nil || n != 1 {
		t.Fatalf("restored draft invisible: %d %v", n, err)
	}
	if ok, err := database.ClaimOutbox(id); err != nil || ok {
		t.Fatalf("edited item still sendable: %v %v", ok, err)
	}
}

func TestOutboxEditFailureKeepsQueue(t *testing.T) {
	database := openDraftTestDB(t)
	id := outboxTestItem(t, database)
	if _, err := database.Exec(`CREATE TRIGGER fail_restore BEFORE UPDATE ON drafts BEGIN SELECT RAISE(ABORT, 'disk full'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.EditOutbox(id); err == nil {
		t.Fatal("expected restore failure")
	}
	if item, err := database.GetOutbox(id); err != nil || item.State != OutboxQueued {
		t.Fatalf("lost queue on restore failure: %+v %v", item, err)
	}
}

func TestSentOutboxDropsMessagePayload(t *testing.T) {
	database := openDraftTestDB(t)
	id := outboxTestItem(t, database)
	if ok, err := database.ClaimOutbox(id); err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	if err := database.FinishOutbox(id, OutboxSent, "", 0); err != nil {
		t.Fatal(err)
	}
	item, err := database.GetOutbox(id)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != OutboxSent || len(item.MessageJSON) != 0 || len(item.DraftJSON) != 0 {
		t.Fatalf("sent payload retained: %+v", item)
	}
}
