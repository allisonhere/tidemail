package ui

import (
	"encoding/json"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/smtp"
)

// Regression tests for the second half of issue #25. The sender used to be
// baked into message_json when the message was queued, and every retry replayed
// it — so an account saved with a bad From kept failing after the account was
// corrected, and the reporter's only way out was to edit the queued row or
// compose the message again.

func queuedItem(t *testing.T, msg smtp.OutgoingMessage) db.OutboxItem {
	t.Helper()
	payload, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}
	return db.OutboxItem{MessageJSON: payload}
}

// The case from the issue: "David Blangstrup" was queued, the account has since
// been fixed, and the retry has to use the fix.
func TestOutboxMessageReresolvesCorrectedSender(t *testing.T) {
	item := queuedItem(t, smtp.OutgoingMessage{
		From: "David Blangstrup", // no address: what the account held when queued
		To:   []string{"bob@example.com"},
	})
	account := config.AccountConfig{
		From: "David Blangstrup <david@blangstrup.info>", // corrected since
		User: "david@blangstrup.info",
	}

	msg, err := outboxMessage(item, account)
	if err != nil {
		t.Fatalf("outboxMessage: %v", err)
	}
	if msg.From != account.From {
		t.Fatalf("retry sender = %q, want the account's current From %q", msg.From, account.From)
	}
	if got := smtp.BuildRaw(account, msg); !containsHeader(string(got), "From: "+account.From) {
		t.Fatalf("expected the built message to carry the corrected From, got %q", got)
	}
}

// With no From on the account, the login stands in — the same fallback Send
// uses, resolved from the account rather than from the queued copy.
func TestOutboxMessageFallsBackToTheCurrentLogin(t *testing.T) {
	item := queuedItem(t, smtp.OutgoingMessage{From: "stale@old.example"})
	account := config.AccountConfig{User: "alice@example.com"}

	msg, err := outboxMessage(item, account)
	if err != nil {
		t.Fatalf("outboxMessage: %v", err)
	}
	if msg.From != "alice@example.com" {
		t.Fatalf("retry sender = %q, want the account's login", msg.From)
	}
}

// The rest of the queued message is untouched — only the sender is re-resolved.
func TestOutboxMessageKeepsTheQueuedContent(t *testing.T) {
	item := queuedItem(t, smtp.OutgoingMessage{
		From:    "stale@old.example",
		To:      []string{"bob@example.com"},
		CC:      []string{"carol@example.com"},
		Subject: "hello",
		Body:    "text",
	})

	msg, err := outboxMessage(item, config.AccountConfig{User: "alice@example.com"})
	if err != nil {
		t.Fatalf("outboxMessage: %v", err)
	}
	if msg.Subject != "hello" || msg.Body != "text" {
		t.Fatalf("queued content changed: %+v", msg)
	}
	if len(msg.To) != 1 || msg.To[0] != "bob@example.com" || len(msg.CC) != 1 {
		t.Fatalf("queued recipients changed: %+v", msg)
	}
}

func TestOutboxMessageReportsUnreadablePayload(t *testing.T) {
	item := db.OutboxItem{MessageJSON: []byte("not json")}
	if _, err := outboxMessage(item, config.AccountConfig{User: "alice@example.com"}); err == nil {
		t.Fatal("expected a corrupt payload to report an error")
	}
}

// Nothing is baked in at queue time any more, so a message serialized today
// cannot pin tomorrow's sender.
func TestQueuedMessageStoresNoSender(t *testing.T) {
	m := newSendTestModel(t, 5)
	m, _ = queueTestSendFrom(t, m, config.AccountConfig{
		Name: "Personal", User: "alice@example.com", From: "Alice <alice@example.com>",
	})

	items, err := m.db.ListOutbox()
	if err != nil {
		t.Fatalf("ListOutbox: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 queued item, got %d", len(items))
	}
	stored, err := m.db.GetOutbox(items[0].ID)
	if err != nil {
		t.Fatalf("GetOutbox: %v", err)
	}
	var msg smtp.OutgoingMessage
	if err := json.Unmarshal(stored.MessageJSON, &msg); err != nil {
		t.Fatalf("unmarshal stored message: %v", err)
	}
	if msg.From != "" {
		t.Fatalf("queued message pinned a sender %q; it must be resolved at send time", msg.From)
	}
}

func containsHeader(raw, header string) bool {
	for _, line := range splitLines(raw) {
		if line == header {
			return true
		}
	}
	return false
}

func splitLines(raw string) []string {
	var out []string
	start := 0
	for i := 0; i+1 < len(raw); i++ {
		if raw[i] == '\r' && raw[i+1] == '\n' {
			out = append(out, raw[start:i])
			start = i + 2
		}
	}
	return append(out, raw[start:])
}
