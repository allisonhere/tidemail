package imap

import (
	"context"
	"errors"
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
