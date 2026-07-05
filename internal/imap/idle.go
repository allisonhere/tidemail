package imap

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/allisonhere/tide/internal/config"
	"github.com/emersion/go-imap/v2"
)

// ErrIdleUnsupported reports a server that advertises neither IDLE nor
// IMAP4rev2; the watcher stops permanently and interval polling remains the
// only refresh path for that account.
var ErrIdleUnsupported = errors.New("server does not support IDLE")

// SupportsIdle reports whether the connected server can run the IDLE command.
func (c *Client) SupportsIdle(ctx context.Context) bool {
	if c.conn == nil {
		return false
	}
	defer c.applyDeadline(ctx)()
	caps := c.conn.Caps()
	return caps.Has(imap.CapIdle) || caps.Has(imap.CapIMAP4rev2)
}

// SelectReadOnly selects a mailbox without the side effects of a writable
// SELECT (EXAMINE semantics) and returns its current message count; the IDLE
// watcher only observes.
func (c *Client) SelectReadOnly(ctx context.Context, mailboxName string) (uint32, error) {
	if c.conn == nil {
		return 0, fmt.Errorf("not connected")
	}
	defer c.applyDeadline(ctx)()
	data, err := c.conn.Select(mailboxName, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return 0, fmt.Errorf("select %s: %w", mailboxName, err)
	}
	return data.NumMessages, nil
}

// IdleWait runs IDLE on the selected mailbox until ctx is canceled or the
// connection dies. Unilateral updates flow through the onUpdate callback
// registered at construction (NewWithUpdates); go-imap restarts the IDLE
// command itself every 28 minutes to dodge server inactivity timeouts.
func (c *Client) IdleWait(ctx context.Context) error {
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}
	idle, err := c.conn.Idle()
	if err != nil {
		return fmt.Errorf("idle: %w", err)
	}
	waitErr := make(chan error, 1)
	go func() { waitErr <- idle.Wait() }()
	select {
	case <-ctx.Done():
		// Expire the socket first so the DONE write (and any pending read)
		// can't hang on a wedged connection.
		if c.netConn != nil {
			c.netConn.SetDeadline(time.Now()) //nolint:errcheck
		}
		idle.Close() //nolint:errcheck
		<-waitErr
		return ctx.Err()
	case err := <-waitErr:
		if err == nil {
			err = fmt.Errorf("idle ended")
		}
		return err
	}
}

// Watcher holds one dedicated IMAP connection in IDLE on a single mailbox and
// emits a coalesced event whenever the server reports a change (new mail,
// expunge). It is deliberately separate from the SessionPool: IDLE monopolizes
// its connection, so parking it in the pool would serialize every other
// operation for the account behind it.
//
// The watcher reconnects with exponential backoff after errors and stops
// permanently only when the server lacks IDLE support or Close is called.
// Events carry no payload — consumers react by running a normal sync, which
// already handles reconciliation, notifications, and filters.
type Watcher struct {
	cfg     config.AccountConfig
	mailbox string
	logf    func(format string, args ...any)

	events chan struct{}
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	// lastCount tracks the selected mailbox's message count so only genuine
	// count changes emit events. Servers echo unilateral data (FLAGS chatter,
	// EXISTS re-announcements) to an idling session whenever *another* session
	// touches the mailbox — including this app's own sync — and treating that
	// echo as "new mail" creates a sync→echo→sync feedback loop. -1 = unknown.
	// Guarded by mu; the callback runs on the connection's read goroutine.
	mu        sync.Mutex
	lastCount int64

	// watchOnce is a seam for tests.
	watchOnce func() error
}

// NewWatcher starts watching mailbox on a dedicated connection. logf receives
// diagnostic lines (reconnects, unsupported servers) and may be nil.
func NewWatcher(cfg config.AccountConfig, mailbox string, logf func(format string, args ...any)) *Watcher {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &Watcher{
		cfg:     cfg,
		mailbox: mailbox,
		logf:    logf,
		events:  make(chan struct{}, 1),
		ctx:     ctx,
		cancel:  cancel,
		done:    make(chan struct{}),
	}
	w.watchOnce = w.connectAndIdle
	go w.run()
	return w
}

// Events emits one (coalesced) signal per burst of server-side changes. The
// channel is never closed; select against Done for shutdown.
func (w *Watcher) Events() <-chan struct{} { return w.events }

// Done is closed when the watcher has stopped for good (Close called or the
// server lacks IDLE support).
func (w *Watcher) Done() <-chan struct{} { return w.done }

// Close stops the watcher and waits for its connection to be torn down.
func (w *Watcher) Close() {
	w.cancel()
	<-w.done
}

func (w *Watcher) run() {
	defer close(w.done)
	const (
		initialBackoff = 30 * time.Second
		maxBackoff     = 10 * time.Minute
		stableAfter    = 5 * time.Minute
	)
	backoff := initialBackoff
	for {
		start := time.Now()
		err := w.watchOnce()
		if w.ctx.Err() != nil {
			return
		}
		if errors.Is(err, ErrIdleUnsupported) {
			w.logf("idle\taccount=%s mailbox=%s server lacks IDLE; falling back to interval polling", w.cfg.Name, w.mailbox)
			return
		}
		// A connection that survived a while earns a fresh backoff; rapid-fire
		// failures (bad credentials, unreachable host) keep doubling.
		if time.Since(start) >= stableAfter {
			backoff = initialBackoff
		}
		w.logf("idle\taccount=%s mailbox=%s watch ended error=%q retry=%v", w.cfg.Name, w.mailbox, err, backoff)
		select {
		case <-time.After(backoff):
		case <-w.ctx.Done():
			return
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

func (w *Watcher) connectAndIdle() error {
	client := NewWithUpdates(w.cfg, w.handleUpdate)
	setupCtx, cancel := context.WithTimeout(w.ctx, 30*time.Second)
	defer cancel()
	if err := client.Connect(setupCtx); err != nil {
		return err
	}
	defer client.Close()
	if !client.SupportsIdle(setupCtx) {
		return ErrIdleUnsupported
	}
	count, err := client.SelectReadOnly(setupCtx, w.mailbox)
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.lastCount = int64(count)
	w.mu.Unlock()
	// Mail may have arrived while this watcher was disconnected; one event on
	// (re)connect closes that gap. The consumer's sync is an incremental SINCE
	// fetch, so a spurious nudge is cheap.
	w.notify()
	return client.IdleWait(w.ctx)
}

// handleUpdate filters unilateral mailbox data down to genuine changes.
// Flags-only chatter is dropped, and an EXISTS that re-announces the count we
// already know is dropped — both are routinely echoed to an idling session
// when another connection (including this app's own sync) selects the same
// mailbox, and reacting to them loops sync→echo→sync forever.
func (w *Watcher) handleUpdate(u MailboxUpdate) {
	w.mu.Lock()
	switch {
	case u.Expunged:
		// The count just shrank by an amount we can't track precisely; mark it
		// unknown so the next EXISTS is always treated as a change.
		w.lastCount = -1
		w.mu.Unlock()
		w.logf("idle\taccount=%s event=expunge", w.cfg.Name)
	case u.NumMessages == nil:
		w.mu.Unlock()
		return
	default:
		n := int64(*u.NumMessages)
		if n == w.lastCount {
			w.mu.Unlock()
			return
		}
		prev := w.lastCount
		w.lastCount = n
		w.mu.Unlock()
		w.logf("idle\taccount=%s event=exists count=%d->%d", w.cfg.Name, prev, n)
	}
	w.notify()
}

// notify coalesces: a full buffer already promises a sync, so extra signals
// during a burst are dropped rather than queued.
func (w *Watcher) notify() {
	select {
	case w.events <- struct{}{}:
	default:
	}
}
