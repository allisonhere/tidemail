package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
)

// uncertainOutboxTestModel queues a real send, then simulates the app
// restarting mid-delivery (ClaimOutbox + RecoverOutbox, exactly what
// RecoverOutbox's own package test uses) to get a genuine OutboxUncertain
// row: the server may have taken the message, so the state isn't a plain
// failure.
func uncertainOutboxTestModel(t *testing.T) (Model, int64) {
	t.Helper()
	account := config.AccountConfig{Name: "Personal", User: "alice@example.com"}
	m := newSendTestModel(t, 5)
	m.cfg.Accounts = []config.AccountConfig{account}
	m, _ = queueTestSendFrom(t, m, account)
	items, err := m.db.ListOutbox()
	if err != nil || len(items) != 1 {
		t.Fatalf("ListOutbox: %+v %v", items, err)
	}
	id := items[0].ID
	if ok, err := m.db.ClaimOutbox(id); err != nil || !ok {
		t.Fatalf("ClaimOutbox: %v %v", ok, err)
	}
	if err := m.db.RecoverOutbox(); err != nil {
		t.Fatalf("RecoverOutbox: %v", err)
	}
	item, err := m.db.GetOutbox(id)
	if err != nil || item.State != db.OutboxUncertain {
		t.Fatalf("expected an uncertain item, got %+v (%v)", item, err)
	}
	m.openOutbox()
	m.outboxCursor = 0
	return m, id
}

// Regression for issue audit #1: e/enter on an uncertain delivery used to
// silently turn it into a draft and delete the outbox row with no warning,
// unlike r (retry), which already confirms. If the first attempt actually
// reached the server, editing then resending duplicates the mail with no
// record that an unconfirmed delivery ever existed.
func TestOutboxEditOnUncertainRequiresConfirmation(t *testing.T) {
	m, id := uncertainOutboxTestModel(t)

	next, _ := m.handleOutboxKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	got := next.(Model)
	if got.outboxConfirmID != id || !got.outboxConfirmEdit {
		t.Fatalf("expected a pending edit confirmation for id %d, got confirmID=%d confirmEdit=%v",
			id, got.outboxConfirmID, got.outboxConfirmEdit)
	}
	if got.overlay == overlayCompose {
		t.Fatal("edit must not open compose before confirmation")
	}
	if _, err := got.db.GetOutbox(id); err != nil {
		t.Fatalf("outbox row must survive until confirmed: %v", err)
	}

	next, _ = got.handleOutboxKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	got = next.(Model)
	if got.overlay != overlayCompose {
		t.Fatalf("expected compose to open after confirming, got overlay %v", got.overlay)
	}
	if _, err := got.db.GetOutbox(id); err == nil {
		t.Fatal("expected EditOutbox to have removed the outbox row")
	}
}

func TestOutboxEditOnUncertainCancelsOnNo(t *testing.T) {
	m, id := uncertainOutboxTestModel(t)

	next, _ := m.handleOutboxKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	got := next.(Model)

	next, _ = got.handleOutboxKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	got = next.(Model)
	if got.outboxConfirmID != 0 || got.outboxConfirmEdit {
		t.Fatalf("expected the pending confirmation to clear, got confirmID=%d confirmEdit=%v",
			got.outboxConfirmID, got.outboxConfirmEdit)
	}
	if got.overlay == overlayCompose {
		t.Fatal("canceling must not open compose")
	}
	if item, err := got.db.GetOutbox(id); err != nil || item.State != db.OutboxUncertain {
		t.Fatalf("expected the uncertain row to survive a canceled edit, got %+v (%v)", item, err)
	}
}

// A non-uncertain item (queued/failed) still edits immediately, with no
// confirmation step — only the uncertain case needs the extra warning.
func TestOutboxEditOnQueuedSkipsConfirmation(t *testing.T) {
	account := config.AccountConfig{Name: "Personal", User: "alice@example.com"}
	m := newSendTestModel(t, 5)
	m.cfg.Accounts = []config.AccountConfig{account}
	m, _ = queueTestSendFrom(t, m, account)
	items, err := m.db.ListOutbox()
	if err != nil || len(items) != 1 {
		t.Fatalf("ListOutbox: %+v %v", items, err)
	}
	m.openOutbox()
	m.outboxCursor = 0

	next, _ := m.handleOutboxKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	got := next.(Model)
	if got.overlay != overlayCompose {
		t.Fatalf("expected compose to open immediately for a queued item, got overlay %v", got.overlay)
	}
	if got.outboxConfirmID != 0 {
		t.Fatal("a queued item must not require confirmation to edit")
	}
}

// Regression for issue audit #2: smtp.ErrDeliveryUncertain means the server
// may have already taken the message, which is why CanRetry refuses to
// retry it automatically. Calling that "send failed" in the transient status
// line (distinct from the Outbox pane's own "unconfirmed" wording) invites a
// needless retry and a duplicate if delivery actually succeeded.
func TestHandleOutboxSentReportsUncertainAsUnconfirmedNotFailed(t *testing.T) {
	m, id := uncertainOutboxTestModel(t)

	next, _ := m.Update(MessageSentMsg{PendingID: uint64(id), Err: errors.New("network timeout")})
	got := next.(Model)
	if !strings.Contains(got.statusMsg, "unconfirmed") {
		t.Fatalf("expected the status to call this unconfirmed, got %q", got.statusMsg)
	}
	if strings.Contains(got.statusMsg, "failed") {
		t.Fatalf("expected the status to NOT call an uncertain send failed, got %q", got.statusMsg)
	}
}
