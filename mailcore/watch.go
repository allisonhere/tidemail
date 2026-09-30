package mailcore

import "github.com/allisonhere/tidemail/internal/imap"

// Watcher holds one dedicated IMAP connection in IDLE on a mailbox and signals when the server reports
// a change (new mail, expunge). It reconnects by itself after errors. Events carry no payload: react
// by fetching the mailbox normally. Stop it with Close.
type Watcher struct {
	inner *imap.Watcher
}

// NewWatcher starts watching mailbox for account.
func NewWatcher(account Account, mailbox string) *Watcher {
	return &Watcher{inner: imap.NewWatcher(account.config(), mailbox, nil)}
}

// Events signals once per burst of changes (and once after each (re)connect, to cover the gap).
func (w *Watcher) Events() <-chan struct{} { return w.inner.Events() }

// Done is closed when the watcher has stopped for good: Close was called, or the server lacks IDLE.
func (w *Watcher) Done() <-chan struct{} { return w.inner.Done() }

// Close stops the watcher and tears its connection down.
func (w *Watcher) Close() { w.inner.Close() }
