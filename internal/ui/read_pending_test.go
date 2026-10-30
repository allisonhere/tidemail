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
	cfg.Display.UnreadFirst = false
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
		next, follow := m.Update(msg)
		return settle(t, next.(Model), follow)
	case <-time.After(3 * time.Second):
		t.Fatal("completion did not arrive")
		return m
	}
}

func TestServerConfirmedReadAcceptanceAndCompletion(t *testing.T) {
	m, entered, release := readPendingModel(t)
	msg, nextID := m.messages[0], m.messages[1].ID
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = next.(Model)
	stored, _ := m.db.GetMessage(msg.ID)
	if stored.Read || m.messages[0].Read || m.mailboxes[0].UnreadCount != 3 || m.currentRowMessage().ID != nextID {
		t.Fatal("acceptance changed state/counts or failed to advance")
	}
	if !strings.Contains(m.renderMessagesPane(), m.pendingReadIndicator()) {
		t.Fatal("missing pending indicator")
	}
	if m.setMessageReadCmd(msg, false, false) != nil {
		t.Fatal("accepted contradictory request")
	}
	done := readRemoteResults(cmd)
	awaitReadStore(t, entered)
	stored, _ = m.db.GetMessage(msg.ID)
	if stored.Read {
		t.Fatal("changed while STORE blocked")
	}
	release <- nil
	m = finishRead(t, m, done)
	stored, _ = m.db.GetMessage(msg.ID)
	if !stored.Read || !m.messages[0].Read || m.mailboxes[0].UnreadCount != 2 || m.currentRowMessage().ID != nextID || len(m.pendingReads) != 0 {
		t.Fatal("confirmation did not persist/reload/preserve selection")
	}
	if strings.Contains(m.renderMessagesPane(), m.pendingReadIndicator()) {
		t.Fatal("pending indicator leaked")
	}
}

func TestServerConfirmedReadConcurrentAndFailure(t *testing.T) {
	m, entered, release := readPendingModel(t)
	first := m.setMessageReadCmd(m.messages[0], true, false)
	done1 := readRemoteResults(first)
	awaitReadStore(t, entered)
	second := m.setMessageReadCmd(m.messages[1], true, false)
	done2 := readRemoteResults(second)
	if len(m.pendingReads) != 2 || m.mailboxes[0].UnreadCount != 3 {
		t.Fatal("other message blocked or counts changed early")
	}
	release <- errors.New("rejected")
	m = finishRead(t, m, done1)
	stored, _ := m.db.GetMessage(m.messages[0].ID)
	if len(m.pendingReads) != 1 || !strings.Contains(m.statusMsg, "rejected") || stored.Read || m.messages[0].Read {
		t.Fatal("failure did not retain original state")
	}
	awaitReadStore(t, entered)
	release <- nil
	m = finishRead(t, m, done2)
	if len(m.pendingReads) != 0 || !m.messages[1].Read || m.mailboxes[0].UnreadCount != 2 {
		t.Fatal("second confirmation failed")
	}
	msg := m.messages[1]
	m.messageCursor = 1
	cmd := m.setMessageReadCmd(msg, false, false)
	pending := m.pendingReads[msg.ID]
	version := m.readVersion
	next, _ := m.Update(readRemoteCompletedMsg{IDs: []int64{msg.ID}, Token: pending.Token - 1, Success: []MessageReadBatchEntry{{MessageID: msg.ID, MailboxID: msg.MailboxID, Read: false}}})
	m = next.(Model)
	if len(m.pendingReads) != 1 || m.readVersion != version || !m.messages[1].Read {
		t.Fatal("stale completion affected newer operation")
	}
	if !strings.Contains(m.renderMessagesPane(), "→"+m.pendingReadIndicator()) {
		t.Fatal("cursor hid pending feedback")
	}
	done := readRemoteResults(cmd)
	awaitReadStore(t, entered)
	release <- nil
	m = finishRead(t, m, done)
	if m.messages[1].Read || m.messageCursor != 1 || m.mailboxes[0].UnreadCount != 3 {
		t.Fatal("unread changed directional behavior")
	}
}

func TestServerConfirmedReadBulkAndThread(t *testing.T) {
	for _, threaded := range []bool{false, true} {
		t.Run(map[bool]string{false: "bulk", true: "thread"}[threaded], func(t *testing.T) {
			m, entered, release := readPendingModel(t)
			if err := m.db.MarkRead(m.messages[1].ID, true); err != nil {
				t.Fatal(err)
			}
			m.messages[1].Read = true
			m.mailboxes[0].UnreadCount = 2
			if threaded {
				m.cfg.Display.ThreadedConversations = true
				for i := range m.messages {
					m.messages[i].MessageID = "<root>"
					if err := m.db.UpsertMessage(m.messages[i]); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				for _, msg := range m.messages {
					m.selectedMessages[msg.ID] = true
				}
			}
			m.applyFilter()
			selectedID := m.currentRowMessage().ID
			next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
			m = next.(Model)
			if len(m.pendingReads) != 3 || m.mailboxes[0].UnreadCount != 2 || m.currentRowMessage().ID != selectedID || !m.messages[1].Read || m.messages[0].Read {
				t.Fatal("bulk acceptance changed state or cursor")
			}
			if threaded && !m.rowReadPending(m.messageThreads[0].Representative, m.messageThreads[0]) {
				t.Fatal("missing conversation pending")
			}
			done := readRemoteResults(cmd)
			stores, completions := 2, 1
			if threaded {
				stores, completions = 3, 3
			}
			for i := 0; i < stores; i++ {
				awaitReadStore(t, entered)
				release <- nil
			}
			for i := 0; i < completions; i++ {
				m = finishRead(t, m, done)
			}
			want := int64(1)
			if threaded {
				want = 0
			}
			if len(m.pendingReads) != 0 || m.mailboxes[0].UnreadCount != want || m.currentRowMessage().ID != selectedID {
				t.Fatal("bulk confirmation or selection failed")
			}
		})
	}
}

func TestServerConfirmedReadAutomatic(t *testing.T) {
	for _, path := range []string{"open", "focus", "command", "summarize"} {
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
			}
			if m.messages[0].Read || len(m.pendingReads) != 1 || m.mailboxes[0].UnreadCount != 3 {
				t.Fatal("automatic path changed state early")
			}
			wantCursor := 0
			if path == "command" {
				wantCursor = 1
			}
			if m.messageCursor != wantCursor {
				t.Fatal("automatic navigation changed")
			}
			done := readRemoteResults(cmd)
			awaitReadStore(t, entered)
			release <- nil
			m = finishRead(t, m, done)
			if len(m.pendingReads) != 0 || !m.messages[0].Read || m.mailboxes[0].UnreadCount != 2 || m.messageCursor != wantCursor {
				t.Fatal("automatic confirmation failed")
			}
		})
	}
}

func TestServerConfirmedReadViewsAndMultiplePending(t *testing.T) {
	for _, view := range []string{"chronological", "unread-first", "unread-only", "unified", "search"} {
		t.Run(view, func(t *testing.T) {
			m, entered, release := readPendingModel(t)
			m.cfg.Display.UnreadFirst = view == "unread-first" || view == "unified"
			m.showUnreadOnly = view == "unread-only"
			if view == "search" {
				m.searchMode = true
				m.searchQuery = "Test"
			}
			if view == "unified" {
				for i, row := range m.sidebarRows {
					if row.kind == rowKindUnified {
						m.sidebarCursor = i
						break
					}
				}
			}
			firstID, secondID, thirdID := m.messages[0].ID, m.messages[1].ID, m.messages[2].ID
			next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
			m = next.(Model)
			done1 := readRemoteResults(cmd)
			awaitReadStore(t, entered)
			// Returning to the pending message must not toggle it or advance again.
			m.messageCursor = 0
			next, duplicate := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
			m = next.(Model)
			if duplicate != nil || m.messageCursor != 0 {
				t.Fatal("duplicate key accepted")
			}
			m.messageCursor = 1
			next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
			m = next.(Model)
			done2 := readRemoteResults(cmd)
			if m.filteredMessages[0].ID != firstID || m.messages[0].Read || m.currentRowMessage().ID != thirdID {
				t.Fatal("pending changed ordering/state")
			}
			release <- nil
			m = finishRead(t, m, done1)
			awaitReadStore(t, entered)
			if m.currentRowMessage().ID != thirdID || len(m.pendingReads) != 1 {
				t.Fatal("completion disturbed selection/pending")
			}
			idx := m.indexOfFilteredMessage(secondID)
			if idx < 0 || m.filteredMessages[idx].Read || len(m.pendingReads) != 1 {
				t.Fatal("other pending changed read classification")
			}
			if view == "unread-only" && len(m.filteredMessages) != 2 {
				t.Fatal("confirmed read not filtered")
			}
			if (view == "unread-first" || view == "unified") && m.filteredMessages[len(m.filteredMessages)-1].ID != firstID {
				t.Fatal("SQL unread-first order not applied")
			}
			release <- errors.New("second rejected")
			m = finishRead(t, m, done2)
			stored, _ := m.db.GetMessage(secondID)
			if stored.Read || len(m.pendingReads) != 0 || m.mailboxes[0].UnreadCount != 2 || m.currentRowMessage().ID != thirdID {
				t.Fatal("failed pending message changed state/selection")
			}
		})
	}
}

func TestServerConfirmedReadPartialBatch(t *testing.T) {
	for _, failedRead := range []bool{true, false} {
		t.Run(map[bool]string{true: "read-fails", false: "unread-fails"}[failedRead], func(t *testing.T) {
			m, entered, release := readPendingModel(t)
			m.messages[1].Read = true
			m.mailboxes[0].UnreadCount = 2
			if err := m.db.MarkRead(m.messages[1].ID, true); err != nil {
				t.Fatal(err)
			}
			cmd := m.setSelectedMessagesReadCmd(m.messages)
			done := readRemoteResults(cmd)
			awaitReadStore(t, entered)
			if failedRead {
				release <- errors.New("group failed")
			} else {
				release <- nil
			}
			awaitReadStore(t, entered)
			if failedRead {
				release <- nil
			} else {
				release <- errors.New("group failed")
			}
			m = finishRead(t, m, done)
			want := int64(0)
			if failedRead {
				want = 3
			}
			if len(m.pendingReads) != 0 || m.mailboxes[0].UnreadCount != want || !strings.Contains(m.statusMsg, "group failed") {
				t.Fatal("partial failure changed wrong counts or leaked locks")
			}
			for _, msg := range m.messages {
				stored, _ := m.db.GetMessage(msg.ID)
				if stored.Read == failedRead || msg.Read == failedRead {
					t.Fatal("failed group changed or successful group not applied")
				}
			}
		})
	}
}

func TestServerConfirmedUnreadOrdering(t *testing.T) {
	m, entered, release := readPendingModel(t)
	msg := m.messages[0]
	if err := m.db.MarkRead(msg.ID, true); err != nil {
		t.Fatal(err)
	}
	m.cfg.Display.UnreadFirst = true
	m.mailboxes[0].UnreadCount = 2
	m = settle(t, m, m.loadMailboxMessagesCmd(msg.MailboxID))
	m.messageCursor = m.indexOfFilteredMessage(msg.ID)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = next.(Model)
	if m.messageCursor != 2 || !m.currentRowMessage().Read || m.mailboxes[0].UnreadCount != 2 {
		t.Fatal("unread request advanced or changed classification early")
	}
	done := readRemoteResults(cmd)
	awaitReadStore(t, entered)
	release <- nil
	m = finishRead(t, m, done)
	if m.messageCursor != 0 || m.currentRowMessage().ID != msg.ID || m.currentRowMessage().Read || m.mailboxes[0].UnreadCount != 3 {
		t.Fatal("confirmed unread failed to reorder while preserving ID")
	}
}

func TestServerConfirmedReadLocalAndErrors(t *testing.T) {
	for _, mode := range []string{"local", "database", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			m, _, _ := readPendingModel(t)
			if mode == "local" {
				m.cfg.Accounts[0].IMAPHost = ""
			} else {
				m.readSession = func(_ context.Context, _ config.AccountConfig, fn func(readStoreFunc) error) error {
					if mode == "deadline" {
						return context.DeadlineExceeded
					}
					return fn(func(context.Context, string, []uint32, bool) error { return nil })
				}
			}
			cmd := m.setMessageReadCmd(m.messages[0], true, true)
			if len(m.pendingReads) != 1 || m.messages[0].Read || m.messageCursor != 1 {
				t.Fatal("missing acceptance")
			}
			if mode == "database" {
				_ = m.db.Close()
			}
			m = finishRead(t, m, readRemoteResults(cmd))
			if len(m.pendingReads) != 0 || m.messageCursor != 1 {
				t.Fatal("cleanup/navigation")
			}
			if mode == "local" {
				if !m.messages[0].Read || m.mailboxes[0].UnreadCount != 2 {
					t.Fatal("local-only update not committed")
				}
			} else if m.messages[0].Read || m.mailboxes[0].UnreadCount != 3 || !m.statusErr {
				t.Fatal("error changed local state")
			}
		})
	}
}

func TestServerConfirmedReadStaleReloadsAndCompletionOrder(t *testing.T) {
	m, entered, release := readPendingModel(t)
	staleMessages := m.loadMailboxMessagesCmd(m.messages[0].MailboxID)()
	staleAccounts := m.loadAccountsCmd()()
	first := m.setMessageReadCmd(m.messages[0], true, true)
	done1 := readRemoteResults(first)
	awaitReadStore(t, entered)
	second := m.setMessageReadCmd(m.messages[1], true, true)
	done2 := readRemoteResults(second)
	selectedID := m.currentRowMessage().ID
	release <- nil
	event1 := <-done1
	awaitReadStore(t, entered)
	release <- nil
	event2 := <-done2
	// Deliver completions in the opposite order from their successful writes.
	next, cmd := m.Update(event2)
	m = settle(t, next.(Model), cmd)
	next, cmd = m.Update(event1)
	m = settle(t, next.(Model), cmd)
	if m.mailboxes[0].UnreadCount != 1 || m.currentRowMessage().ID != selectedID {
		t.Fatal("out-of-order completion repeated counts/advancement")
	}
	next, cmd = m.Update(staleMessages)
	m = next.(Model)
	if cmd == nil || !m.messages[0].Read {
		t.Fatal("stale message snapshot applied")
	}
	m = settle(t, m, cmd)
	next, cmd = m.Update(staleAccounts)
	m = next.(Model)
	if cmd == nil || m.mailboxes[0].UnreadCount != 1 {
		t.Fatal("stale count snapshot applied")
	}
	fresh := cmd().(AccountsLoadedMsg)
	if fresh.ReadVersion != m.readVersion || fresh.Mailboxes[0].UnreadCount != 1 {
		t.Fatal("replacement account snapshot not current")
	}
	if len(m.pendingReads) != 0 || m.currentRowMessage().ID != selectedID {
		t.Fatal("stale reload moved selection")
	}
}

func TestServerConfirmedReadToggleBeforeReload(t *testing.T) {
	m, entered, release := readPendingModel(t)
	msg := m.messages[0]
	cmd := m.setMessageReadCmd(msg, true, false)
	done := readRemoteResults(cmd)
	awaitReadStore(t, entered)
	release <- nil
	event := <-done
	// Deliberately do not execute the asynchronous reload yet.
	next, reload := m.Update(event)
	m = next.(Model)
	if reload == nil || !m.currentRowMessage().Read || len(m.pendingReads) != 0 {
		t.Fatal("unlocked row did not expose confirmed state")
	}
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = next.(Model)
	if pending := m.pendingReads[msg.ID]; pending.Read || len(m.pendingReads) != 1 || m.messageCursor != 0 {
		t.Fatal("immediate toggle used old state or advanced unread action")
	}
	done = readRemoteResults(cmd)
	awaitReadStore(t, entered)
	release <- nil
	m = finishRead(t, m, done)
	// A reload requested by the earlier completion must not restore the read state.
	m = settle(t, m, reload)
	if m.currentRowMessage().Read || m.mailboxes[0].UnreadCount != 3 || len(m.pendingReads) != 0 {
		t.Fatal("late earlier reload changed latest confirmed state")
	}
}
