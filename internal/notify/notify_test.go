package notify

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

type fakeTransport struct {
	capabilities []string
	capErr       error
	signals      chan *dbus.Signal
	notifyID     uint32
	notifyErr    error
	actions      []string
	closedIDs    []uint32
	closed       int
}

func newFakeTransport(capabilities ...string) *fakeTransport {
	return &fakeTransport{
		capabilities: capabilities,
		signals:      make(chan *dbus.Signal, 8),
		notifyID:     42,
	}
}

func (f *fakeTransport) Capabilities() ([]string, error) {
	return f.capabilities, f.capErr
}

func (f *fakeTransport) Notify(_, _, _, _ string, actions []string) (uint32, error) {
	f.actions = append([]string(nil), actions...)
	return f.notifyID, f.notifyErr
}

func (f *fakeTransport) CloseNotification(id uint32) error {
	f.closedIDs = append(f.closedIDs, id)
	return nil
}

func (f *fakeTransport) Signals() <-chan *dbus.Signal { return f.signals }

func (f *fakeTransport) Close() error {
	f.closed++
	close(f.signals)
	return nil
}

func TestConnectionRequiresActionsCapability(t *testing.T) {
	for _, tc := range []struct {
		name string
		fake *fakeTransport
	}{
		{name: "missing", fake: newFakeTransport("body", "persistence")},
		{name: "lookup failure", fake: func() *fakeTransport {
			f := newFakeTransport()
			f.capErr = errors.New("unavailable")
			return f
		}()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if conn, err := newConnection("tidemail", tc.fake); err == nil || conn != nil {
				t.Fatalf("newConnection() = (%v, %v), want failure", conn, err)
			}
			if tc.fake.closed != 1 {
				t.Fatalf("failed transport closed %d times, want 1", tc.fake.closed)
			}
		})
	}
}

func TestNotifyPreservesActionPairs(t *testing.T) {
	fake := newFakeTransport("actions")
	conn, err := newConnection("tidemail", fake)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	want := []string{"mark-read", "Mark Read", "delete", "Delete", "ignore", "Ignore"}
	id, err := conn.Notify("tidemail", "mail-unread", "Sender", "Subject", want)
	if err != nil || id != fake.notifyID {
		t.Fatalf("Notify() = (%d, %v), want (%d, nil)", id, err, fake.notifyID)
	}
	if !reflect.DeepEqual(fake.actions, want) {
		t.Fatalf("actions = %#v, want %#v", fake.actions, want)
	}
}

func TestSignalsDecodeToEvents(t *testing.T) {
	fake := newFakeTransport("actions")
	conn, err := newConnection("tidemail", fake)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	fake.signals <- &dbus.Signal{Name: iface + ".ActionInvoked", Body: []any{uint32(7), "mark-read"}}
	fake.signals <- &dbus.Signal{Name: iface + ".NotificationClosed", Body: []any{uint32(8), uint32(2)}}

	for _, want := range []Event{{ID: 7, ActionKey: "mark-read"}, {ID: 8, Closed: true}} {
		select {
		case got := <-conn.Events():
			if got != want {
				t.Fatalf("event = %#v, want %#v", got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for %#v", want)
		}
	}
}

func TestMalformedSignalsAreIgnored(t *testing.T) {
	for _, signal := range []*dbus.Signal{
		nil,
		{Name: iface + ".ActionInvoked", Body: []any{uint32(1)}},
		{Name: iface + ".ActionInvoked", Body: []any{"1", "delete"}},
		{Name: iface + ".ActionInvoked", Body: []any{uint32(1), uint32(2)}},
		{Name: iface + ".NotificationClosed", Body: []any{uint32(1), "expired"}},
		{Name: iface + ".Other", Body: []any{uint32(1), "delete"}},
	} {
		if event, ok := decodeSignal(signal); ok {
			t.Fatalf("decodeSignal(%#v) = %#v, want ignored", signal, event)
		}
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	fake := newFakeTransport("actions")
	conn, err := newConnection("tidemail", fake)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if fake.closed != 1 {
		t.Fatalf("transport closed %d times, want 1", fake.closed)
	}
}
