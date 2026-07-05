package ui

import (
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	imapClient "github.com/allisonhere/tide/internal/imap"
)

// IdleEventMsg reports that an account's IDLE watcher saw a server-side change
// (or stopped). Watcher identifies which generation of watcher emitted it so
// events from a replaced watcher are dropped instead of double-arming waits.
type IdleEventMsg struct {
	AccountID int64
	MailboxID int64
	Watcher   *imapClient.Watcher
	Stopped   bool
}

// idleWatcherEntry pairs a running watcher with the spec it was started from,
// so startIdleWatchers can tell "unchanged" apart from "needs restart".
type idleWatcherEntry struct {
	watcher     *imapClient.Watcher
	cfg         config.AccountConfig
	mailboxID   int64
	mailboxName string
}

// startIdleWatchers reconciles the running IMAP IDLE watchers against the
// current accounts so new mail syncs the moment the server announces it,
// instead of waiting for the next polling tick. Only accounts that opted into
// background refresh (sync_minutes > 0) get a watcher — sync_minutes = 0 means
// manual-only sync, and a persistent push connection would override that
// intent. Polling timers keep running regardless; IDLE is an accelerator, not
// a replacement.
//
// This MUST reconcile, not blindly restart: it runs on every AccountsLoadedMsg,
// and every sync completion reloads accounts. Restarting watchers each time
// made each fresh watcher emit its gap-repair event on connect, which triggered
// a sync, which reloaded accounts, which restarted the watchers — an endless
// one-second sync loop. A watcher whose account config and inbox are unchanged
// is left alone (its pending waitIdleCmd stays armed and valid).
func (m *Model) startIdleWatchers() tea.Cmd {
	desired := make(map[int64]idleWatcherEntry, len(m.accounts))
	for _, acc := range m.accounts {
		var acfg config.AccountConfig
		for _, a := range m.cfg.Accounts {
			if a.Name == acc.Name {
				acfg = a
				break
			}
		}
		if acfg.IMAPHost == "" || acfg.SyncMinutes <= 0 {
			continue
		}
		var inbox *db.Mailbox
		for i := range m.mailboxes {
			if m.mailboxes[i].AccountID == acc.ID && isInboxMailbox(m.mailboxes[i]) {
				inbox = &m.mailboxes[i]
				break
			}
		}
		if inbox == nil {
			continue
		}
		desired[acc.ID] = idleWatcherEntry{cfg: acfg, mailboxID: inbox.ID, mailboxName: inbox.Name}
	}

	// Close watchers that are gone or whose spec changed (edited credentials,
	// different inbox); keep matching ones untouched.
	for id, e := range m.idleWatchers {
		want, ok := desired[id]
		if ok && e.cfg == want.cfg && e.mailboxID == want.mailboxID && e.mailboxName == want.mailboxName {
			continue
		}
		e.watcher.Close()
		delete(m.idleWatchers, id)
	}

	var cmds []tea.Cmd
	for id, want := range desired {
		if _, running := m.idleWatchers[id]; running {
			continue
		}
		want.watcher = imapClient.NewWatcher(want.cfg, want.mailboxName, logIdle)
		m.idleWatchers[id] = want
		cmds = append(cmds, waitIdleCmd(want.watcher, id, want.mailboxID))
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// stopIdleWatchers closes every running watcher; safe to call repeatedly.
func (m *Model) stopIdleWatchers() {
	for id, e := range m.idleWatchers {
		e.watcher.Close()
		delete(m.idleWatchers, id)
	}
}

// waitIdleCmd blocks until the watcher signals a change (or stops) and turns
// it into a tea.Msg. The Update handler re-arms it after each event.
func waitIdleCmd(w *imapClient.Watcher, accountID, mailboxID int64) tea.Cmd {
	return func() tea.Msg {
		select {
		case <-w.Events():
			return IdleEventMsg{AccountID: accountID, MailboxID: mailboxID, Watcher: w}
		case <-w.Done():
			return IdleEventMsg{AccountID: accountID, MailboxID: mailboxID, Watcher: w, Stopped: true}
		}
	}
}

// logIdle appends watcher diagnostics to the fetch log (same file syncs log to).
func logIdle(format string, args ...any) {
	logPath, pathErr := config.LogPath()
	if pathErr != nil {
		return
	}
	f, openErr := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if openErr != nil {
		return
	}
	defer f.Close()
	log.New(f, "", log.LstdFlags).Printf(format, args...)
}
