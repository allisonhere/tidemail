package ui

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestDeleteHidesRowImmediatelyAndUndoRestoresIt(t *testing.T) {
	f := newUndoTestFixture(t, 3)
	m := f.model
	first := f.messages[0]
	m.scheduleDelete(f.messages[:1])
	if len(m.filteredMessages) != 2 || m.indexOfFilteredMessage(first.ID) >= 0 {
		t.Fatal("pending delete must hide the row at once")
	}
	m.scheduleDelete(f.messages[1:2])
	if len(m.filteredMessages) != 1 {
		t.Fatal("second pending delete must hide its row too")
	}
	m.undoLatestDestructive()
	if len(m.filteredMessages) != 2 || m.indexOfFilteredMessage(f.messages[1].ID) < 0 || m.indexOfFilteredMessage(first.ID) >= 0 {
		t.Fatal("undo must restore only the latest delete")
	}
	id := m.pendingDestructiveActions[0].ID
	m.beginDestructiveCommit(id)
	if m.indexOfFilteredMessage(first.ID) >= 0 {
		t.Fatal("a delete the server is working on must stay hidden")
	}
}

func TestDeletePrototypeResultsAndStaleReload(t *testing.T) {
	for _, success := range []bool{false, true} {
		f := newUndoTestFixture(t, 3)
		m := f.model
		m.rebuildSidebar()
		m.cursorToMailbox(f.source.ID)
		// Prove this mailbox reload is accepted without version invalidation.
		probe, _ := m.Update(MessagesLoadedMsg{MailboxID: f.source.ID, Messages: f.messages[:1]})
		if len(probe.(Model).filteredMessages) != 1 {
			t.Fatal("fixture does not accept source mailbox reloads")
		}
		stale := MessagesLoadedMsg{ReadVersion: m.readVersion, MailboxID: f.source.ID, Messages: f.messages}
		m.scheduleDelete(f.messages[:1])
		currentID := m.currentRowMessage().ID
		id := m.pendingDestructiveActions[0].ID
		result := DestructiveActionResultMsg{ID: id, Kind: destructiveDelete}
		if success {
			result = m.beginDestructiveCommit(id)().(DestructiveActionResultMsg)
		} else {
			result.Failed = f.messages[:1]
			result.Err = errors.New("rejected")
		}
		m.handleDestructiveResult(result)
		want, wantCursor := 3, f.messages[0].ID // a failure puts the cursor back on the message
		if success {
			want, wantCursor = 2, currentID
		}
		if len(m.filteredMessages) != want || m.currentRowMessage().ID != wantCursor || m.messagePendingDestructive(f.messages[0].ID) {
			t.Fatal("bad result state")
		}
		if success {
			next, cmd := m.Update(stale)
			m = next.(Model)
			if cmd == nil || len(m.filteredMessages) != 2 || m.readVersion <= stale.ReadVersion {
				t.Fatal("stale reload resurrected delete")
			}
		} else if m.messages[0].Read != f.messages[0].Read {
			t.Fatal("failure changed read state")
		}
	}
}

func TestDeletePrototypeConflictingLocks(t *testing.T) {
	f := newUndoTestFixture(t, 3)
	m := f.model
	m.pendingReads = map[int64]pendingRead{f.messages[0].ID: {Token: 1}}
	m.scheduleDelete(f.messages[:1])
	m.scheduleArchive(f.messages[:1])
	m.scheduleMove(f.messages[:1], f.target)
	if len(m.pendingDestructiveActions) != 0 {
		t.Fatal("read pending allowed destructive action")
	}
	m.scheduleDelete(f.messages[1:2])
	m.scheduleDelete(f.messages[1:2])
	m.scheduleArchive(f.messages[1:2])
	m.scheduleMove(f.messages[1:2], f.target)
	if cmd := m.acceptReadChanges(f.messages[1:2], nil, false); cmd != nil {
		t.Fatal("delete allowed read change")
	}
	if len(m.pendingDestructiveActions) != 1 {
		t.Fatal("delete allowed conflicting destructive action")
	}
	m.scheduleDelete(f.messages[2:])
	if len(m.pendingDestructiveActions) != 2 {
		t.Fatal("unrelated message locked")
	}
}

func TestDeleteCompletionKeepsCursorVisibleOutOfOrder(t *testing.T) {
	f := newUndoTestFixture(t, 40)
	m := f.model
	m.width, m.height = 120, 20
	m.scheduleDelete(f.messages[:1])
	first := m.pendingDestructiveActions[0].ID
	m.scheduleDelete(f.messages[1:2])
	second := m.pendingDestructiveActions[1].ID
	m.messageCursor, m.listOffset = 12, 12
	currentID := m.currentRowMessage().ID
	// The second operation resolves first, above the scrolled viewport.
	m.handleDestructiveResult(m.beginDestructiveCommit(second)().(DestructiveActionResultMsg))
	if m.currentRowMessage().ID != currentID || m.messageCursor != 12 || m.listOffset > m.messageCursor ||
		m.messageCursor >= m.listOffset+m.articleRowsVisible() {
		t.Fatal("highlight left viewport after ID restoration")
	}
	m.handleDestructiveResult(DestructiveActionResultMsg{ID: first, Kind: destructiveDelete, Failed: f.messages[:1], Err: errors.New("rejected")})
	// The rejected row returns above the highlight: it stays on the same message.
	if m.currentRowMessage().ID != currentID || m.messageCursor != 13 || m.listOffset > m.messageCursor ||
		m.messageCursor >= m.listOffset+m.articleRowsVisible() {
		t.Fatal("out-of-order failure moved highlight")
	}
}

func TestDeleteCompletionDoesNotDoubleDecrementRecountedUnread(t *testing.T) {
	f := newUndoTestFixture(t, 3)
	m := f.model
	var unread []db.Message
	for _, msg := range f.messages {
		if !msg.Read {
			unread = append(unread, msg)
		}
	}
	if len(unread) != 2 {
		t.Fatal("fixture needs two unread messages")
	}
	m.scheduleDelete(unread[:1])
	deletion := m.beginDestructiveCommit(m.pendingDestructiveActions[0].ID)().(DestructiveActionResultMsg)
	// Both writes have reached SQL, but the read completion arrives first.
	other := unread[1]
	if err := f.database.MarkRead(other.ID, true); err != nil {
		t.Fatal(err)
	}
	m.pendingReads = map[int64]pendingRead{other.ID: {Token: 77, Read: true, MailboxID: other.MailboxID}}
	m.finishReadChanges(readRemoteCompletedMsg{Token: 77, IDs: []int64{other.ID}, Success: []MessageReadBatchEntry{{MessageID: other.ID, MailboxID: other.MailboxID, Read: true}}})
	// Leave one unread cached message to make a second decrement observable.
	if err := f.database.UpsertMessage(db.Message{MailboxID: f.source.ID, UID: 99, Subject: "unrelated unread"}); err != nil {
		t.Fatal(err)
	}
	if err := m.recountMailboxUnread(f.source.ID); err != nil {
		t.Fatal(err)
	}
	m.handleDestructiveResult(deletion)
	count, err := f.database.CountUnread(f.source.ID)
	if err != nil || count != 1 || m.mailboxes[0].UnreadCount != count {
		t.Fatalf("count=%d displayed=%d err=%v", count, m.mailboxes[0].UnreadCount, err)
	}
	mailboxes, err := f.database.ListMailboxes(f.source.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	for _, mb := range mailboxes {
		if mb.ID == f.source.ID && mb.UnreadCount != count {
			t.Fatal("persisted count differs from confirmed state")
		}
	}
}

func TestDeleteCacheFailureSuppressedAndRetriedOnReload(t *testing.T) {
	f := newUndoTestFixture(t, 2)
	m := f.model
	m.rebuildSidebar()
	m.cursorToMailbox(f.source.ID)
	msg := f.messages[0]
	// Fail the cache deletion at its last write, leaving a real cached row.
	if _, err := f.database.Exec("CREATE TRIGGER fail_delete BEFORE DELETE ON messages BEGIN SELECT RAISE(FAIL, 'cache unavailable'); END"); err != nil {
		t.Fatal(err)
	}
	m.scheduleDelete(f.messages[:1])
	id := m.pendingDestructiveActions[0].ID
	action := m.pendingDestructiveActions[0]
	action.Entries[0].Account.IMAPHost = "remote-already-confirmed"
	result := executeDestructiveBatches(action, f.database, func(entries []pendingDestructiveEntry) error {
		if len(entries) != 1 || entries[0].Message.ID != msg.ID {
			t.Fatal("unexpected remote batch")
		}
		return nil
	})
	if result.ID != id || len(result.Succeeded) != 1 || len(result.CacheFailed) != 1 || len(result.Failed) != 0 || result.Err == nil {
		t.Fatalf("remote success with cache failure misclassified: %+v", result)
	}
	m.handleDestructiveResult(result)
	if !strings.Contains(m.statusMsg, "local cache update failed") || !m.statusErr || len(m.confirmedDeleteCache) != 1 {
		t.Fatal("cache failure not reported and suppressed")
	}
	reload := m.loadMailboxMessagesCmd(f.source.ID)().(MessagesLoadedMsg)
	if len(reload.Messages) != 2 {
		t.Fatal("fixture lost failed cache row")
	}
	next, _ := m.Update(reload)
	m = next.(Model)
	if len(m.filteredMessages) != 1 || len(m.confirmedDeleteCache) != 1 {
		t.Fatal("fresh reload resurrected remote deletion")
	}
	if _, err := f.database.Exec("DROP TRIGGER fail_delete"); err != nil {
		t.Fatal(err)
	}
	stale := m.loadMailboxMessagesCmd(f.source.ID)().(MessagesLoadedMsg)
	next, _ = m.Update(stale)
	m = next.(Model)
	if len(m.filteredMessages) != 1 || len(m.confirmedDeleteCache) != 0 {
		t.Fatal("cleanup did not finish without resurrection")
	}
	if _, err := f.database.GetMessage(msg.ID); err == nil {
		t.Fatal("cleanup left cache row")
	}
	next, cmd := m.Update(stale)
	m = next.(Model)
	if cmd == nil || len(m.filteredMessages) != 1 {
		t.Fatal("snapshot preceding cache reconciliation was accepted")
	}
}

func TestBulkReadLocksBothSchedulingOrders(t *testing.T) {
	for _, account := range []bool{false, true} {
		for _, pendingKind := range []string{"delete", "read"} {
			for _, bulkFirst := range []bool{false, true} {
				t.Run(fmt.Sprintf("account=%v/pending=%s/bulkFirst=%v", account, pendingKind, bulkFirst), func(t *testing.T) {
					f := newUndoTestFixture(t, 2)
					m := f.model
					msg := f.messages[0]
					bulk := func() tea.Cmd {
						if account {
							return m.markAccountReadCmd(f.source.AccountID)
						}
						return m.markMailboxReadCmd(f.source.ID)
					}
					pending := func() tea.Cmd {
						if pendingKind == "delete" {
							return m.scheduleDelete(f.messages[:1])
						}
						read := true
						return m.acceptReadChanges(f.messages[:1], &read, false)
					}
					if bulkFirst {
						cmd := bulk()
						if m.pendingBulkReads[msg.MailboxID] == 0 {
							t.Fatal("bulk reservation not synchronous")
						}
						pending()
						m.scheduleArchive(f.messages[:1])
						m.scheduleMove(f.messages[:1], f.target)
						if len(m.pendingReads) != 0 || len(m.pendingDestructiveActions) != 0 {
							t.Fatal("bulk allowed conflicting action")
						}
						result := cmd().(MailboxReadUpdatedMsg)
						next, _ := m.Update(result)
						m = next.(Model)
						if len(m.pendingBulkReads) != 0 {
							t.Fatal("bulk reservation leaked")
						}
						stored, err := f.database.GetMessage(msg.ID)
						if err != nil || !stored.Read {
							t.Fatal("bulk read semantics changed")
						}
						pending()
						if pendingKind == "delete" && len(m.pendingDestructiveActions) != 1 {
							t.Fatal("delete stayed locked")
						}
						if pendingKind == "read" && len(m.pendingReads) != 1 {
							t.Fatal("read stayed locked")
						}
						next, _ = m.Update(result)
						m = next.(Model)
						if pendingKind == "delete" && len(m.pendingDestructiveActions) != 1 {
							t.Fatal("stale bulk completion changed newer action")
						}
					} else {
						pending()
						bulk()
						if len(m.pendingBulkReads) != 0 || !strings.Contains(m.statusMsg, "blocked") {
							t.Fatal("pending action allowed bulk read")
						}
						stored, err := f.database.GetMessage(msg.ID)
						if err != nil || stored.Read != msg.Read {
							t.Fatal("blocked bulk changed database")
						}
					}
				})
			}
		}
	}
}

func TestBulkReadFailureReleasesAllMailboxReservations(t *testing.T) {
	f := newUndoTestFixture(t, 1)
	m := f.model
	if _, err := f.database.Exec(fmt.Sprintf("CREATE TRIGGER fail_bulk BEFORE UPDATE OF read ON messages WHEN OLD.mailbox_id = %d BEGIN SELECT RAISE(FAIL, 'read failed'); END", f.source.ID)); err != nil {
		t.Fatal(err)
	}
	cmd := m.markAccountReadCmd(f.source.AccountID)
	result := cmd().(MailboxReadUpdatedMsg)
	if result.Err == nil {
		t.Fatal("expected SQL failure")
	}
	next, _ := m.Update(result)
	m = next.(Model)
	if len(m.pendingBulkReads) != 0 || !m.statusErr {
		t.Fatal("failed bulk retained reservations")
	}
	m.scheduleDelete(f.messages)
	if len(m.pendingDestructiveActions) != 1 {
		t.Fatal("failed bulk kept deletion locked")
	}
}

func TestBulkReadLeavesOtherMailboxesInteractiveAndIgnoresStaleCompletion(t *testing.T) {
	f := newUndoTestFixture(t, 1)
	m := f.model
	other := db.Message{MailboxID: f.target.ID, UID: 9, Subject: "other mailbox"}
	if err := f.database.UpsertMessage(other); err != nil {
		t.Fatal(err)
	}
	messages, err := f.database.ListMessages(f.target.ID)
	if err != nil {
		t.Fatal(err)
	}
	first := m.markMailboxReadCmd(f.source.ID)
	m.scheduleDelete(messages)
	if len(m.pendingDestructiveActions) != 1 {
		t.Fatal("bulk locked unrelated mailbox")
	}
	// A repeated bulk request must neither replace the token nor run SQL.
	token := m.pendingBulkReads[f.source.ID]
	m.markMailboxReadCmd(f.source.ID)
	if m.pendingBulkReads[f.source.ID] != token {
		t.Fatal("repeated bulk replaced reservation")
	}
	completed := first().(MailboxReadUpdatedMsg)
	next, _ := m.Update(completed)
	m = next.(Model)
	m.markMailboxReadCmd(f.source.ID)
	currentToken := m.pendingBulkReads[f.source.ID]
	next, _ = m.Update(completed)
	m = next.(Model)
	if currentToken == 0 || m.pendingBulkReads[f.source.ID] != currentToken {
		t.Fatal("stale completion released newer reservation")
	}
}

func TestAccountBulkReadPreservesPartialFailure(t *testing.T) {
	f := newUndoTestFixture(t, 1)
	m := f.model
	if err := f.database.UpsertMessage(db.Message{MailboxID: f.target.ID, UID: 1, Subject: "archive unread"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.database.Exec(fmt.Sprintf("CREATE TRIGGER fail_account_read BEFORE UPDATE OF read ON messages WHEN OLD.mailbox_id = %d BEGIN SELECT RAISE(FAIL, 'archive read failed'); END", f.target.ID)); err != nil {
		t.Fatal(err)
	}
	result := m.markAccountReadCmd(f.source.AccountID)().(MailboxReadUpdatedMsg)
	if result.Err == nil || len(result.MailboxIDs) != 1 || result.MailboxIDs[0] != f.source.ID {
		t.Fatalf("partial failure semantics changed: %+v", result)
	}
	next, _ := m.Update(result)
	m = next.(Model)
	if len(m.pendingBulkReads) != 0 || !m.statusErr {
		t.Fatal("partial failure left locks or hid error")
	}
	inbox, err := f.database.ListMessages(f.source.ID)
	if err != nil || !inbox[0].Read {
		t.Fatal("successful mailbox not marked read")
	}
	archive, err := f.database.ListMessages(f.target.ID)
	if err != nil || archive[0].Read {
		t.Fatal("failed mailbox changed")
	}
}

func TestDeleteStaleTimerCannotCommitNewerAction(t *testing.T) {
	f := newUndoTestFixture(t, 1)
	m := f.model
	m.scheduleDelete(f.messages)
	old := m.pendingDestructiveActions[0].ID
	m.undoLatestDestructive()
	m.scheduleDelete(f.messages)
	current := m.pendingDestructiveActions[0].ID
	if cmd := m.beginDestructiveCommit(old); cmd != nil || m.pendingDestructiveActions[0].Committing || current == old {
		t.Fatal("stale timer affected newer deletion")
	}
}

func TestDeletePartialRemoteAndCacheFailuresStayDistinct(t *testing.T) {
	for _, cacheFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("cacheFirst=%v", cacheFirst), func(t *testing.T) {
			f := newUndoTestFixture(t, 2)
			m := f.model
			other := f.messages[1]
			other.MailboxID = f.target.ID
			if err := f.database.MoveMessage(other.ID, other.MailboxID); err != nil {
				t.Fatal(err)
			}
			m.messages[1] = other
			if _, err := f.database.Exec("CREATE TRIGGER fail_cache BEFORE DELETE ON messages BEGIN SELECT RAISE(FAIL, 'cache write failed'); END"); err != nil {
				t.Fatal(err)
			}
			m.scheduleDelete([]db.Message{f.messages[0], other})
			action := m.pendingDestructiveActions[0]
			for i := range action.Entries {
				action.Entries[i].Account.IMAPHost = "confirmed-test"
			}
			if cacheFirst {
				action.Entries[0], action.Entries[1] = action.Entries[1], action.Entries[0]
			}
			result := executeDestructiveBatches(action, f.database, func(entries []pendingDestructiveEntry) error {
				if entries[0].Source.ID == f.source.ID {
					return errors.New("remote rejected")
				}
				return nil
			})
			if len(result.Failed) != 1 || len(result.CacheFailed) != 1 || len(result.Succeeded) != 1 ||
				result.CacheErr == nil || !strings.Contains(result.CacheErr.Error(), "cache write failed") {
				t.Fatalf("partial result: %+v", result)
			}
			m.handleDestructiveResult(result)
			if !strings.Contains(m.statusMsg, "1 failed") || !strings.Contains(m.statusMsg, "local cache update failed") ||
				!strings.Contains(m.statusMsg, "cache write failed") || !strings.Contains(m.statusMsg, "remote rejected") || m.messageHiddenDestructive(f.messages[0].ID) ||
				!m.messageHiddenDestructive(other.ID) {
				t.Fatal("partial failures conflated", m.statusMsg)
			}
		})
	}
}

func TestDeleteSyntheticMixedDestructiveCounts(t *testing.T) {
	// Two result orders are sufficient; archive and move share this accounting.
	for _, deleteFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("deleteFirst=%v", deleteFirst), func(t *testing.T) {
			f := newUndoTestFixture(t, 3)
			m := f.model
			for i := range m.messages {
				if err := f.database.MarkRead(m.messages[i].ID, false); err != nil {
					t.Fatal(err)
				}
				m.messages[i].Read = false
			}
			m.applyFilter()
			check := func(want int64) {
				t.Helper()
				if got := m.displayMailboxUnreadCount(m.mailboxes[0]); got != want {
					t.Fatalf("displayed unread = %d, want %d", got, want)
				}
			}
			m.scheduleDelete(m.messages[:1])
			deleteID := m.pendingDestructiveActions[0].ID
			m.scheduleMove(m.messages[1:2], f.target)
			moveID := m.pendingDestructiveActions[1].ID
			check(1) // Delete and move both hide immediately.
			if err := m.recountMailboxUnread(f.source.ID); err != nil {
				t.Fatal(err)
			}
			check(1) // Still subtract a pending entry present in the SQL count.
			deletion := m.beginDestructiveCommit(deleteID)().(DestructiveActionResultMsg)
			moved := m.beginDestructiveCommit(moveID)().(DestructiveActionResultMsg)
			results := []DestructiveActionResultMsg{deletion, moved}
			if !deleteFirst {
				results[0], results[1] = results[1], results[0]
			}
			for _, result := range results {
				m.handleDestructiveResult(result)
				check(1) // Both SQL writes finished before either UI result.
			}
			if len(m.pendingUnreadExcluded) != 0 {
				t.Fatal("pending accounting survived completed actions")
			}
			// Undo after a recount must also release the cached adjustment.
			m.scheduleMove(m.messages, f.target)
			if err := m.recountMailboxUnread(f.source.ID); err != nil {
				t.Fatal(err)
			}
			check(0)
			m.undoLatestDestructive()
			check(1)
			if len(m.pendingUnreadExcluded) != 0 {
				t.Fatal("undo retained pending accounting")
			}
		})
	}
}

func TestDeleteSyntheticViewportMixedResults(t *testing.T) {
	f := newUndoTestFixture(t, 0)
	m := f.model
	m.width, m.height = 120, 20
	// Keep the list synthetic and in memory; the SQL cache stays empty.
	for i := 0; i < 60; i++ {
		m.messages = append(m.messages, db.Message{ID: int64(1000 + i), MailboxID: f.source.ID,
			Subject: fmt.Sprintf("message %d", i), Read: true, Date: time.Unix(int64(100-i), 0)})
	}
	m.applyFilter()
	targets := []db.Message{m.messages[2], m.messages[10], m.messages[45], m.messages[55]}
	var ids []uint64
	for _, msg := range targets {
		m.scheduleDelete([]db.Message{msg})
		ids = append(ids, m.pendingDestructiveActions[len(m.pendingDestructiveActions)-1].ID)
	}
	m.messageCursor, m.listOffset = 25, 25
	currentID := m.currentRowMessage().ID
	// Resolve below, above, below, above; alternate success and failure.
	for _, completion := range []struct {
		index   int
		success bool
	}{{2, true}, {0, false}, {3, false}, {1, true}} {
		msg := targets[completion.index]
		result := DestructiveActionResultMsg{ID: ids[completion.index], Kind: destructiveDelete}
		if completion.success {
			result.Succeeded = []MessageRef{{ID: msg.ID, MailboxID: msg.MailboxID}}
		} else {
			result.Failed, result.Err = []db.Message{msg}, errors.New("synthetic rejection")
		}
		m.handleDestructiveResult(result)
		if m.currentRowMessage().ID != currentID || m.messageCursor < m.listOffset ||
			m.messageCursor >= m.listOffset+m.articleRowsVisible() {
			t.Fatalf("highlight moved or left viewport after target %d", completion.index)
		}
	}
	if len(m.filteredMessages) != 58 {
		t.Fatal("mixed outcomes removed failed messages")
	}
}

func TestDeleteSyntheticThreadedPartialDeletion(t *testing.T) {
	f := newUndoTestFixture(t, 0)
	m := f.model
	m.cfg.Display.ThreadedConversations = true
	m.width, m.height = 120, 20
	root := db.Message{ID: 1001, MailboxID: f.source.ID, MessageID: "<root@test>", Subject: "conversation", Date: time.Unix(100, 0)}
	reply := db.Message{ID: 1002, MailboxID: f.source.ID, MessageID: "<reply@test>", InReplyTo: root.MessageID, Subject: "Re: conversation", Date: time.Unix(300, 0)}
	other := db.Message{ID: 1003, MailboxID: f.source.ID, MessageID: "<other@test>", Subject: "other conversation", Date: time.Unix(200, 0)}
	m.messages = []db.Message{reply, other, root}
	m.applyFilter()
	if len(m.messageThreads) != 2 || m.currentRowMessage().ID != reply.ID {
		t.Fatal("fixture did not form representative thread")
	}
	m.scheduleDelete([]db.Message{reply})
	replyAction := m.pendingDestructiveActions[0].ID
	if len(m.messageThreads) != 2 || m.indexOfFilteredMessage(reply.ID) >= 0 || m.indexOfFilteredMessage(root.ID) < 0 {
		t.Fatal("deleting a thread's newest message must leave the conversation with its surviving member")
	}
	m.scheduleDelete([]db.Message{root})
	rootAction := m.pendingDestructiveActions[1].ID
	if len(m.messageThreads) != 1 || m.currentRowMessage().ID != other.ID {
		t.Fatal("deleting every member must hide the whole conversation")
	}
	// Mark commits without dispatching worker commands: no SQL rows or network.
	m.beginDestructiveCommit(replyAction)
	m.handleDestructiveResult(DestructiveActionResultMsg{ID: replyAction, Kind: destructiveDelete, Succeeded: []MessageRef{{ID: reply.ID, MailboxID: reply.MailboxID}}})
	if len(m.messageThreads) != 1 || m.indexOfFilteredMessage(root.ID) >= 0 {
		t.Fatal("a confirmed delete must not bring back a still-pending sibling")
	}
	m.handleDestructiveResult(DestructiveActionResultMsg{ID: rootAction, Kind: destructiveDelete, Failed: []db.Message{root}, Err: errors.New("synthetic rejection")})
	if m.indexOfFilteredMessage(root.ID) < 0 || m.indexOfFilteredMessage(reply.ID) >= 0 {
		t.Fatal("a rejected delete must restore exactly its own message")
	}
}

func TestDeleteSuccessStatusIsPill(t *testing.T) {
	f := newUndoTestFixture(t, 2)
	m := f.model
	m.rebuildSidebar()
	m.cursorToMailbox(f.source.ID)
	m.scheduleDelete(f.messages[:1])
	action := m.pendingDestructiveActions[0]
	m.handleDestructiveResult(executeDestructiveBatches(action, f.database, func([]pendingDestructiveEntry) error { return nil }))
	if m.statusMsg != "deleted" || m.statusErr || !m.statusPill {
		t.Fatalf("status = %q err=%v pill=%v", m.statusMsg, m.statusErr, m.statusPill)
	}

	m.styles.PlainUI = false
	pill := m.statusPillText()
	if !strings.Contains(pill, "deleted") || !strings.Contains(pill, pillCapLeft) {
		t.Fatalf("pill = %q", pill)
	}
	m.styles.PlainUI = true
	if got := ansi.Strip(m.statusPillText()); got != "[deleted]" {
		t.Fatalf("plain pill = %q", got)
	}

	// Every non-error status is a pill; errors are drawn by the red pill instead.
	m.setStatus("synced", false)
	if !m.statusPill {
		t.Fatal("a plain status should be a pill")
	}
	m.setStatus("boom", true)
	if m.statusPill {
		t.Fatal("an error should not use the green pill")
	}
}

func TestEveryStatusRendersAsPill(t *testing.T) {
	f := newUndoTestFixture(t, 1)
	m := f.model
	m.width = 100
	m.styles.PlainUI = false
	m.setStatus("marked read", false)
	if bar := m.renderStatusBar(); !strings.Contains(bar, pillCapLeft) {
		t.Fatalf("status bar = %q, want a pill", ansi.Strip(bar))
	}
	m.styles.PlainUI = true
	if bar := ansi.Strip(m.renderStatusBar()); !strings.Contains(bar, "[marked read]") {
		t.Fatalf("plain status bar = %q, want bracketed text", bar)
	}
}

func TestPendingDeleteAndUndoShareOnePill(t *testing.T) {
	f := newUndoTestFixture(t, 2)
	m := f.model
	m.rebuildSidebar()
	m.cursorToMailbox(f.source.ID)
	m.scheduleDelete(f.messages[:1])
	m.width = 120
	row := ansi.Strip(m.renderStatusBar())
	want := pillCapLeft + "delete pending · ctrl+z undo" + pillCapRight
	if !strings.Contains(row, want) {
		t.Fatalf("status bar = %q, want one pill %q", row, want)
	}
	m.styles.PlainUI = true
	if row := ansi.Strip(m.renderStatusBar()); !strings.Contains(row, "[delete pending · ctrl+z undo 6s]") {
		t.Fatalf("plain status bar = %q", row)
	}
}

func TestUndoGaugePillDrains(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })

	f := newUndoTestFixture(t, 1)
	m := f.model
	const text = "delete pending · ctrl+z undo"
	bgRuns := func(s string) int {
		return len(regexp.MustCompile(`48;2;\d+;\d+;\d+m`).FindAllString(s, -1))
	}
	full := m.gaugePillText(text, destructiveUndoWindow)
	half := m.gaugePillText(text, destructiveUndoWindow/2)
	empty := m.gaugePillText(text, 0)
	for name, pill := range map[string]string{"full": full, "half": half, "empty": empty} {
		if got := ansi.Strip(pill); got != pillCapLeft+text+pillCapRight {
			t.Fatalf("%s gauge text = %q", name, got)
		}
	}
	if bgRuns(half) <= bgRuns(full) || bgRuns(half) <= bgRuns(empty) {
		t.Fatalf("half gauge should use two backgrounds: full=%d half=%d empty=%d", bgRuns(full), bgRuns(half), bgRuns(empty))
	}
	if full == empty {
		t.Fatal("full and empty gauges look identical")
	}

	m.styles.PlainUI = true
	if got := ansi.Strip(m.gaugePillText(text, 3500*time.Millisecond)); got != "["+text+" 4s]" {
		t.Fatalf("plain gauge = %q", got)
	}
}

func TestUndoGaugeClockRunsForArchiveAndMove(t *testing.T) {
	f := newUndoTestFixture(t, 2)
	m := f.model
	m.rebuildSidebar()
	m.cursorToMailbox(f.source.ID)
	if cmd := m.scheduleArchive(f.messages[:1]); cmd == nil || !m.readAnimationRunning {
		t.Fatalf("archive did not start the redraw clock: running=%v", m.readAnimationRunning)
	}
	m.undoLatestDestructive()
	if m.deleteFeedbackActive() {
		t.Fatal("clock still wanted after undo")
	}
}

// Past the undo window, the pill stays up as a yellow "deleting" pill with a
// spinner until the server confirms, so there is no blank gap.
func TestCommittingDeleteShowsWorkingPill(t *testing.T) {
	trueColor(t)
	f := newUndoTestFixture(t, 2)
	m := f.model
	m.rebuildSidebar()
	m.cursorToMailbox(f.source.ID)
	m.scheduleDelete(f.messages[:1])
	m.beginDestructiveCommit(m.pendingDestructiveActions[0].ID)
	m.width = 120

	row := m.renderStatusBar()
	if !strings.Contains(ansi.Strip(row), "deleting ") || !strings.Contains(row, background(t, alertPillYellow.fill)) {
		t.Fatalf("status bar = %q, want a yellow deleting pill", row)
	}
	if !m.deleteFeedbackActive() {
		t.Fatal("the read clock must keep running to turn the spinner")
	}
	m.styles.PlainUI = true
	if got := ansi.Strip(m.renderStatusBar()); !strings.Contains(got, "[deleting…]") {
		t.Fatalf("plain status bar = %q", got)
	}
}

// When the server confirms, the yellow pill fades into the green result, and
// ends on the exact green pair.
func TestFinishedDeleteFadesFromYellowToGreen(t *testing.T) {
	trueColor(t)
	f := newUndoTestFixture(t, 2)
	m := f.model
	m.rebuildSidebar()
	m.cursorToMailbox(f.source.ID)
	m.scheduleDelete(f.messages[:1])
	id := m.pendingDestructiveActions[0].ID
	m.beginDestructiveCommit(id)
	action := m.pendingDestructiveActions[0]
	if cmd := m.handleDestructiveResult(executeDestructiveBatches(action, f.database, func([]pendingDestructiveEntry) error { return nil })); cmd == nil {
		t.Fatal("expected status clear and fade ticks")
	}
	if m.pillFadeStart.IsZero() || m.pillFadeFrom != alertPillYellow {
		t.Fatalf("fade not started from yellow: %+v", m.pillFadeFrom)
	}

	start := m.pillFadeStart
	mid := m.fadedPill(alertPillGreen, start.Add(pillFadeDuration/2))
	if want := mixColors(alertPillYellow.fill, alertPillGreen.fill, 0.5); mid.fill != want {
		t.Fatalf("midway fill = %s, want %s", mid.fill, want)
	}
	if end := m.fadedPill(alertPillGreen, start.Add(pillFadeDuration)); end != alertPillGreen {
		t.Fatalf("end pill = %+v, want exact green", end)
	}

	// The tick re-arms mid-fade, stops once done, and ignores stale generations.
	if m.pillFadeTick(pillFadeTickMsg{Generation: m.pillFadeGeneration}) == nil {
		t.Fatal("fade stopped early")
	}
	if m.pillFadeTick(pillFadeTickMsg{Generation: m.pillFadeGeneration - 1}) != nil {
		t.Fatal("stale fade tick re-armed")
	}
	m.pillFadeStart = time.Now().Add(-pillFadeDuration)
	if m.pillFadeTick(pillFadeTickMsg{Generation: m.pillFadeGeneration}) != nil || !m.pillFadeStart.IsZero() {
		t.Fatal("fade did not stop after its duration")
	}

	// An unrelated status never inherits a half-faded color.
	m.pillFadeStart = time.Now()
	m.setStatus("synced", false)
	if !m.pillFadeStart.IsZero() {
		t.Fatal("setStatus did not cancel the fade")
	}
}

// A failed delete fades from yellow into the red error pill.
func TestFailedDeleteFadesToRed(t *testing.T) {
	f := newUndoTestFixture(t, 2)
	m := f.model
	m.rebuildSidebar()
	m.cursorToMailbox(f.source.ID)
	m.scheduleDelete(f.messages[:1])
	m.beginDestructiveCommit(m.pendingDestructiveActions[0].ID)
	action := m.pendingDestructiveActions[0]
	m.handleDestructiveResult(executeDestructiveBatches(action, f.database, func([]pendingDestructiveEntry) error { return errors.New("offline") }))
	if !m.statusErr || m.pillFadeStart.IsZero() || m.pillFadeFrom != alertPillYellow {
		t.Fatalf("err=%v fadeFrom=%+v start=%v", m.statusErr, m.pillFadeFrom, m.pillFadeStart)
	}
	mid := m.fadedPill(alertPillRed, m.pillFadeStart.Add(pillFadeDuration/2))
	if want := mixColors(alertPillYellow.fill, alertPillRed.fill, 0.5); mid.fill != want {
		t.Fatalf("midway fill = %s, want %s", mid.fill, want)
	}
}

func TestDeleteCommitIsLogged(t *testing.T) {
	f := newUndoTestFixture(t, 1)
	m := f.model
	m.rebuildSidebar()
	m.cursorToMailbox(f.source.ID)
	m.scheduleDelete(f.messages[:1])
	id := m.pendingDestructiveActions[0].ID
	m.beginDestructiveCommit(id)
	result := executeDestructiveBatches(m.pendingDestructiveActions[0], f.database, func([]pendingDestructiveEntry) error { return nil })
	result.Elapsed = 1500 * time.Millisecond
	m.handleDestructiveResult(result)
	var logs []string
	for _, e := range m.logBuffer {
		logs = append(logs, e.Message)
	}
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "delete: sending 1 message(s) to the server") ||
		!strings.Contains(joined, "delete: server work took 1.5s") ||
		!strings.Contains(joined, "1 ok, 0 failed") {
		t.Fatalf("delete lifecycle missing from the log:\n%s", joined)
	}
}

func TestDeleteLeavesCursorWhenFocusedMessageNotDeleted(t *testing.T) {
	f := newUndoTestFixture(t, 4)
	m := f.model
	m.messageCursor = 2
	focused := f.messages[2].ID
	if cur := m.currentRowMessage(); cur == nil || cur.ID != focused {
		t.Fatalf("fixture cursor not on message 2: %+v", cur)
	}
	m.scheduleDelete(f.messages[:1])
	if cur := m.currentRowMessage(); cur == nil || cur.ID != focused {
		t.Fatalf("cursor moved off the focused message: %+v", cur)
	}
}

func TestStuckDeleteIsRestoredByWatchdog(t *testing.T) {
	f := newUndoTestFixture(t, 2)
	m := f.model
	m.scheduleDelete(f.messages[:1])
	id := m.pendingDestructiveActions[0].ID
	if cmd := m.failStuckDestructive(id); cmd != nil || len(m.pendingDestructiveActions) != 1 {
		t.Fatal("watchdog must ignore an action that has not started committing")
	}
	m.beginDestructiveCommit(id)
	m.failStuckDestructive(id)
	if len(m.pendingDestructiveActions) != 0 || m.messageActionPending(f.messages[0].ID) {
		t.Fatal("stuck delete was not cleared")
	}
	if len(m.filteredMessages) != 2 {
		t.Fatalf("message not restored: %d rows", len(m.filteredMessages))
	}
	// A late real result for the same action is ignored.
	m.handleDestructiveResult(DestructiveActionResultMsg{ID: id, Kind: destructiveDelete})
}

// A pending delete is hidden by a filter over whatever list is loaded, so it
// has to stay hidden through every reload path until the server answers.
func TestPendingDeleteStaysHiddenAcrossReloadsFolderSwitchesAndSearch(t *testing.T) {
	f := newUndoTestFixture(t, 3)
	m := f.model
	m.rebuildSidebar()
	m.cursorToMailbox(f.source.ID)
	deleted := f.messages[0]
	all := append([]db.Message(nil), f.messages...)
	m.scheduleDelete([]db.Message{deleted})

	hidden := func(step string) {
		t.Helper()
		if m.indexOfFilteredMessage(deleted.ID) >= 0 || len(m.filteredMessages) != 2 {
			t.Fatalf("%s: pending delete resurfaced (%d visible)", step, len(m.filteredMessages))
		}
	}
	reload := func(msg MessagesLoadedMsg) {
		next, _ := m.Update(msg)
		m = next.(Model)
	}

	// A fresh reload and a stale one, both still carrying the message.
	reload(MessagesLoadedMsg{ReadVersion: m.readVersion, MailboxID: f.source.ID, Messages: all})
	hidden("fresh reload")
	reload(MessagesLoadedMsg{ReadVersion: m.readVersion - 1, MailboxID: f.source.ID, Messages: all})
	hidden("stale reload")

	// Away to another folder and back again.
	reload(MessagesLoadedMsg{ReadVersion: m.readVersion, MailboxID: f.target.ID, Messages: nil})
	m.cursorToMailbox(f.target.ID)
	m.cursorToMailbox(f.source.ID)
	reload(MessagesLoadedMsg{ReadVersion: m.readVersion, MailboxID: f.source.ID, Messages: all})
	hidden("folder switch")

	// Search results that include the message, then leaving search.
	m.messages = append([]db.Message(nil), all...)
	m.searchMode = true
	m.applyFilter()
	hidden("search results")
	m.searchMode = false
	m.messages = append([]db.Message(nil), all...)
	m.applyFilter()
	hidden("leaving search")

	// While the server is working on it.
	m.beginDestructiveCommit(m.pendingDestructiveActions[0].ID)
	reload(MessagesLoadedMsg{ReadVersion: m.readVersion, MailboxID: f.source.ID, Messages: all})
	hidden("reload while committing")

	// Only a rejection brings it back, exactly once.
	id := m.pendingDestructiveActions[0].ID
	m.handleDestructiveResult(DestructiveActionResultMsg{ID: id, Kind: destructiveDelete, Failed: []db.Message{deleted}, Err: errors.New("rejected")})
	if len(m.filteredMessages) != 3 || m.indexOfFilteredMessage(deleted.ID) < 0 {
		t.Fatal("a rejected delete must restore the message")
	}
}

// Found driving a real mailbox offline: a failed delete brought its message
// back but left the cursor on the neighbour that had taken its place, so
// pressing d again to retry would delete the wrong message.
func TestFailedDeleteReturnsCursorToTheRestoredMessage(t *testing.T) {
	for _, moved := range []bool{false, true} {
		t.Run(fmt.Sprintf("cursorMoved=%v", moved), func(t *testing.T) {
			f := newUndoTestFixture(t, 4)
			m := f.model
			m.messageCursor = m.indexOfFilteredMessage(f.messages[1].ID)
			target := m.currentRowMessage().ID
			m.scheduleDelete([]db.Message{*m.currentRowMessage()})
			replacement := m.currentRowMessage().ID
			if replacement == target {
				t.Fatal("fixture: the cursor should have moved off the deleted message")
			}
			if moved {
				m.messageCursor = m.indexOfFilteredMessage(f.messages[3].ID)
			}
			stayOn := m.currentRowMessage().ID
			id := m.pendingDestructiveActions[0].ID
			deleted := []db.Message{f.messages[1]}
			m.handleDestructiveResult(DestructiveActionResultMsg{ID: id, Kind: destructiveDelete, Failed: deleted, Err: errors.New("network is unreachable")})
			want := target
			if moved {
				want = stayOn // the user went somewhere else; don't yank them back
			}
			if got := m.currentRowMessage().ID; got != want {
				t.Fatalf("cursor on %d after the failed delete, want %d", got, want)
			}
		})
	}
}
