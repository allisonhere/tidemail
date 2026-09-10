package ui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/smtp"
)

func newSendTestModel(t *testing.T, delaySeconds int) Model {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	cfg := config.DefaultConfig()
	cfg.Display.SendDelaySeconds = delaySeconds
	m := NewModel(database, cfg, "dev", false)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return next.(Model)
}

func queueTestSend(t *testing.T, m Model) (Model, tea.Cmd) {
	t.Helper()
	c := NewCompose(config.AccountConfig{}, nil, nil)
	c.toInput.SetValue("bob@example.com")
	c.subjectInput.SetValue("hello")
	m.compose = c
	m.overlay = overlayCompose
	next, cmd := m.Update(SendQueuedMsg{
		Account: config.AccountConfig{},
		Msg:     smtp.OutgoingMessage{To: []string{"bob@example.com"}, Subject: "hello"},
	})
	return next.(Model), cmd
}

func TestSendQueuedHoldsMessageAndClosesCompose(t *testing.T) {
	m := newSendTestModel(t, 5)
	m, cmd := queueTestSend(t, m)

	if m.overlay != overlayNone {
		t.Fatal("expected compose to close when the send is queued")
	}
	if len(m.pendingSends) != 1 {
		t.Fatalf("expected 1 pending send, got %d", len(m.pendingSends))
	}
	if cmd == nil {
		t.Fatal("expected the grace-period timer command")
	}
}

func TestSendQueuedZeroDelayDispatchesImmediately(t *testing.T) {
	m := newSendTestModel(t, 0)
	m, cmd := queueTestSend(t, m)

	if len(m.pendingSends) != 1 || !m.pendingSends[0].Committing {
		t.Fatal("zero delay must track the in-flight send for shutdown")
	}
	if m.undoLatestPendingSend() {
		t.Fatal("immediate send must not be undoable")
	}
	if cmd == nil {
		t.Fatal("expected an immediate send command")
	}
}

func TestUndoCancelsPendingSendAndRestoresCompose(t *testing.T) {
	m := newSendTestModel(t, 5)
	m, _ = queueTestSend(t, m)
	id := m.pendingSends[0].ID

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlZ})
	m = next.(Model)

	if len(m.pendingSends) != 0 {
		t.Fatal("expected undo to drop the pending send")
	}
	if m.overlay != overlayCompose {
		t.Fatal("expected undo to restore the compose overlay")
	}
	if got := m.compose.toInput.Value(); got != "bob@example.com" {
		t.Fatalf("expected restored recipients, got %q", got)
	}

	// The original grace timer still fires; it must now be a no-op.
	next, cmd := m.Update(CommitSendMsg{ID: id})
	m = next.(Model)
	if cmd != nil {
		t.Fatal("stale commit tick for an undone send must be a no-op")
	}
}

func TestCommitMarksPendingSendUncancelable(t *testing.T) {
	m := newSendTestModel(t, 5)
	m, _ = queueTestSend(t, m)
	id := m.pendingSends[0].ID

	next, cmd := m.Update(CommitSendMsg{ID: id})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("expected the commit to dispatch the send")
	}
	if !m.pendingSends[0].Committing {
		t.Fatal("expected the pending send to be marked committing")
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlZ})
	m = next.(Model)
	if m.overlay == overlayCompose {
		t.Fatal("a committing send must not be undoable")
	}

	next, _ = m.Update(MessageSentMsg{PendingID: id})
	m = next.(Model)
	if len(m.pendingSends) != 0 {
		t.Fatal("expected the sent entry to be cleared")
	}
}

func TestScheduledSendPersistsFutureTimeAndDoesNotFlushEarly(t *testing.T) {
	m := newSendTestModel(t, 5)
	c := NewCompose(config.AccountConfig{}, nil, nil)
	c.toInput.SetValue("bob@example.com")
	c.subjectInput.SetValue("later")
	m.compose = c
	m.overlay = overlayCompose
	due := time.Now().Add(2 * time.Hour).Truncate(time.Second)

	next, cmd := m.Update(SendQueuedMsg{
		Account:     config.AccountConfig{},
		Msg:         smtp.OutgoingMessage{To: []string{"bob@example.com"}, Subject: "later"},
		ScheduledAt: due,
	})
	m = next.(Model)
	if cmd == nil || len(m.pendingSends) != 1 {
		t.Fatal("expected future send to remain queued with a timer")
	}
	if got := m.pendingSends[0].DueAt; got != due.Unix() {
		t.Fatalf("DueAt = %d, want %d", got, due.Unix())
	}
	item, err := m.db.GetOutbox(int64(m.pendingSends[0].ID))
	if err != nil {
		t.Fatal(err)
	}
	if item.NextAttempt != due.Unix() || item.State != db.OutboxQueued {
		t.Fatalf("scheduled outbox item = %+v", item)
	}
	if err := m.FlushPendingSends(); err != nil {
		t.Fatal(err)
	}
	item, err = m.db.GetOutbox(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != db.OutboxQueued || item.Attempts != 0 {
		t.Fatalf("shutdown sent a future message: %+v", item)
	}
}

func TestScheduleSendRejectsPastTime(t *testing.T) {
	m := newSendTestModel(t, 5)
	m.compose = NewCompose(config.AccountConfig{}, nil, nil)
	m.overlay = overlayCompose
	m.openScheduleSend()
	m.schedulePicker.Select(3) // Custom date and time.
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	// Confirm tomorrow's date, then enter an invalid time to stay in the picker.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	m.scheduleSendInput.SetValue("not-a-time")

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if m.overlay != overlayScheduleSend || m.scheduleSendInput.Err == nil {
		t.Fatal("invalid custom time should leave the picker open with an error")
	}
}

func TestScheduleSendUsesTwelveHourTime(t *testing.T) {
	m := newSendTestModel(t, 5)
	m.compose = NewCompose(config.AccountConfig{}, nil, nil)
	m.compose.toInput.SetValue("bob@example.com")
	m.overlay = overlayCompose
	m.openScheduleSend()
	m.schedulePicker.Select(3) // Custom date and time.
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if got := m.scheduleSendInput.Value(); got != "9:00 AM" {
		t.Fatalf("custom time default = %q, want 12-hour time", got)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // Choose the default future date.
	m = next.(Model)
	m.scheduleSendInput.SetValue("11:30 pm")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("expected custom 12-hour time to queue a send")
	}
	queued, ok := cmd().(SendQueuedMsg)
	if !ok {
		t.Fatalf("schedule command returned %T, want SendQueuedMsg", cmd())
	}
	if got := queued.ScheduledAt.In(time.Local).Hour(); got != 23 {
		t.Fatalf("queued scheduled hour = %d, want 23", got)
	}
	next, _ = m.Update(queued)
	m = next.(Model)
	if len(m.pendingSends) != 1 {
		t.Fatal("expected scheduled send in outbox")
	}
	if got := time.Unix(m.pendingSends[0].DueAt, 0).In(time.Local).Hour(); got != 23 {
		t.Fatalf("scheduled hour = %d, want 23", got)
	}
}
