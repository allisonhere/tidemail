package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	tea "github.com/charmbracelet/bubbletea"
)

type undoTestFixture struct {
	model    Model
	database *db.DB
	source   db.Mailbox
	target   db.Mailbox
	messages []db.Message
}

func newUndoTestFixture(t *testing.T, count int) undoTestFixture {
	t.Helper()
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	accountName := "undo-" + strings.ReplaceAll(t.Name(), "/", "-")
	accountID, err := database.AddAccount(accountName, "")
	if err != nil {
		t.Fatal(err)
	}
	sourceID, err := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "INBOX"})
	if err != nil {
		t.Fatal(err)
	}
	targetID, err := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "Archive", Flags: []string{"\\Archive"}})
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= count; i++ {
		if err := database.UpsertMessage(db.Message{MailboxID: sourceID, UID: uint32(i), Subject: "undo message", Read: i%2 == 0}); err != nil {
			t.Fatal(err)
		}
	}
	messages, err := database.ListMessages(sourceID)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Accounts = []config.AccountConfig{{Name: accountName}}
	m := NewModel(database, cfg, "dev", false)
	m.accounts = []db.Account{{ID: accountID, Name: accountName}}
	m.sourceMailboxForUndoTest(sourceID, targetID, accountID, int64(count))
	m.messages = append([]db.Message(nil), messages...)
	m.applyFilter()
	return undoTestFixture{model: m, database: database, source: m.mailboxes[0], target: m.mailboxes[1], messages: messages}
}

func (m *Model) sourceMailboxForUndoTest(sourceID, targetID, accountID, unread int64) {
	m.mailboxes = []db.Mailbox{
		{ID: sourceID, AccountID: accountID, Name: "INBOX", UnreadCount: unread},
		{ID: targetID, AccountID: accountID, Name: "Archive", Flags: []string{"\\Archive"}},
	}
}

func TestPendingDeleteHidesWithoutTouchingDatabaseAndUndoRestores(t *testing.T) {
	f := newUndoTestFixture(t, 1)
	m := f.model
	cmd := m.scheduleDelete(f.messages)
	if cmd == nil {
		t.Fatal("expected delayed commit timer")
	}
	if len(m.filteredMessages) != 0 {
		t.Fatalf("expected pending message hidden, got %d visible", len(m.filteredMessages))
	}
	stored, err := f.database.GetMessage(f.messages[0].ID)
	if err != nil || stored.MailboxID != f.source.ID {
		t.Fatalf("pending action changed database before commit: message=%+v err=%v", stored, err)
	}
	if got := m.destructiveUndoPrompt(); !strings.Contains(got, "deleted") || !strings.Contains(got, "ctrl+z undo") {
		t.Fatalf("unexpected undo prompt %q", got)
	}

	m.undoLatestDestructive()
	if len(m.filteredMessages) != 1 {
		t.Fatalf("expected undo to restore message, got %d visible", len(m.filteredMessages))
	}
	if len(m.pendingDestructiveActions) != 0 {
		t.Fatal("expected pending action removed after undo")
	}
}

func TestPendingMessagesStayHiddenAfterReload(t *testing.T) {
	f := newUndoTestFixture(t, 1)
	m := f.model
	m.scheduleDelete(f.messages)
	reloaded, err := f.database.ListMessages(f.source.ID)
	if err != nil {
		t.Fatal(err)
	}
	m.messages = reloaded
	m.applyFilter()
	if len(m.filteredMessages) != 0 {
		t.Fatal("database reload exposed a pending destructive message")
	}
}

func TestUndoStackRestoresNewestBatchFirst(t *testing.T) {
	f := newUndoTestFixture(t, 2)
	m := f.model
	m.scheduleDelete(f.messages[:1])
	m.scheduleMove(f.messages[1:], f.target)
	if len(m.pendingDestructiveActions) != 2 {
		t.Fatalf("expected two pending actions, got %d", len(m.pendingDestructiveActions))
	}

	next, _ := m.handleMainKey(tea.KeyMsg{Type: tea.KeyCtrlZ})
	m = next.(Model)
	if len(m.pendingDestructiveActions) != 1 || m.pendingDestructiveActions[0].Kind != destructiveDelete {
		t.Fatalf("expected newest move undone first, got %+v", m.pendingDestructiveActions)
	}
	if len(m.filteredMessages) != 1 || m.filteredMessages[0].ID != f.messages[1].ID {
		t.Fatalf("expected only newest batch restored, got %+v", m.filteredMessages)
	}
}

func TestExpiredPendingMoveCommitsLocalState(t *testing.T) {
	f := newUndoTestFixture(t, 1)
	m := f.model
	m.scheduleMove(f.messages, f.target)
	id := m.pendingDestructiveActions[0].ID
	cmd := m.beginDestructiveCommit(id)
	if cmd == nil {
		t.Fatal("expected commit command")
	}
	result := cmd().(DestructiveActionResultMsg)
	next, _ := m.Update(result)
	m = next.(Model)
	stored, err := f.database.GetMessage(f.messages[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.MailboxID != f.target.ID {
		t.Fatalf("expected committed target mailbox %d, got %d", f.target.ID, stored.MailboxID)
	}
	if len(m.pendingDestructiveActions) != 0 || len(m.messages) != 0 {
		t.Fatal("expected committed action and source message removed from the live model")
	}
}

func TestFailedLocalMoveRestoresPendingMessage(t *testing.T) {
	f := newUndoTestFixture(t, 1)
	m := f.model
	invalid := db.Mailbox{ID: 999999, AccountID: f.source.AccountID, Name: "Missing"}
	m.scheduleMove(f.messages, invalid)
	cmd := m.beginDestructiveCommit(m.pendingDestructiveActions[0].ID)
	result := cmd().(DestructiveActionResultMsg)
	if len(result.Failed) != 1 {
		t.Fatalf("expected failed message returned, got %+v", result)
	}
	next, _ := m.Update(result)
	m = next.(Model)
	if len(m.filteredMessages) != 1 {
		t.Fatal("expected failed move to restore message visibility")
	}
}

func TestPendingUnreadCountsAreDerivedAndUndoable(t *testing.T) {
	f := newUndoTestFixture(t, 1)
	m := f.model
	m.scheduleDelete(f.messages)
	if got := m.displayMailboxUnreadCount(f.source); got != 0 {
		t.Fatalf("expected pending unread removed from display count, got %d", got)
	}
	m.undoLatestDestructive()
	if got := m.displayMailboxUnreadCount(f.source); got != 1 {
		t.Fatalf("expected undo to restore unread display count, got %d", got)
	}
}

func TestFlushPendingActionsCommitsBeforeExit(t *testing.T) {
	f := newUndoTestFixture(t, 1)
	m := f.model
	m.scheduleMove(f.messages, f.target)
	if err := m.FlushPendingDestructiveActions(); err != nil {
		t.Fatal(err)
	}
	stored, err := f.database.GetMessage(f.messages[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.MailboxID != f.target.ID {
		t.Fatalf("expected exit flush to commit target %d, got %d", f.target.ID, stored.MailboxID)
	}
}

func TestUndoneTimerMessageIsHarmless(t *testing.T) {
	f := newUndoTestFixture(t, 1)
	m := f.model
	m.scheduleDelete(f.messages)
	id := m.pendingDestructiveActions[0].ID
	m.undoLatestDestructive()
	if cmd := m.beginDestructiveCommit(id); cmd != nil {
		t.Fatal("expected stale timer for undone action to be ignored")
	}
}

func TestFlushWaitsForCommittingAction(t *testing.T) {
	runtime := &destructiveActionRuntime{done: make(chan struct{})}
	m := Model{pendingDestructiveActions: []pendingDestructiveAction{{ID: 1, Committing: true, Runtime: runtime}}}
	flushed := make(chan error, 1)
	go func() { flushed <- m.FlushPendingDestructiveActions() }()

	select {
	case <-flushed:
		t.Fatal("flush returned before the committing action finished")
	case <-time.After(20 * time.Millisecond):
	}
	close(runtime.done)
	select {
	case err := <-flushed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("flush did not return after the committing action finished")
	}
}
