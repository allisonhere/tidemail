package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	"github.com/allisonhere/tide/internal/smtp"
)

func newSendTestModel(t *testing.T, delaySeconds int) Model {
	t.Helper()
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

	if len(m.pendingSends) != 0 {
		t.Fatal("zero delay must not park the message")
	}
	if cmd == nil {
		t.Fatal("expected an immediate send command")
	}
}

func TestUndoCancelsPendingSendAndRestoresCompose(t *testing.T) {
	m := newSendTestModel(t, 5)
	m, _ = queueTestSend(t, m)

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
	next, cmd := m.Update(CommitSendMsg{ID: 1})
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
