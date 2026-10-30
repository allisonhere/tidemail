package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
)

func readPendingModel(t *testing.T) (Model, chan struct{}, chan error) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	database, err := db.OpenPath(t.TempDir() + "/mail.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	entered := make(chan struct{}, 10)
	release := make(chan error, 10)
	t.Cleanup(func() { close(release) })
	cfg := config.DefaultConfig()
	cfg.Display.MarkReadOnFocus = false
	cfg.Display.MarkReadOnOpen = false
	cfg.Display.ThreadedConversations = false
	cfg.Accounts = []config.AccountConfig{{ID: "read-test", Name: "Read test", User: "testuser", Password: "testpass", IMAPHost: "127.0.0.1", IMAPPort: 143}}
	accountID, err := database.AddAccount("read-test", "Read test", "")
	if err != nil {
		t.Fatal(err)
	}
	mailboxID, err := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "INBOX", UnreadCount: 3})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {

		if err := database.UpsertMessage(db.Message{MailboxID: mailboxID, UID: uint32(i + 1), Subject: "Test", Date: time.Unix(int64(100-i), 0)}); err != nil {
			t.Fatal(err)
		}
	}
	m := NewModel(database, cfg, "test", false)
	t.Cleanup(m.sessions.Close)
	// Model the account session lock and a blocked STORE without requiring a
	// TCP listener. The production adapter uses SessionPool.Do/MarkSeenUIDs.
	lock := make(chan struct{}, 1)
	m.readSession = func(ctx context.Context, _ config.AccountConfig, fn func(readStoreFunc) error) error {
		select {
		case lock <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
		defer func() { <-lock }()
		return fn(func(ctx context.Context, _ string, _ []uint32, _ bool) error {
			entered <- struct{}{}
			select {
			case err := <-release:
				return err
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}
	m.accounts = []db.Account{{ID: accountID, ConfigID: "read-test", Name: "Read test"}}
	m.mailboxes = []db.Mailbox{{ID: mailboxID, AccountID: accountID, Name: "INBOX", UnreadCount: 3}}
	m.rebuildSidebar()
	for i, row := range m.sidebarRows {
		if row.kind == rowKindMailbox && row.mailboxID == mailboxID {
			m.sidebarCursor = i
			break
		}
	}
	m.messages, err = database.ListMessages(mailboxID)
	if err != nil {
		t.Fatal(err)
	}
	m.focused = paneMessages
	m.applyFilter()
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)
	return m, entered, release
}

// Execute batches as the runtime does, without executing status timers.
func readRemoteResults(cmd tea.Cmd) <-chan tea.Msg {
	done := make(chan tea.Msg, 10)
	var run func(tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		msg := c()
		if b, ok := msg.(tea.BatchMsg); ok {
			for _, child := range b {
				go run(child)
			}
			return
		}
		if _, ok := msg.(readRemoteCompletedMsg); ok {
			done <- msg
		}
	}
	go run(cmd)
	return done
}
func awaitReadStore(t *testing.T, entered <-chan struct{}) {
	t.Helper()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("STORE did not start")
	}
}
func finishRead(t *testing.T, m Model, done <-chan tea.Msg) Model {
	t.Helper()
	select {
	case msg := <-done:
		if _, ok := msg.(readRemoteCompletedMsg); !ok {
			t.Fatalf("unexpected completion %T", msg)
		}
		next, _ := m.Update(msg)
		return next.(Model)
	case <-time.After(3 * time.Second):
		t.Fatal("completion did not arrive")
		return m
	}
}

func TestOptimisticReadImmediateAndRepair(t *testing.T) {
	m, entered, release := readPendingModel(t)
	msg := m.messages[0]
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = next.(Model)
	if !m.messages[0].Read || m.mailboxes[0].UnreadCount != 2 || m.messageCursor != 1 {
		t.Fatalf("not immediate: read=%v count=%d cursor=%d", m.messages[0].Read, m.mailboxes[0].UnreadCount, m.messageCursor)
	}
	stored, _ := m.db.GetMessage(msg.ID)
	if !stored.Read {
		t.Fatal("SQLite not immediate")
	}
	if !strings.Contains(m.renderMessagesPane(), m.pendingReadIndicator()) {
		t.Fatal("missing pending indicator")
	}
	if m.setMessageReadCmd(msg, false, false) != nil {
		t.Fatal("duplicate accepted")
	}
	done := readRemoteResults(cmd)
	awaitReadStore(t, entered)
	// Simulate the known stale-sync write while STORE is outstanding.
	if err := m.db.MarkRead(msg.ID, false); err != nil {
		t.Fatal(err)
	}
	release <- nil
	m = finishRead(t, m, done)
	stored, _ = m.db.GetMessage(msg.ID)
	if !stored.Read {
		t.Fatal("successful STORE did not repair cache")
	}
	if len(m.pendingReads) != 0 || m.messageCursor != 1 || m.mailboxes[0].UnreadCount != 2 {
		t.Fatal("completion reapplied UI changes or leaked pending")
	}
	if strings.Contains(m.renderMessagesPane(), m.pendingReadIndicator()) {
		t.Fatal("pending remained visible")
	}
}

func TestOptimisticReadConcurrentAndFailure(t *testing.T) {
	m, entered, release := readPendingModel(t)
	first := m.setMessageReadCmd(m.messages[0], true, false)
	done1 := readRemoteResults(first)
	awaitReadStore(t, entered)
	second := m.setMessageReadCmd(m.messages[1], true, false)
	done2 := readRemoteResults(second)
	if len(m.pendingReads) != 2 || m.mailboxes[0].UnreadCount != 1 {
		t.Fatal("other message blocked")
	}
	release <- errors.New("rejected")
	m = finishRead(t, m, done1)
	if len(m.pendingReads) != 1 || !strings.Contains(m.statusMsg, "rejected") || !m.messages[0].Read {
		t.Fatal("failure cleanup/optimistic state")
	}
	awaitReadStore(t, entered)
	release <- nil
	m = finishRead(t, m, done2)
	if len(m.pendingReads) != 0 || m.mailboxes[0].UnreadCount != 1 {
		t.Fatal("second completion")
	}
	// A stale completion cannot clear a newly accepted operation.
	cmd := m.setMessageReadCmd(m.messages[0], false, false)
	pending := m.pendingReads[m.messages[0].ID]
	next, _ := m.Update(readRemoteCompletedMsg{IDs: []int64{m.messages[0].ID}, Token: pending.Token - 1})
	m = next.(Model)
	if len(m.pendingReads) != 1 {
		t.Fatal("stale completion unlocked newer request")
	}
	if !strings.Contains(m.renderMessagesPane(), "→"+m.pendingReadIndicator()) {
		t.Fatal("cursor hid pending unread")
	}
	done := readRemoteResults(cmd)
	awaitReadStore(t, entered)
	release <- nil
	m = finishRead(t, m, done)
	if m.messages[0].Read || m.messageCursor != 0 || m.mailboxes[0].UnreadCount != 2 {
		t.Fatal("unread semantics changed")
	}
}

func TestOptimisticReadBulkAndThread(t *testing.T) {
	for _, threaded := range []bool{false, true} {
		t.Run(map[bool]string{false: "bulk", true: "thread"}[threaded], func(t *testing.T) {
			m, entered, release := readPendingModel(t)
			// Start with a mixed read state. Bulk toggles each; a conversation marks all read.
			if err := m.db.MarkRead(m.messages[1].ID, true); err != nil {
				t.Fatal(err)
			}
			m.messages[1].Read = true
			m.mailboxes[0].UnreadCount = 2
			if threaded {
				m.cfg.Display.ThreadedConversations = true
				for i := range m.messages {
					m.messages[i].MessageID = "<root>"
				}
			} else {
				for _, msg := range m.messages {
					m.selectedMessages[msg.ID] = true
				}
			}
			m.applyFilter()
			next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
			m = next.(Model)
			want := int64(1)
			if threaded {
				want = 0
			}
			if len(m.pendingReads) != 3 || m.mailboxes[0].UnreadCount != want || m.messageCursor != 0 {
				t.Fatalf("pending=%d count=%d", len(m.pendingReads), m.mailboxes[0].UnreadCount)
			}
			if threaded && !m.rowReadPending(m.messageThreads[0].Representative, m.messageThreads[0]) {
				t.Fatal("thread lost pending member")
			}
			done := readRemoteResults(cmd)
			// Thread operations use independent commands; bulk uses one STORE per desired state.
			stores := 2
			completions := 1
			if threaded {
				stores = 3
				completions = 3
			}
			for i := 0; i < stores; i++ {
				awaitReadStore(t, entered)
				release <- nil
			}
			for i := 0; i < completions; i++ {
				m = finishRead(t, m, done)
			}
			if len(m.pendingReads) != 0 || m.mailboxes[0].UnreadCount != want || m.messageCursor != 0 {
				t.Fatal("bulk completion changed local result")
			}
		})
	}
}

func TestOptimisticReadAutomaticAndFiltering(t *testing.T) {
	for _, path := range []string{"open", "focus", "command", "summarize", "filter"} {
		t.Run(path, func(t *testing.T) {
			m, entered, release := readPendingModel(t)
			msg := m.messages[0]
			var cmd tea.Cmd
			switch path {
			case "open":
				if m.openedMessageCmd(msg) != nil {
					t.Fatal("disabled open")
				}
				m.cfg.Display.MarkReadOnOpen = true
				cmd = m.openedMessageCmd(msg)
			case "focus":
				if m.focusedMessageChangedCmd(msg) != nil {
					t.Fatal("disabled focus")
				}
				m.cfg.Display.MarkReadOnFocus = true
				cmd = m.focusedMessageChangedCmd(msg)
			case "command":
				next, c := m.executeCommand("toggle-read")
				m = next.(Model)
				cmd = c
			case "summarize":
				m.cfg.AI.MarkReadOnSummarize = false
				next, c := m.Update(AISummaryFetchedMsg{MessageID: msg.ID, Summary: "summary"})
				m = next.(Model)
				if c != nil || len(m.pendingReads) != 0 {
					t.Fatal("disabled summarize")
				}
				m.cfg.AI.MarkReadOnSummarize = true
				next, cmd = m.Update(AISummaryFetchedMsg{MessageID: msg.ID, Summary: "summary"})
				m = next.(Model)
			case "filter":
				m.showUnreadOnly = true
				cmd = m.setMessageReadCmd(msg, true, true)
			}
			if !m.messages[0].Read || len(m.pendingReads) != 1 {
				t.Fatal("automatic path not immediate")
			}
			if path == "filter" && (len(m.filteredMessages) != 2 || m.messageCursor != 0) {
				t.Fatal("filter skipped successor")
			}
			done := readRemoteResults(cmd)
			awaitReadStore(t, entered)
			release <- nil
			m = finishRead(t, m, done)
			if len(m.pendingReads) != 0 {
				t.Fatal("pending leaked")
			}
		})
	}
}

func TestOptimisticReadLocalFailureAndDeadline(t *testing.T) {
	t.Run("database", func(t *testing.T) {
		m, _, _ := readPendingModel(t)
		msg := m.messages[0]
		_ = m.db.Close()
		_ = m.setMessageReadCmd(msg, true, true)
		if len(m.pendingReads) != 0 || m.messages[0].Read || m.messageCursor != 0 || m.mailboxes[0].UnreadCount != 3 {
			t.Fatal("failed persistence published success")
		}
	})
	t.Run("deadline", func(t *testing.T) {
		m, _, _ := readPendingModel(t)
		m.readSession = func(context.Context, config.AccountConfig, func(readStoreFunc) error) error {
			return context.DeadlineExceeded
		}
		cmd := m.setMessageReadCmd(m.messages[0], true, true)
		m = finishRead(t, m, readRemoteResults(cmd))
		if len(m.pendingReads) != 0 || !strings.Contains(m.statusMsg, "deadline exceeded") || m.messageCursor != 1 {
			t.Fatal("deadline cleanup")
		}
	})
}

func TestOptimisticReadRepeatedKeyAndStableOrder(t *testing.T) {
	m, entered, release := readPendingModel(t)
	m.cfg.Display.UnreadFirst = true
	firstID := m.messages[0].ID
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = next.(Model)
	done := readRemoteResults(cmd)
	awaitReadStore(t, entered)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = next.(Model)
	next, duplicate := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = next.(Model)
	if duplicate != nil || !m.messages[0].Read || m.filteredMessages[0].ID != firstID || m.mailboxes[0].UnreadCount != 2 {
		t.Fatal("pending x changed state or ordering")
	}
	release <- nil
	_ = finishRead(t, m, done)
}

func TestOptimisticReadIndicatorModes(t *testing.T) {
	m, _, _ := readPendingModel(t)
	m.cfg.Display.Icons = true
	if m.pendingReadIndicator() != readAnimationFrames[0] {
		t.Fatal("missing Unicode pending indicator")
	}
	m.cfg.Display.Icons = false
	if m.pendingReadIndicator() != "~" {
		t.Fatal("missing icon-disabled fallback")
	}
	m.cfg.Display.Icons = true
	m.styles.PlainUI = true
	if m.pendingReadIndicator() != "~" {
		t.Fatal("missing plain fallback")
	}
}

func TestOptimisticReadBulkPartialFailure(t *testing.T) {
	m, entered, release := readPendingModel(t)
	m.messages[1].Read = true
	m.mailboxes[0].UnreadCount = 2
	cmd := m.setSelectedMessagesReadCmd(m.messages)
	done := readRemoteResults(cmd)
	awaitReadStore(t, entered)
	release <- errors.New("read group failed")
	awaitReadStore(t, entered)
	release <- nil
	m = finishRead(t, m, done)
	if len(m.pendingReads) != 0 || m.mailboxes[0].UnreadCount != 1 || !strings.Contains(m.statusMsg, "read group failed") {
		t.Fatal("partial failure leaked locks or reapplied counts")
	}
}
