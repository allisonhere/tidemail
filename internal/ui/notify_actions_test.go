package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/notify"
)

type fakeNotifyConn struct {
	events    chan notify.Event
	notifyID  uint32
	notifyErr error
	actions   []string
	closedIDs []uint32
	closed    bool
}

func newFakeNotifyConn() *fakeNotifyConn {
	return &fakeNotifyConn{events: make(chan notify.Event, 8), notifyID: 41}
}

func (f *fakeNotifyConn) Notify(_, _, _, _ string, actions []string) (uint32, error) {
	f.actions = append([]string(nil), actions...)
	return f.notifyID, f.notifyErr
}

func (f *fakeNotifyConn) CloseNotification(id uint32) error {
	f.closedIDs = append(f.closedIDs, id)
	return nil
}

func (f *fakeNotifyConn) Events() <-chan notify.Event { return f.events }

func (f *fakeNotifyConn) Close() error {
	if !f.closed {
		f.closed = true
		close(f.events)
	}
	return nil
}

func TestUseActionableNotify(t *testing.T) {
	conn := newFakeNotifyConn()
	for _, tc := range []struct {
		name  string
		goos  string
		count int
		conn  notify.Conn
		want  bool
	}{
		{name: "one on Linux", goos: "linux", count: 1, conn: conn, want: true},
		{name: "batch", goos: "linux", count: 2, conn: conn, want: true},
		{name: "no connection", goos: "linux", count: 1},
		{name: "zero", goos: "linux", count: 0, conn: conn},
		{name: "macOS", goos: "darwin", count: 1, conn: conn},
		{name: "Windows", goos: "windows", count: 1, conn: conn},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := useActionableNotify(tc.goos, tc.count, tc.conn); got != tc.want {
				t.Fatalf("useActionableNotify(%q, %d, conn=%t) = %t, want %t", tc.goos, tc.count, tc.conn != nil, got, tc.want)
			}
		})
	}
}

func TestActionableNotificationRegistersMessageAndActions(t *testing.T) {
	f := newUndoTestFixture(t, 1)
	m := f.model
	conn := newFakeNotifyConn()
	m.notifier = conn

	got := m.notifyActionableCmd(f.source.ID, []db.Message{f.messages[0]})()
	sent, ok := got.(notifySentMsg)
	if !ok {
		t.Fatalf("command returned %T, want notifySentMsg", got)
	}
	if sent.notifyID != conn.notifyID || !reflect.DeepEqual(sent.messageIDs, []int64{f.messages[0].ID}) {
		t.Fatalf("sent = %#v", sent)
	}
	wantActions := []string{"default", "Open", "mark-read", "Mark Read", "delete", "Delete", "read", "Read"}
	if !reflect.DeepEqual(conn.actions, wantActions) {
		t.Fatalf("actions = %#v, want %#v", conn.actions, wantActions)
	}

	next, _ := m.handleNotifySent(sent)
	m = next.(Model)
	if !reflect.DeepEqual(m.pendingNotifyActions[conn.notifyID], []int64{f.messages[0].ID}) {
		t.Fatalf("notification map = %#v", m.pendingNotifyActions)
	}
}

func TestNotificationActionsDispatch(t *testing.T) {
	for _, action := range []string{"mark-read", "delete", "unknown"} {
		t.Run(action, func(t *testing.T) {
			f := newUndoTestFixture(t, 1)
			m := f.model
			conn := newFakeNotifyConn()
			m.notifier = conn
			m.pendingNotifyActions[9] = []int64{f.messages[0].ID}

			next, _ := m.handleNotifyActionEvent(notifyActionEventMsg{event: notify.Event{ID: 9, ActionKey: action}})
			m = next.(Model)

			if _, ok := m.pendingNotifyActions[9]; ok {
				t.Fatal("handled notification stayed pending")
			}
			if !reflect.DeepEqual(conn.closedIDs, []uint32{9}) {
				t.Fatalf("closed IDs = %#v, want [9]", conn.closedIDs)
			}
			switch action {
			case "mark-read":
				if pending, ok := m.pendingReads[f.messages[0].ID]; !ok || !pending.Read {
					t.Fatalf("mark-read did not reserve a read update: %#v", m.pendingReads)
				}
				if len(m.pendingDestructiveActions) != 0 {
					t.Fatal("mark-read scheduled a delete")
				}
			case "delete":
				if len(m.pendingDestructiveActions) != 1 || m.pendingDestructiveActions[0].Kind != destructiveDelete {
					t.Fatalf("delete action = %#v", m.pendingDestructiveActions)
				}
			case "unknown":
				if len(m.pendingReads) != 0 || len(m.pendingDestructiveActions) != 0 {
					t.Fatalf("%s changed message state", action)
				}
			}
		})
	}
}

// TestBatchNotificationCoversEveryNewMessage guards the fix for a sync that
// pulls in more than one new message: previously useActionableNotify refused
// anything but a single message, so a 2+ message batch silently fell back to
// a plain, button-less notify-send every time. One notification should now
// cover the whole batch, and Mark Read/Delete should apply to all of it.
func TestBatchNotificationCoversEveryNewMessage(t *testing.T) {
	f := newUndoTestFixture(t, 3)
	m := f.model
	conn := newFakeNotifyConn()
	m.notifier = conn

	if !useActionableNotify("linux", len(f.messages), conn) {
		t.Fatal("expected a 3-message batch to still use the actionable path")
	}

	got := m.notifyActionableCmd(f.source.ID, f.messages)()
	sent, ok := got.(notifySentMsg)
	if !ok {
		t.Fatalf("command returned %T, want notifySentMsg", got)
	}
	wantIDs := []int64{f.messages[0].ID, f.messages[1].ID, f.messages[2].ID}
	if !reflect.DeepEqual(sent.messageIDs, wantIDs) {
		t.Fatalf("sent.messageIDs = %#v, want %#v", sent.messageIDs, wantIDs)
	}

	next, _ := m.handleNotifySent(sent)
	m = next.(Model)

	next, _ = m.handleNotifyActionEvent(notifyActionEventMsg{event: notify.Event{ID: sent.notifyID, ActionKey: "delete"}})
	m = next.(Model)
	if len(m.pendingDestructiveActions) != 1 || len(m.pendingDestructiveActions[0].Entries) != 3 {
		t.Fatalf("expected one delete action covering all 3 messages: %#v", m.pendingDestructiveActions)
	}
}

func TestClosedAndStaleNotificationEventsAreHarmless(t *testing.T) {
	f := newUndoTestFixture(t, 1)
	m := f.model
	conn := newFakeNotifyConn()
	m.notifier = conn
	m.pendingNotifyActions[4] = []int64{f.messages[0].ID}

	next, _ := m.handleNotifyActionEvent(notifyActionEventMsg{event: notify.Event{ID: 4, Closed: true}})
	m = next.(Model)
	if _, ok := m.pendingNotifyActions[4]; ok {
		t.Fatal("closed notification stayed pending")
	}
	if len(m.pendingReads) != 0 || len(m.pendingDestructiveActions) != 0 {
		t.Fatal("closed notification changed message state")
	}

	next, _ = m.handleNotifyActionEvent(notifyActionEventMsg{event: notify.Event{ID: 99, ActionKey: "unknown"}})
	m = next.(Model)
	if len(m.earlyNotifyActions) != 0 {
		t.Fatalf("unknown stale event was buffered: %#v", m.earlyNotifyActions)
	}
}

func TestEarlyNotificationActionRunsAfterIDRegistration(t *testing.T) {
	f := newUndoTestFixture(t, 1)
	m := f.model
	conn := newFakeNotifyConn()
	m.notifier = conn

	next, _ := m.handleNotifyActionEvent(notifyActionEventMsg{event: notify.Event{ID: 12, ActionKey: "delete"}})
	m = next.(Model)
	if _, ok := m.earlyNotifyActions[12]; !ok {
		t.Fatal("early action was not buffered")
	}

	next, _ = m.handleNotifySent(notifySentMsg{notifyID: 12, messageIDs: []int64{f.messages[0].ID}})
	m = next.(Model)
	if len(m.pendingDestructiveActions) != 1 || m.pendingDestructiveActions[0].Kind != destructiveDelete {
		t.Fatalf("buffered delete was not dispatched: %#v", m.pendingDestructiveActions)
	}
}

func TestActionableNotificationFailureReturnsNoTrackingMessage(t *testing.T) {
	// The failure path falls back to notify-send. Keep this test from
	// launching the real desktop notifier on the developer's machine.
	t.Setenv("PATH", t.TempDir())
	f := newUndoTestFixture(t, 1)
	m := f.model
	conn := newFakeNotifyConn()
	conn.notifyErr = errors.New("send failed")
	m.notifier = conn

	if got := m.notifyActionableCmd(f.source.ID, []db.Message{f.messages[0]})(); got != nil {
		t.Fatalf("failed notification returned %T, want nil fallback", got)
	}
}

// TestLogNotifyWritesToFetchLog guards the fix for a notifier that failed to
// open: previously the error from notify.Open was silently discarded, so a
// daemon without the "actions" capability (or any other D-Bus hiccup) turned
// off actionable notifications for the whole session with no way to tell
// why. logNotify is what openNotifierCmd now calls on that error path.
func TestLogNotifyWritesToFetchLog(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, err := config.LogPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}

	logNotify("notify\terror=%q", errors.New("daemon has no actions capability"))

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "daemon has no actions capability") {
		t.Fatalf("fetch.log doesn't contain the notifier error: %q", data)
	}
}

func TestCloseSessionsClosesNotifier(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	conn := newFakeNotifyConn()
	m.notifier = conn
	m.CloseSessions()
	if !conn.closed {
		t.Fatal("CloseSessions left notifier open")
	}
}

func TestNotificationClickOpensExactMessage(t *testing.T) {
	for _, action := range []string{"default", "read"} {
		t.Run(action, func(t *testing.T) { testNotificationOpensExactMessage(t, action) })
	}
}

func testNotificationOpensExactMessage(t *testing.T, action string) {
	t.Helper()
	for _, markRead := range []bool{false, true} {
		t.Run(fmt.Sprint(markRead), func(t *testing.T) {
			f := newUndoTestFixture(t, 3)
			m := f.model
			target := f.messages[1]
			if err := f.database.MarkRead(target.ID, false); err != nil {
				t.Fatal(err)
			}
			m.cfg.Display.MarkReadOnOpen = markRead
			m.searchMode = true
			m.searchQuery = "does not match"
			m.showUnreadOnly = true
			m.collapsedAccounts[f.source.AccountID] = true
			m.rebuildSidebar()
			m.pendingNotifyActions[7] = []int64{target.ID, f.messages[0].ID}
			next, _ := m.handleNotifyActionEvent(notifyActionEventMsg{event: notify.Event{ID: 7, ActionKey: action}})
			m = next.(Model)
			if m.contentMessageID != target.ID || m.focused != paneContent {
				t.Fatalf("reader = %d focus = %v, want %d in reader", m.contentMessageID, m.focused, target.ID)
			}
			if cur := m.currentRowMessage(); cur == nil || cur.ID != target.ID {
				t.Fatalf("selected = %+v", cur)
			}
			if selected := m.selectedMailbox(); selected == nil || selected.ID != target.MailboxID {
				t.Fatalf("mailbox = %+v", selected)
			}
			if m.searchActive() || m.showUnreadOnly {
				t.Fatal("old filters retained")
			}
			_, pending := m.pendingReads[target.ID]
			if pending != markRead {
				t.Fatalf("mark-read pending = %v, want %v", pending, markRead)
			}
			if len(m.pendingReads) > 1 {
				t.Fatal("opening batch marked other messages read")
			}
		})
	}
}

func TestNotificationClickWaitsForCompose(t *testing.T) {
	f := newUndoTestFixture(t, 1)
	m := f.model
	m.compose = NewComposeFromDraft(db.Draft{BodyText: "keep this draft"}, m.cfg.Accounts, nil)
	m.overlay = overlayCompose
	m.pendingNotifyActions[7] = []int64{f.messages[0].ID}
	next, _ := m.Update(notifyActionEventMsg{event: notify.Event{ID: 7, ActionKey: "default"}})
	m = next.(Model)
	if m.overlay != overlayCompose || m.compose.bodyInput.Value() != "keep this draft" || m.pendingNotificationMessage != f.messages[0].ID {
		t.Fatal("notification disrupted compose or lost pending message")
	}
	m.overlay = overlayNone
	next, _ = m.Update(struct{}{})
	m = next.(Model)
	if m.contentMessageID != f.messages[0].ID || m.focused != paneContent || m.pendingNotificationMessage != 0 {
		t.Fatal("message did not open after dialog closed")
	}
	if strings.Contains(m.statusMsg, "Close the current dialog") {
		t.Fatal("stale dialog instruction remained after opening")
	}
}

func TestNotificationClickEarlyAndMissing(t *testing.T) {
	f := newUndoTestFixture(t, 1)
	m := f.model
	next, _ := m.handleNotifyActionEvent(notifyActionEventMsg{event: notify.Event{ID: 7, ActionKey: "default"}})
	m = next.(Model)
	next, _ = m.handleNotifySent(notifySentMsg{notifyID: 7, messageIDs: []int64{f.messages[0].ID}})
	m = next.(Model)
	if m.contentMessageID != f.messages[0].ID {
		t.Fatal("early click lost")
	}
	m.pendingNotifyActions[8] = []int64{999999, f.messages[0].ID}
	next, _ = m.handleNotifyActionEvent(notifyActionEventMsg{event: notify.Event{ID: 8, ActionKey: "default"}})
	m = next.(Model)
	if !strings.Contains(m.statusMsg, "no longer available") {
		t.Fatalf("missing message status = %q", m.statusMsg)
	}
}

func TestNotificationClickFollowsMovedMessage(t *testing.T) {
	f := newUndoTestFixture(t, 1)
	m := f.model
	id := f.messages[0].ID
	if err := f.database.MoveMessage(id, f.target.ID); err != nil {
		t.Fatal(err)
	}
	m.collapsedSections[fmt.Sprintf("personal:%d", f.target.AccountID)] = true
	m.pendingNotifyActions[7] = []int64{id}
	next, _ := m.Update(notifyActionEventMsg{event: notify.Event{ID: 7, ActionKey: "default"}})
	m = next.(Model)
	if mailbox := m.selectedMailbox(); mailbox == nil || mailbox.ID != f.target.ID {
		t.Fatalf("wrong destination: %+v", mailbox)
	}
	if m.contentMessageID != id || m.focused != paneContent {
		t.Fatal("moved message not opened")
	}
}

func TestNotificationClickRevealsThreadChild(t *testing.T) {
	f := newUndoTestFixture(t, 2)
	m := f.model
	m.cfg.Display.ThreadedConversations = true
	root, reply := f.messages[0], f.messages[1]
	root.MessageID = "<notification-root@example.com>"
	reply.MessageID = "<notification-reply@example.com>"
	reply.InReplyTo = root.MessageID
	root.Date, reply.Date = time.Unix(100, 0), time.Unix(200, 0)
	for _, msg := range []db.Message{root, reply} {
		if err := f.database.UpsertMessage(msg); err != nil {
			t.Fatal(err)
		}
	}
	m.pendingNotifyActions[7] = []int64{root.ID}
	next, _ := m.Update(notifyActionEventMsg{event: notify.Event{ID: 7, ActionKey: "default"}})
	m = next.(Model)
	if m.contentMessageID != root.ID {
		t.Fatalf("opened %d instead of notified child %d", m.contentMessageID, root.ID)
	}
	if current := m.currentRowMessage(); current == nil || current.ID != root.ID {
		t.Fatalf("wrong row: %+v", current)
	}
}
