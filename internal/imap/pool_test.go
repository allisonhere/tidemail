package imap

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/allisonhere/tide/internal/config"
)

func newTestPool() (*SessionPool, *atomic.Int32) {
	var dials atomic.Int32
	p := &SessionPool{
		entries: map[string]*session{},
		stop:    make(chan struct{}),
		connect: func(ctx context.Context, cfg config.AccountConfig) (*Client, error) {
			dials.Add(1)
			return &Client{cfg: cfg}, nil
		},
		validate: func(ctx context.Context, c *Client) error { return nil },
	}
	return p, &dials
}

func TestSessionPoolReusesConnectionPerAccount(t *testing.T) {
	p, dials := newTestPool()
	acfg := config.AccountConfig{Name: "Acct", IMAPHost: "imap.x", User: "a@x"}

	for i := 0; i < 3; i++ {
		if err := p.Do(context.Background(), acfg, func(c *Client) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	if got := dials.Load(); got != 1 {
		t.Fatalf("expected 1 dial for 3 operations, got %d", got)
	}

	other := config.AccountConfig{Name: "Other", IMAPHost: "imap.y", User: "b@y"}
	if err := p.Do(context.Background(), other, func(c *Client) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if got := dials.Load(); got != 2 {
		t.Fatalf("expected a second dial for a different account, got %d", got)
	}
}

func TestSessionPoolRedialsOnConfigChangeAndFailedValidation(t *testing.T) {
	p, dials := newTestPool()
	acfg := config.AccountConfig{Name: "Acct", IMAPHost: "imap.x", User: "a@x"}

	if err := p.Do(context.Background(), acfg, func(c *Client) error { return nil }); err != nil {
		t.Fatal(err)
	}

	// Edited credentials must not reuse the old connection.
	changed := acfg
	changed.Password = "new-secret"
	if err := p.Do(context.Background(), changed, func(c *Client) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if got := dials.Load(); got != 2 {
		t.Fatalf("expected redial after config change, got %d dials", got)
	}

	// A dead connection (failed NOOP) must be replaced.
	p.validate = func(ctx context.Context, c *Client) error { return errors.New("connection reset") }
	if err := p.Do(context.Background(), changed, func(c *Client) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if got := dials.Load(); got != 3 {
		t.Fatalf("expected redial after failed validation, got %d dials", got)
	}
}

func TestSessionPoolSerializesPerAccount(t *testing.T) {
	p, _ := newTestPool()
	acfg := config.AccountConfig{Name: "Acct", IMAPHost: "imap.x", User: "a@x"}

	var inFlight, maxInFlight atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = p.Do(context.Background(), acfg, func(c *Client) error {
				n := inFlight.Add(1)
				if m := maxInFlight.Load(); n > m {
					maxInFlight.Store(n)
				}
				time.Sleep(5 * time.Millisecond)
				inFlight.Add(-1)
				return nil
			})
		}()
	}
	wg.Wait()
	if got := maxInFlight.Load(); got != 1 {
		t.Fatalf("expected operations serialized (max 1 in flight), got %d", got)
	}
}

func TestSessionPoolDoRespectsContextWhileLockHeld(t *testing.T) {
	p, _ := newTestPool()
	acfg := config.AccountConfig{Name: "Acct", IMAPHost: "imap.x", User: "a@x"}

	// Hold the per-account lock from a goroutine until we release it, simulating a
	// wedged in-flight operation.
	holding := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_ = p.Do(context.Background(), acfg, func(c *Client) error {
			close(holding)
			<-release
			return nil
		})
	}()
	<-holding
	defer close(release)

	// A second operation for the same account with a short deadline must give up
	// with a context error instead of blocking forever behind the held lock.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- p.Do(ctx, acfg, func(c *Client) error { return nil })
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected context deadline error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Do blocked on a held lock instead of honoring its context")
	}
}

// Close must abort an in-flight operation by expiring its socket rather than
// waiting for the full network op to finish, so quitting is fast even mid-fetch.
func TestSessionPoolCloseInterruptsInFlightOp(t *testing.T) {
	local, remote := net.Pipe()
	defer local.Close()
	defer remote.Close()

	p := &SessionPool{
		entries: map[string]*session{},
		stop:    make(chan struct{}),
		connect: func(ctx context.Context, cfg config.AccountConfig) (*Client, error) {
			return &Client{cfg: cfg, netConn: local}, nil
		},
		validate: func(ctx context.Context, c *Client) error { return nil },
	}
	acfg := config.AccountConfig{Name: "Acct", IMAPHost: "imap.x", User: "a@x"}

	started := make(chan struct{})
	opDone := make(chan error, 1)
	go func() {
		opDone <- p.Do(context.Background(), acfg, func(c *Client) error {
			close(started)
			// Blocks forever unless the socket deadline is expired by Close.
			_, err := c.netConn.Read(make([]byte, 1))
			return err
		})
	}()
	<-started
	time.Sleep(20 * time.Millisecond) // ensure the read is actually blocking

	closed := make(chan struct{})
	start := time.Now()
	go func() { p.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close blocked on the in-flight op instead of interrupting it")
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("Close took %v; in-flight op was not interrupted", d)
	}

	// The interrupted operation must unwind with an error (its socket expired).
	select {
	case err := <-opDone:
		if err == nil {
			t.Fatal("expected the interrupted op to return an error")
		}
	case <-time.After(time.Second):
		t.Fatal("interrupted op did not return")
	}
}

func TestSessionPoolReapsIdleConnections(t *testing.T) {
	p, dials := newTestPool()
	acfg := config.AccountConfig{Name: "Acct", IMAPHost: "imap.x", User: "a@x"}

	if err := p.Do(context.Background(), acfg, func(c *Client) error { return nil }); err != nil {
		t.Fatal(err)
	}
	p.reapIdle(time.Now().Add(sessionIdleTimeout + time.Second))

	if err := p.Do(context.Background(), acfg, func(c *Client) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if got := dials.Load(); got != 2 {
		t.Fatalf("expected redial after idle reap, got %d dials", got)
	}
}
