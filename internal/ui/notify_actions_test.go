package ui

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

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
	wantActions := []string{"mark-read", "Mark Read", "delete", "Delete", "ignore", "Ignore"}
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
	for _, action := range []string{"mark-read", "delete", "ignore", "unknown"} {
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
			case "ignore", "unknown":
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
