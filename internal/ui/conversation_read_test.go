package ui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func conversationReadModel(t *testing.T, reads []bool) (Model, chan struct{}, chan error, []int64) {
	t.Helper()
	m, entered, release := readPendingModel(t)
	ids := messageIDs(m.messages)
	unread := int64(0)
	for i := range m.messages {
		msg := &m.messages[i]
		msg.MessageID = fmt.Sprintf("<conversation-%d>", msg.ID)
		msg.InReplyTo = "<conversation>"
		msg.Subject = fmt.Sprintf("Conversation %d", i)
		msg.Read = reads[i]
		if !msg.Read {
			unread++
		}
		if err := m.db.UpsertMessage(*msg); err != nil {
			t.Fatal(err)
		}
		if err := m.db.MarkRead(msg.ID, msg.Read); err != nil {
			t.Fatal(err)
		}
	}
	m.mailboxes[0].UnreadCount = unread
	if err := m.db.SetMailboxUnreadCount(m.mailboxes[0].ID, unread); err != nil {
		t.Fatal(err)
	}
	m.cfg.Display.ThreadedConversations = true
	m.cfg.Display.UnreadFirst = true
	m.applyFilter()
	m.setViewportForCurrentRow()
	return m, entered, release, ids
}

func receiveReadResult(t *testing.T, done <-chan tea.Msg) tea.Msg {
	t.Helper()
	select {
	case result := <-done:
		return result
	case <-time.After(3 * time.Second):
		t.Fatal("read confirmation missing")
		return nil
	}
}

func assertConversationReadSelection(t *testing.T, m *Model, id int64, expanded bool) {
	t.Helper()
	if m.currentRowMessage() == nil || m.currentRowMessage().ID != id {
		t.Fatalf("selection=%+v, want ID %d", m.currentRowMessage(), id)
	}
	for _, thread := range m.messageThreads {
		for _, msg := range thread.Messages {
			if msg.ID == id && m.threadExpanded(thread) != expanded {
				t.Fatalf("expansion changed for selected ID %d", id)
			}
		}
	}
}

func TestConversationReadTargetsAndAdvancement(t *testing.T) {
	cases := []struct {
		name          string
		reads         []bool
		expanded      bool
		selected      int // -1 selects the representative
		chronological bool
		fallback      bool
		pending       []int
		accept        int // -1 selects the current representative
		confirmed     int
		want          []bool
	}{
		{"collapsed-mixed", []bool{true, false, true}, false, -1, false, false, []int{0, 1, 2}, -1, 0, []bool{true, true, true}},
		{"collapsed-all-read", []bool{true, true, true}, false, -1, false, false, []int{0, 1, 2}, 0, 0, []bool{false, false, false}},
		{"expanded-unread-parent", []bool{true, false, false}, true, -1, false, false, []int{1}, 2, 2, []bool{true, true, false}},
		{"expanded-read-parent", []bool{true, false, true}, true, -1, true, false, []int{0}, 0, 0, []bool{false, false, true}},
		{"expanded-read-child-becomes-parent", []bool{true, false, false}, true, 0, false, false, []int{0}, 0, 0, []bool{false, false, false}},
		{"expanded-unread-child", []bool{false, true, false}, true, 2, false, false, []int{2}, 1, 1, []bool{false, true, true}},
		{"expanded-last-child-fallback", []bool{false, false, false}, true, 2, false, true, []int{2}, 3, 3, []bool{false, false, true}},
		{"expanded-last-visible-row", []bool{false, false, false}, true, 2, false, false, []int{2}, 2, 2, []bool{false, false, true}},
		{"collapsed-read-advances", []bool{true, false, true}, false, -1, false, true, []int{0, 1, 2}, 3, 3, []bool{true, true, true}},
		{"collapsed-read-parent-unread-members", []bool{true, false, true}, false, -1, true, true, []int{0, 1, 2}, 3, 3, []bool{true, true, true}},
		{"collapsed-unread-stays", []bool{true, true, true}, false, -1, false, true, []int{0, 1, 2}, 0, 0, []bool{false, false, false}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, entered, release, ids := conversationReadModel(t, c.reads)
			if c.chronological {
				m.cfg.Display.UnreadFirst = false
				m.rebuildMessageThreads()
			}
			if c.fallback {
				other := db.Message{MailboxID: m.mailboxes[0].ID, UID: 4, MessageID: "<other>", Subject: "Other conversation", Date: time.Unix(50, 0), Read: c.name != "collapsed-read-advances"}
				if err := m.db.UpsertMessage(other); err != nil {
					t.Fatal(err)
				}
				stored, err := m.db.ListMessages(other.MailboxID)
				if err != nil {
					t.Fatal(err)
				}
				for _, msg := range stored {
					if msg.UID == 4 {
						ids = append(ids, msg.ID)
					}
				}
				m.messages = stored
				m.applyFilter()
			}
			m.messageCursor = m.indexOfMessageSelection(ids[0], true)
			if c.expanded {
				m.toggleCurrentConversation()
			}
			if c.selected >= 0 {
				m.messageCursor = m.indexOfFilteredMessage(ids[c.selected])
			}
			m.setViewportForCurrentRow()
			sourceID := m.currentRowMessage().ID
			next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
			m = next.(Model)
			wantAccepted := sourceID
			if c.accept >= 0 {
				wantAccepted = ids[c.accept]
			}
			// Advancement is immediate, before any read flag or ordering changes.
			acceptedExpanded := c.expanded
			if c.accept == 3 {
				acceptedExpanded = false
			}
			assertConversationReadSelection(t, &m, wantAccepted, acceptedExpanded)
			if len(m.pendingReads) != len(c.pending) {
				t.Fatalf("pending count=%d want %d", len(m.pendingReads), len(c.pending))
			}
			for _, i := range c.pending {
				pending, ok := m.pendingReads[ids[i]]
				if !ok || pending.Read != c.want[i] {
					t.Fatalf("wrong target for ID %d: %+v", ids[i], pending)
				}
			}
			for i, id := range ids[:3] {
				stored, err := m.db.GetMessage(id)
				if err != nil || stored.Read != c.reads[i] {
					t.Fatal("read state changed optimistically")
				}
			}
			done := readRemoteResults(cmd)
			awaitReadStore(t, entered)
			release <- nil
			next, reload := m.Update(receiveReadResult(t, done))
			m = next.(Model)
			confirmedExpanded := c.expanded
			if c.confirmed == 3 {
				confirmedExpanded = false
			}
			assertConversationReadSelection(t, &m, ids[c.confirmed], confirmedExpanded)
			m = settle(t, m, reload)
			assertConversationReadSelection(t, &m, ids[c.confirmed], confirmedExpanded)
			if m.contentMessageID != ids[c.confirmed] {
				t.Fatal("reader did not follow selected target")
			}
			if len(m.pendingReads) != 0 {
				t.Fatal("pending reservation leaked")
			}
			for i, id := range ids[:3] {
				stored, err := m.db.GetMessage(id)
				if err != nil || stored.Read != c.want[i] {
					t.Fatalf("ID %d read=%t want %t: %v", id, stored.Read, c.want[i], err)
				}
			}
		})
	}
}

func TestConversationUnreadIndicatorsFollowExpansion(t *testing.T) {
	m, _, _, ids := conversationReadModel(t, []bool{true, false, true})
	m.cfg.Display.UnreadFirst = false
	m.rebuildMessageThreads()
	status := func(subject string) string { return ansi.Cut(ansi.Strip(rowLine(t, m, subject)), 1, 3) }
	m.messageCursor = -1 // inspect unread markers without the cursor marker replacing them
	if got := status("Conversation 0"); got != m.messageRowPrefix(false) {
		t.Fatalf("collapsed indicator %q", got)
	}
	m.messageCursor = 0
	m.toggleCurrentConversation()
	m.messageCursor = -1
	for i, read := range []bool{true, false, true} {
		if got := status(fmt.Sprintf("Conversation %d", i)); got != m.messageRowPrefix(read) {
			t.Fatalf("expanded row %d indicator %q", i, got)
		}
	}
	m.pendingReads = map[int64]pendingRead{ids[1]: {Token: 1, Read: true}}
	if status("Conversation 0") != m.messageRowPrefix(true) || status("Conversation 1") != m.pendingReadIndicator()+" " {
		t.Fatal("expanded pending feedback aggregated siblings")
	}
	m.messageCursor = 0
	m.toggleCurrentConversation()
	m.messageCursor = -1
	if status("Conversation 0") != m.pendingReadIndicator()+" " {
		t.Fatal("collapsed pending feedback omitted member")
	}
}

func TestConversationReadDuplicateFailureAndReload(t *testing.T) {
	m, entered, release, ids := conversationReadModel(t, []bool{true, false, true})
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = next.(Model)
	done := readRemoteResults(cmd)
	awaitReadStore(t, entered)
	next, duplicate := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = next.(Model)
	if duplicate != nil || len(m.pendingReads) != 3 {
		t.Fatal("duplicate collapsed request accepted")
	}
	release <- errors.New("read rejected")
	m = finishRead(t, m, done)
	assertConversationReadSelection(t, &m, ids[1], false)
	if len(m.pendingReads) != 0 || !strings.Contains(m.statusMsg, "read rejected") {
		t.Fatal("failure did not clear pending or report error")
	}
	for i, id := range ids {
		stored, _ := m.db.GetMessage(id)
		if stored.Read != []bool{true, false, true}[i] {
			t.Fatal("failure changed flags")
		}
	}
	// A passive reload still preserves the exact email, even if a new parent arrives.
	newMsg := db.Message{MailboxID: m.mailboxes[0].ID, UID: 4, MessageID: "<arrival>", InReplyTo: "<conversation>", Date: time.Unix(200, 0), Subject: "New arrival"}
	if err := m.db.UpsertMessage(newMsg); err != nil {
		t.Fatal(err)
	}
	m = settle(t, m, m.loadMailboxMessagesCmd(m.mailboxes[0].ID))
	assertConversationReadSelection(t, &m, ids[1], true)
}

func TestConversationReadSingleMessageAdvancementUnchanged(t *testing.T) {
	for _, threaded := range []bool{false, true} {
		for _, read := range []bool{false, true} {
			t.Run(fmt.Sprintf("threaded=%t/read=%t", threaded, read), func(t *testing.T) {
				m, entered, release := readPendingModel(t)
				ids := messageIDs(m.messages)
				m.cfg.Display.ThreadedConversations = threaded
				m.cfg.Display.UnreadFirst = true
				m.messages[0].Read = read
				if err := m.db.MarkRead(ids[0], read); err != nil {
					t.Fatal(err)
				}
				m.applyFilter()
				m.messageCursor = m.indexOfFilteredMessage(ids[0])
				// Unthreaded data order comes from SQL as in production.
				m = settle(t, m, m.loadMailboxMessagesCmd(m.mailboxes[0].ID))
				m.messageCursor = m.indexOfFilteredMessage(ids[0])
				wantID := ids[0]
				if !read {
					wantID = ids[1]
				}
				next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
				m = next.(Model)
				if m.currentRowMessage().ID != wantID {
					t.Fatal("single-message advancement changed")
				}
				done := readRemoteResults(cmd)
				awaitReadStore(t, entered)
				release <- nil
				m = finishRead(t, m, done)
				if m.currentRowMessage().ID != wantID {
					t.Fatal("single-message reconciliation undid navigation")
				}
			})
		}
	}
}

func TestConversationReadExplicitSelectionPrecedesRowScope(t *testing.T) {
	for _, expanded := range []bool{false, true} {
		t.Run(fmt.Sprintf("expanded=%t", expanded), func(t *testing.T) {
			m, entered, release, ids := conversationReadModel(t, []bool{true, false, true})
			if expanded {
				m.toggleCurrentConversation()
			}
			m.selectedMessages[ids[0]] = true
			m.selectedMessages[ids[1]] = true
			source := m.currentRowMessage().ID
			next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
			m = next.(Model)
			if m.currentRowMessage().ID != source || m.hasSelection() || len(m.pendingReads) != 2 || m.pendingReads[ids[0]].Read || !m.pendingReads[ids[1]].Read {
				t.Fatal("explicit toggles or no-advance semantics changed")
			}
			done := readRemoteResults(cmd)
			for i := 0; i < 2; i++ {
				awaitReadStore(t, entered)
				release <- nil
			}
			m = finishRead(t, m, done)
			for i, want := range []bool{false, true, true} {
				stored, _ := m.db.GetMessage(ids[i])
				if stored.Read != want {
					t.Fatal("selection touched unselected sibling or used aggregate target")
				}
			}
			wantID := ids[0]
			if expanded {
				wantID = source
			}
			assertConversationReadSelection(t, &m, wantID, expanded)
		})
	}
}

func TestConversationReadUnifiedMailboxBatchesAndPartialFailure(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(fmt.Sprintf("partial=%t", partial), func(t *testing.T) {
			reads := []bool{true, false, true}
			if partial {
				reads = []bool{true, true, true}
			}
			m, entered, release, ids := conversationReadModel(t, reads)
			accountID, err := m.db.AddAccount("other-test", "Other read test", "")
			if err != nil {
				t.Fatal(err)
			}
			mailboxID, err := m.db.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "INBOX"})
			if err != nil {
				t.Fatal(err)
			}
			// Same UID in different accounts: writes must follow DB ID and mailbox.
			if _, err = m.db.Exec("UPDATE messages SET mailbox_id = ?, uid = 1 WHERE id = ?", mailboxID, ids[1]); err != nil {
				t.Fatal(err)
			}
			m.accounts = append(m.accounts, db.Account{ID: accountID, ConfigID: "other-test", Name: "Other read test"})
			m.mailboxes = append(m.mailboxes, db.Mailbox{ID: mailboxID, AccountID: accountID, Name: "INBOX"})
			otherCfg := m.cfg.Accounts[0]
			otherCfg.ID, otherCfg.Name = "other-test", "Other read test"
			m.cfg.Accounts = append(m.cfg.Accounts, otherCfg)
			m.rebuildSidebar()
			for i, row := range m.sidebarRows {
				if row.kind == rowKindUnified {
					m.sidebarCursor = i
					break
				}
			}
			m.messages, err = m.db.ListUnifiedInboxUnreadFirst(false)
			if err != nil {
				t.Fatal(err)
			}
			m.applyFilter()
			if len(m.messageThreads) != 1 || m.messageThreads[0].Count != 3 {
				t.Fatal("cross-account thread missing")
			}
			stale := m.loadUnifiedInboxCmd()()
			type call struct {
				account string
				uids    []uint32
				read    bool
			}
			calls := make(chan call, 2)
			original := m.readSession
			m.readSession = func(ctx context.Context, account config.AccountConfig, fn func(readStoreFunc) error) error {
				return original(ctx, account, func(store readStoreFunc) error {
					return fn(func(ctx context.Context, mailbox string, uids []uint32, read bool) error {
						calls <- call{account.ID, append([]uint32(nil), uids...), read}
						return store(ctx, mailbox, uids, read)
					})
				})
			}
			next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
			m = next.(Model)
			if len(m.pendingReads) != 3 {
				t.Fatal("cross-account members not reserved")
			}
			done := readRemoteResults(cmd)
			seen := map[string]bool{}
			for i := 0; i < 2; i++ {
				awaitReadStore(t, entered)
				got := <-calls
				if seen[got.account] || got.read == partial {
					t.Fatalf("wrong mailbox batch %+v", got)
				}
				seen[got.account] = true
				wantUIDs := []uint32{1, 3}
				if got.account == "other-test" {
					wantUIDs = []uint32{1}
				}
				if !reflect.DeepEqual(got.uids, wantUIDs) {
					t.Fatalf("cross-account UID routing %+v, want %v", got, wantUIDs)
				}
				if partial && got.account == "read-test" {
					release <- errors.New("source account rejected")
				} else {
					release <- nil
				}
				event := receiveReadResult(t, done)
				next, reload := m.Update(event)
				m = settle(t, next.(Model), reload)
				if i == 0 {
					next, duplicate := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
					m = next.(Model)
					if duplicate != nil {
						t.Fatal("collapsed operation restarted while another mailbox remained pending")
					}
				}
			}
			wantID := ids[0]
			if partial {
				wantID = ids[1]
			}
			assertConversationReadSelection(t, &m, wantID, false)
			if len(m.pendingReads) != 0 {
				t.Fatal("pending locks leaked")
			}
			for i, id := range ids {
				stored, _ := m.db.GetMessage(id)
				want := true
				if partial {
					want = i != 1
				}
				if stored.Read != want {
					t.Fatalf("partial failure applied wrong state to ID %d", id)
				}
			}
			if partial && !strings.Contains(m.statusMsg, "source account rejected") {
				t.Fatal("partial failure not reported")
			}
			// Stale snapshots must not undo either the confirmed flags or collapsed selection.
			next, reload := m.Update(stale)
			m = settle(t, next.(Model), reload)
			assertConversationReadSelection(t, &m, wantID, false)
		})
	}
}

func TestConversationReadCollapsedSearchReloadKeepsCollapsed(t *testing.T) {
	m, entered, release, ids := conversationReadModel(t, []bool{true, false, true})
	m.searchMode = true
	m.searchQuery = "Conversation"
	m.applyFilter()
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = next.(Model)
	done := readRemoteResults(cmd)
	awaitReadStore(t, entered)
	release <- nil
	next, reload := m.Update(receiveReadResult(t, done))
	m = next.(Model)
	assertConversationReadSelection(t, &m, ids[0], false)
	m = settle(t, m, reload)
	assertConversationReadSelection(t, &m, ids[0], false)
}

func TestConversationReadAdvancesIntoIndependentlyExpandedConversation(t *testing.T) {
	m, entered, release, ids := conversationReadModel(t, []bool{false, false, false})
	for i := 4; i <= 5; i++ {
		if err := m.db.UpsertMessage(db.Message{MailboxID: m.mailboxes[0].ID, UID: uint32(i), MessageID: fmt.Sprintf("<other-%d>", i), InReplyTo: "<other>", Subject: fmt.Sprintf("Other %d", i), Date: time.Unix(int64(80-i), 0)}); err != nil {
			t.Fatal(err)
		}
	}
	stored, err := m.db.ListMessages(m.mailboxes[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	m.messages = stored
	m.applyFilter()
	m.toggleCurrentConversation()
	var otherID int64
	for _, msg := range stored {
		if msg.UID == 4 {
			otherID = msg.ID
		}
	}
	m.messageCursor = m.indexOfMessageSelection(otherID, true)
	m.toggleCurrentConversation()
	m.messageCursor = m.indexOfFilteredMessage(ids[2])
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = next.(Model)
	assertConversationReadSelection(t, &m, otherID, true)
	done := readRemoteResults(cmd)
	awaitReadStore(t, entered)
	release <- nil
	m = finishRead(t, m, done)
	assertConversationReadSelection(t, &m, otherID, true)
	for _, thread := range m.messageThreads {
		if !m.threadExpanded(thread) {
			t.Fatal("read reconciliation collapsed an independently expanded conversation")
		}
	}
}

func TestConversationReadExpandedDuplicateDoesNotAdvanceAgain(t *testing.T) {
	m, entered, release, ids := conversationReadModel(t, []bool{false, false, false})
	m.toggleCurrentConversation()
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = next.(Model)
	assertConversationReadSelection(t, &m, ids[1], true)
	done := readRemoteResults(cmd)
	awaitReadStore(t, entered)
	token := m.pendingReads[ids[0]].Token
	m.messageCursor = m.indexOfFilteredMessage(ids[0])
	next, duplicate := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = next.(Model)
	if duplicate != nil || m.currentRowMessage().ID != ids[0] || len(m.pendingReads) != 1 || m.pendingReads[ids[0]].Token != token {
		t.Fatal("duplicate expanded request advanced or replaced pending operation")
	}
	// Return to the intentional next target before delivering confirmation.
	m.messageCursor = m.indexOfFilteredMessage(ids[1])
	release <- nil
	m = finishRead(t, m, done)
	assertConversationReadSelection(t, &m, ids[1], true)
}

func TestConversationReadSnapshotBeforeCompletionKeepsCollapsed(t *testing.T) {
	m, entered, release, ids := conversationReadModel(t, []bool{true, false, true})
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = next.(Model)
	done := readRemoteResults(cmd)
	awaitReadStore(t, entered)
	release <- nil
	event := receiveReadResult(t, done) // DB write is confirmed, but the UI has not received it
	snapshot := m.loadMailboxMessagesCmd(m.mailboxes[0].ID)()
	next, _ = m.Update(snapshot)
	m = next.(Model)
	assertConversationReadSelection(t, &m, ids[0], false)
	if len(m.pendingReads) != 3 {
		t.Fatal("passive snapshot unlocked the pending operation")
	}
	next, reload := m.Update(event)
	m = settle(t, next.(Model), reload)
	assertConversationReadSelection(t, &m, ids[0], false)
}

func TestConversationReadReloadPreservesAdvancedTargetOnArrival(t *testing.T) {
	m, entered, release, _ := conversationReadModel(t, []bool{false, false, false})
	mailboxID := m.mailboxes[0].ID
	insert := func(uid uint32, date int64) int64 {
		t.Helper()
		if err := m.db.UpsertMessage(db.Message{MailboxID: mailboxID, UID: uid, MessageID: fmt.Sprintf("<b-%d>", uid), InReplyTo: "<b>", Subject: "B", Date: time.Unix(date, 0)}); err != nil {
			t.Fatal(err)
		}
		messages, err := m.db.ListMessages(mailboxID)
		if err != nil {
			t.Fatal(err)
		}
		for _, msg := range messages {
			if msg.UID == uid {
				return msg.ID
			}
		}
		t.Fatal("inserted message missing")
		return 0
	}
	b1 := insert(4, 50)
	insert(5, 40)
	m.messages, _ = m.db.ListMessages(mailboxID)
	m.applyFilter()
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = next.(Model)
	assertConversationReadSelection(t, &m, b1, false)
	done := readRemoteResults(cmd)
	awaitReadStore(t, entered)
	release <- nil
	next, reload := m.Update(receiveReadResult(t, done))
	m = next.(Model)
	assertConversationReadSelection(t, &m, b1, false)
	// Arrive after completion schedules its SQL reload, before that reload runs.
	b2 := insert(6, 500)
	m = settle(t, m, reload)
	assertConversationReadSelection(t, &m, b1, true)
	if m.currentParentThread() != nil || m.contentMessageID != b1 {
		t.Fatalf("arrival %d replaced the captured target %d or its preview", b2, b1)
	}
}
