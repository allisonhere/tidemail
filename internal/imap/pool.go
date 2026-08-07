package imap

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
)

// SessionPool maintains one live IMAP connection per account and serializes
// operations on it. Commands previously dialed a fresh connection each — a
// burst of operations (bulk actions, sync-all) then exceeded per-user
// connection caps (Gmail: 15, Dovecot default: 10) and the overflow failed;
// every operation also paid the full TCP+TLS+auth handshake. Connections are
// validated with NOOP before reuse and reaped after sitting idle.
type SessionPool struct {
	mu      sync.Mutex
	entries map[string]*session
	stop    chan struct{}

	// connect and validate are seams for tests.
	connect  func(ctx context.Context, cfg config.AccountConfig) (*Client, error)
	validate func(ctx context.Context, c *Client) error
}

type session struct {
	// lockCh is a capacity-1 channel used as a context-aware mutex: it serializes
	// operations per account, but unlike sync.Mutex a waiter can give up when its
	// ctx expires instead of blocking indefinitely behind a wedged operation.
	lockCh chan struct{}
	client *Client
	// active holds the client while an operation is running fn, so Close can
	// interrupt an in-flight fetch (expire its socket) instead of blocking on the
	// lock until the fetch finishes. Published via atomic Store/Load so Close can
	// read it without holding the per-session lock.
	active   atomic.Pointer[Client]
	cfg      config.AccountConfig
	lastUsed time.Time
}

// lock acquires the session, honoring ctx; it returns ctx.Err() if the caller's
// deadline passes first. unlock releases it.
func (s *session) lock(ctx context.Context) error {
	select {
	case s.lockCh <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *session) unlock() { <-s.lockCh }

const sessionIdleTimeout = 3 * time.Minute

func NewSessionPool() *SessionPool {
	p := &SessionPool{
		entries: map[string]*session{},
		stop:    make(chan struct{}),
		connect: func(ctx context.Context, cfg config.AccountConfig) (*Client, error) {
			c := New(cfg)
			if err := c.Connect(ctx); err != nil {
				return nil, err
			}
			return c, nil
		},
		validate: func(ctx context.Context, c *Client) error { return c.Noop(ctx) },
	}
	go p.reapLoop()
	return p
}

// Do runs fn with a connected client for the account, reusing the account's
// live connection when possible. Operations for the same account run one at a
// time; different accounts proceed in parallel. The connection is kept open
// for later operations — fn must not close it.
func (p *SessionPool) Do(ctx context.Context, acfg config.AccountConfig, fn func(*Client) error) error {
	s := p.entry(acfg)
	if err := s.lock(ctx); err != nil {
		return err
	}
	defer s.unlock()

	// A changed account config (edited credentials, host) invalidates the
	// cached connection; so does failing the NOOP liveness probe.
	if s.client != nil && s.cfg != acfg {
		s.client.Close() //nolint:errcheck
		s.client = nil
	}
	if s.client != nil {
		if err := p.validate(ctx, s.client); err != nil {
			s.client.Close() //nolint:errcheck
			s.client = nil
		}
	}
	if s.client == nil {
		client, err := p.connect(ctx, acfg)
		if err != nil {
			return err
		}
		s.client = client
		s.cfg = acfg
	}
	s.lastUsed = time.Now()
	// Publish the live client so Close can interrupt this operation on shutdown.
	s.active.Store(s.client)
	defer s.active.Store(nil)
	return fn(s.client)
}

// Close shuts down the reaper and closes all pooled connections.
func (p *SessionPool) Close() {
	close(p.stop)
	p.mu.Lock()
	entries := make([]*session, 0, len(p.entries))
	for _, s := range p.entries {
		entries = append(entries, s)
	}
	p.mu.Unlock()
	// Close concurrently so a slow LOGOUT on one account can't serialize behind
	// the others — quit time is bounded by the slowest single close, not the sum.
	var wg sync.WaitGroup
	for _, s := range entries {
		wg.Add(1)
		go func(s *session) {
			defer wg.Done()
			// Abort any in-flight operation first so acquiring the lock doesn't
			// wait out a full network fetch; the interrupted op then releases it.
			if c := s.active.Load(); c != nil {
				c.interrupt()
			}
			s.lockCh <- struct{}{}
			if s.client != nil {
				s.client.Close() //nolint:errcheck
				s.client = nil
			}
			<-s.lockCh
		}(s)
	}
	wg.Wait()
}

func (p *SessionPool) entry(acfg config.AccountConfig) *session {
	key := acfg.Name
	if key == "" {
		key = acfg.User + "@" + acfg.IMAPHost
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.entries[key]
	if s == nil {
		s = &session{lockCh: make(chan struct{}, 1)}
		p.entries[key] = s
	}
	return s
}

func (p *SessionPool) reapLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-p.stop:
			return
		case <-ticker.C:
			p.reapIdle(time.Now())
		}
	}
}

// reapIdle closes connections that have sat unused past the idle timeout.
// TryLock skips sessions with an operation in flight rather than waiting.
func (p *SessionPool) reapIdle(now time.Time) {
	p.mu.Lock()
	entries := make([]*session, 0, len(p.entries))
	for _, s := range p.entries {
		entries = append(entries, s)
	}
	p.mu.Unlock()
	for _, s := range entries {
		// TryLock-equivalent: skip sessions with an operation in flight rather
		// than waiting on them.
		select {
		case s.lockCh <- struct{}{}:
		default:
			continue
		}
		if s.client != nil && now.Sub(s.lastUsed) > sessionIdleTimeout {
			s.client.Close() //nolint:errcheck
			s.client = nil
		}
		<-s.lockCh
	}
}
