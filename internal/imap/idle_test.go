package imap

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
)

func idleTestConfig(port int) config.AccountConfig {
	return config.AccountConfig{
		Name:     "idle-test",
		IMAPHost: "127.0.0.1",
		IMAPPort: port,
		IMAPTLS:  false,
		User:     "testuser",
		Password: "testpass",
	}
}

func TestWatcherEmitsEventOnNewMail(t *testing.T) {
	port, cleanup := startTestServer(t)
	defer cleanup()

	w := NewWatcher(idleTestConfig(port), "INBOX", t.Logf)
	defer w.Close()

	// The watcher emits one gap-repair event right after (re)connecting;
	// consume it so the next event observed is the push for new mail.
	select {
	case <-w.Events():
	case <-time.After(5 * time.Second):
		t.Fatal("expected the initial connect event")
	}

	// Give the watcher a moment to enter IDLE after the connect event, then
	// deliver mail via a second connection.
	time.Sleep(200 * time.Millisecond)
	appendTestMessage(t, port, "INBOX", false)

	select {
	case <-w.Events():
	case <-time.After(5 * time.Second):
		t.Fatal("expected an IDLE push event after new mail arrived")
	}
}

func TestWatcherCloseStopsPromptly(t *testing.T) {
	port, cleanup := startTestServer(t)
	defer cleanup()

	w := NewWatcher(idleTestConfig(port), "INBOX", t.Logf)
	select {
	case <-w.Events():
	case <-time.After(5 * time.Second):
		t.Fatal("expected the initial connect event")
	}

	done := make(chan struct{})
	go func() {
		w.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not stop a mid-IDLE watcher promptly")
	}
	select {
	case <-w.Done():
	default:
		t.Fatal("expected Done to be closed after Close")
	}
}

// newWatcherForTest builds a watcher whose network layer is replaced by
// watchOnce, mirroring NewWatcher's wiring.
func newWatcherForTest(watchOnce func() error) *Watcher {
	ctx, cancel := context.WithCancel(context.Background())
	w := &Watcher{
		logf:   func(string, ...any) {},
		events: make(chan struct{}, 1),
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
	}
	w.watchOnce = watchOnce
	go w.run()
	return w
}

func TestWatcherStopsWhenIdleUnsupported(t *testing.T) {
	w := newWatcherForTest(func() error { return ErrIdleUnsupported })
	select {
	case <-w.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("expected the watcher to stop permanently on ErrIdleUnsupported")
	}
	w.Close() // must be safe after a self-stop
}

func TestWatcherBackoffRetriesAfterError(t *testing.T) {
	attempts := make(chan struct{}, 8)
	w := newWatcherForTest(func() error {
		attempts <- struct{}{}
		return errors.New("boom")
	})
	defer w.Close()

	select {
	case <-attempts:
	case <-time.After(2 * time.Second):
		t.Fatal("expected a first watch attempt")
	}
	// A retry only happens after the backoff (30s) — don't wait for it; just
	// confirm the watcher hasn't stopped for good on a plain error.
	select {
	case <-w.Done():
		t.Fatal("watcher must keep retrying after a transient error")
	case <-time.After(100 * time.Millisecond):
	}
}

// TestWatcherHandleUpdateFiltersEcho guards against the sync feedback loop:
// servers echo FLAGS-only unilateral data and same-count EXISTS to an idling
// session whenever another connection (like the app's own sync) selects the
// mailbox. Reacting to those echoes made the watcher trigger a sync every
// second forever.
func TestWatcherHandleUpdateFiltersEcho(t *testing.T) {
	w := &Watcher{events: make(chan struct{}, 1), lastCount: 2, logf: func(string, ...any) {}}
	num := func(n uint32) *uint32 { return &n }
	drained := func() bool {
		select {
		case <-w.events:
			return true
		default:
			return false
		}
	}

	w.handleUpdate(MailboxUpdate{}) // flags-only chatter
	if drained() {
		t.Fatal("flags-only unilateral data must not emit an event")
	}
	w.handleUpdate(MailboxUpdate{NumMessages: num(2)}) // same-count EXISTS echo
	if drained() {
		t.Fatal("an EXISTS re-announcing the known count must not emit an event")
	}
	w.handleUpdate(MailboxUpdate{NumMessages: num(3)}) // genuine new mail
	if !drained() {
		t.Fatal("a count change must emit an event")
	}
	w.handleUpdate(MailboxUpdate{Expunged: true}) // deletion
	if !drained() {
		t.Fatal("an expunge must emit an event")
	}
	// After an expunge the count is unknown: the next EXISTS always counts.
	w.handleUpdate(MailboxUpdate{NumMessages: num(3)})
	if !drained() {
		t.Fatal("the first EXISTS after an expunge must emit an event")
	}
	w.handleUpdate(MailboxUpdate{NumMessages: num(3)})
	if drained() {
		t.Fatal("a repeat EXISTS after resync must be filtered again")
	}
}
