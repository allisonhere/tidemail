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
	"github.com/muesli/termenv"
)

func TestDeletePrototypeCountdownUndoAndSpinner(t *testing.T) {
	f := newUndoTestFixture(t, 3)
	m := f.model
	first := f.messages[0]
	m.scheduleDelete(f.messages[:1])
	if len(m.filteredMessages) != 3 || m.messageCursor != 1 {
		t.Fatal("delete must stay visible and advance cursor")
	}
	deadline := m.pendingDestructiveActions[0].Deadline
	for seconds := 6; seconds >= 1; seconds-- {
		now := deadline.Add(-time.Duration(seconds) * time.Second)
		if got := m.rowDeleteIndicator(first, messageThread{}, now); got != string(rune('0'+seconds)) {
			t.Fatalf("countdown %d: %q", seconds, got)
		}
	}
	m.scheduleDelete(f.messages[1:2])
	m.pendingDestructiveActions[1].Deadline = deadline.Add(time.Second)
	if got := m.rowDeleteIndicator(f.messages[1], messageThread{}, deadline.Add(-2*time.Second)); got != "3" {
		t.Fatal("independent deadline", got)
	}
	cursor := m.messageCursor
	m.undoLatestDestructive()
	if m.messageCursor != cursor || m.rowDeleteIndicator(f.messages[1], messageThread{}, time.Now()) != "" {
		t.Fatal("undo changed cursor or retained marker")
	}
	id := m.pendingDestructiveActions[0].ID
	m.beginDestructiveCommit(id)
	if got := m.rowDeleteIndicator(first, messageThread{}, time.Now()); got != m.pendingReadIndicator() {
		t.Fatal("missing spinner", got)
	}
	if got := m.rowDeleteIndicator(f.messages[2], messageThread{Messages: []db.Message{first}}, time.Now()); got != m.pendingReadIndicator() {
		t.Fatal("thread missing spinner")
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
		want := 3
		if success {
			want = 2
		}
		if len(m.filteredMessages) != want || m.currentRowMessage().ID != currentID || m.messagePendingDestructive(f.messages[0].ID) {
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

func TestDeletePrototypeSubjectStrikethrough(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
	f := newUndoTestFixture(t, 2)
	m := f.model
	m.width, m.height = 120, 40
	check := func(want bool) {
		t.Helper()
		if got := regexp.MustCompile("\x1b\\[(?:[0-9]+;)*9(?:;[0-9]+)*m").MatchString(m.renderMessagesPane()); got != want {
			t.Fatalf("subject strikethrough = %v, want %v", got, want)
		}
	}
	check(false)
	m.scheduleDelete(f.messages[:1])
	check(true)
	m.undoLatestDestructive()
	check(false)
	m.scheduleDelete(f.messages[:1])
	id := m.pendingDestructiveActions[0].ID
	m.beginDestructiveCommit(id)
	check(true)
	m.handleDestructiveResult(DestructiveActionResultMsg{ID: id, Kind: destructiveDelete, Failed: f.messages[:1], Err: errors.New("rejected")})
	check(false)
	m.pendingReads = map[int64]pendingRead{f.messages[0].ID: {Token: 1}}
	check(false)
}

func TestDeletePrototypeSubjectInheritsRowStyle(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
	f := newUndoTestFixture(t, 2)
	m := f.model
	m.width, m.height = 120, 40
	m.messages[0].Read = false
	m.applyFilter()
	m.scheduleDelete(m.messages[:1])
	for _, cursor := range []int{1, 0} {
		m.messageCursor = cursor
		unread, _, selected, _, _, _ := m.messageRowStyles()
		style := applyMessageRowState(unread, selected, false, cursor == 0, m.styles.Theme)
		subject := style.UnsetPadding().UnsetWidth().UnsetMaxWidth().Strikethrough(true).Render(m.messageRowTitle(m.messages[0]))
		for _, segment := range strings.SplitAfter(subject, "\x1b[0m") {
			if !strings.Contains(m.renderMessagesPane(), segment) {
				t.Fatalf("pending subject lost row colors, cursor=%d", cursor)
			}
		}
	}
	if m.messages[0].Read {
		t.Fatal("rendering changed unread state")
	}
}

func TestDeletePrototypeRowBackgroundAfterSubject(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
	f := newUndoTestFixture(t, 1)
	m := f.model
	unread, _, selected, _, _, _ := m.messageRowStyles()
	for _, style := range []lipgloss.Style{unread, selected} {
		inline := style.UnsetPadding().UnsetWidth().UnsetMaxWidth()
		title := inline.Strikethrough(true).Render("subject")
		row := m.renderMessageRow(style, "6 ", "", "", false, 0, title, nil, "", 40)
		// Seven subject cells and two marker cells leave 31 background cells.
		if !strings.Contains(row, inline.Render(strings.Repeat(" ", 31))) {
			t.Fatal("trailing row padding does not restore row colors")
		}
		if lipgloss.Width(row) != 40 {
			t.Fatal("row width changed")
		}
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
	if m.currentRowMessage().ID != currentID || m.messageCursor != 11 || m.listOffset > m.messageCursor ||
		m.messageCursor >= m.listOffset+m.articleRowsVisible() {
		t.Fatal("highlight left viewport after ID restoration")
	}
	cursor, offset := m.messageCursor, m.listOffset
	m.handleDestructiveResult(DestructiveActionResultMsg{ID: first, Kind: destructiveDelete, Failed: f.messages[:1], Err: errors.New("rejected")})
	if m.currentRowMessage().ID != currentID || m.messageCursor != cursor || m.listOffset != offset {
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
		!strings.Contains(m.statusMsg, "cache write failed") || m.messageHiddenDestructive(f.messages[0].ID) ||
		!m.messageHiddenDestructive(other.ID) {
		t.Fatal("partial failures conflated", m.statusMsg)
	}
}
