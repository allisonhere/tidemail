package imap

import (
	"context"
	"sync"
	"time"

	"github.com/allisonhere/tide/internal/config"
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
	mu       sync.Mutex // serializes operations per account
	client   *Client
	cfg      config.AccountConfig
	lastUsed time.Time
}

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
	s.mu.Lock()
	defer s.mu.Unlock()

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
	for _, s := range entries {
		s.mu.Lock()
		if s.client != nil {
			s.client.Close() //nolint:errcheck
			s.client = nil
		}
		s.mu.Unlock()
	}
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
		s = &session{}
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
		if !s.mu.TryLock() {
			continue
		}
		if s.client != nil && now.Sub(s.lastUsed) > sessionIdleTimeout {
			s.client.Close() //nolint:errcheck
			s.client = nil
		}
		s.mu.Unlock()
	}
}
