package ui

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	imapClient "github.com/allisonhere/tidemail/internal/imap"
	"github.com/allisonhere/tidemail/internal/mailapp"
	"github.com/allisonhere/tidemail/internal/smtp"
	tea "github.com/charmbracelet/bubbletea"
)

type pendingSend struct {
	ID         uint64
	Account    config.AccountConfig
	Msg        smtp.OutgoingMessage
	DraftID    int64
	Compose    ComposeModel
	Attempts   int
	DueAt      int64
	Scheduled  bool
	Committing bool
	Runtime    *sendRuntime
}

type sendRuntime struct {
	once    sync.Once
	command tea.Cmd
	result  MessageSentMsg
}

// Persist first: a failed queue write leaves compose open with all content intact.
func (m Model) handleSendQueued(msg SendQueuedMsg) (Model, tea.Cmd) {
	snapshot := m.compose
	snapshot.busy = false
	snapshot.statusMsg = ""
	draft := snapshot.toDraftRecord()
	if m.db == nil {
		m.compose = snapshot
		m.compose.statusMsg = "cannot queue mail: database unavailable"
		m.compose.isErr = true
		return m, nil
	}
	dueAt := msg.ScheduledAt
	if dueAt.IsZero() {
		dueAt = time.Now().Add(time.Duration(m.cfg.Display.SendDelaySeconds) * time.Second)
	}
	delay := time.Until(dueAt)
	id, err := mailapp.Enqueue(m.db, m.cfg, msg.Account, msg.Msg, draft, dueAt)
	if err != nil {
		m.compose = snapshot
		m.compose.statusMsg = "cannot queue mail: " + err.Error()
		m.compose.isErr = true
		return m, nil
	}
	for i, saved := range m.drafts {
		if saved.ID == draft.ID {
			m.drafts = append(m.drafts[:i], m.drafts[i+1:]...)
			break
		}
	}
	m.compose = ComposeModel{}
	m.overlay = overlayNone
	m.pendingSends = append(m.pendingSends, pendingSend{
		ID: uint64(id), Account: msg.Account, Msg: msg.Msg, DraftID: draft.ID, Compose: snapshot,
		DueAt: dueAt.Unix(), Scheduled: !msg.ScheduledAt.IsZero(), Runtime: newOutboxRuntime(m.db, m.sessions, id, msg.Account, m.deleteDraftCmd(draft.ID)),
	})
	m.refreshOutbox()
	if delay <= 0 {
		cmd := m.commitPendingSend(uint64(id))
		return m, cmd
	}
	if !msg.ScheduledAt.IsZero() {
		m.setStatus("scheduled for "+dueAt.Local().Format("Jan 2 3:04 PM")+" · O opens Outbox", false)
		return m, outboxTick(uint64(id), delay)
	}
	m.setStatus(fmt.Sprintf("sending in %s · ctrl+z to undo · O opens Outbox", delay.Round(time.Second)), false)
	return m, outboxTick(uint64(id), delay)
}

func outboxTick(id uint64, delay time.Duration) tea.Cmd {
	return tea.Tick(max(0, delay), func(time.Time) tea.Msg { return CommitSendMsg{ID: id} })
}

func (m *Model) commitPendingSend(id uint64) tea.Cmd {
	for i := range m.pendingSends {
		p := &m.pendingSends[i]
		if p.ID != id || p.Committing {
			continue
		}
		p.Committing = true
		m.setStatus("sending...", false)
		// Rendering uses this immediate state until the durable claim is made.
		for j := range m.outboxItems {
			if m.outboxItems[j].ID == int64(id) {
				m.outboxItems[j].State = db.OutboxSending
				m.outboxItems[j].Attempts = p.Attempts + 1
			}
		}
		return p.Runtime.run
	}
	return nil
}

func (m *Model) undoLatestPendingSend() bool {
	if m.overlay != overlayNone {
		return false
	}
	for i := len(m.pendingSends) - 1; i >= 0; i-- {
		// Only a send still inside its undo delay is Ctrl+Z's to take back. A
		// scheduled send or a pending retry can be hours old — cancelling it here
		// would steal the undo meant for a later action. The Outbox edits those.
		if p := m.pendingSends[i]; p.Committing || p.Scheduled || p.Attempts > 0 {
			continue
		}
		p := m.pendingSends[i]
		draft, err := m.db.EditOutbox(int64(p.ID))
		if err != nil {
			m.setStatus("cancel send failed: "+err.Error(), true)
			return false
		}
		m.pendingSends = append(m.pendingSends[:i], m.pendingSends[i+1:]...)
		m.compose = p.Compose
		m.compose.draftID = draft.ID
		m.overlay = overlayCompose
		m.refreshOutbox()
		m.setStatus("send canceled", false)
		return true
	}
	return false
}

func (m *Model) removePendingSend(id uint64) {
	for i := range m.pendingSends {
		if m.pendingSends[i].ID == id {
			m.pendingSends = append(m.pendingSends[:i], m.pendingSends[i+1:]...)
			return
		}
	}
}

func (rt *sendRuntime) run() tea.Msg {
	rt.once.Do(func() { rt.result = rt.command().(MessageSentMsg) })
	return rt.result
}

func outboxMessage(item db.OutboxItem, account config.AccountConfig) (smtp.OutgoingMessage, error) {
	return mailapp.OutboxMessage(item, account)
}
func newOutboxRuntime(database *db.DB, sessions *imapClient.SessionPool, id int64, account config.AccountConfig, cleanup tea.Cmd) *sendRuntime {
	return &sendRuntime{command: func() tea.Msg {
		var cleanupFn func() error
		if cleanup != nil {
			cleanupFn = func() error { return cleanup().(DraftDeletedMsg).Err }
		}
		r := mailapp.Deliver(context.Background(), database, sessions, id, account, true, cleanupFn)
		return MessageSentMsg{PendingID: uint64(id), Skipped: r.Skipped, Err: r.Err, CleanupErr: r.CleanupErr, SentCopyErr: r.SentCopyErr}
	}}
}
func serverFilesSentMail(account config.AccountConfig) bool {
	return mailapp.ServerFilesSentMail(account)
}
func appendToSentFolder(database *db.DB, sessions *imapClient.SessionPool, account config.AccountConfig, msg smtp.OutgoingMessage) error {
	return mailapp.AppendToSentFolder(database, sessions, account, msg)
}

// Complete first sends and in-flight deliveries on quit. Scheduled retries are
// already durable and wait until the next session instead of delaying shutdown.
func (m Model) FlushPendingSends() error {
	var firstErr error
	for _, p := range m.pendingSends {
		if (p.Scheduled && !p.Committing && p.DueAt > time.Now().Unix()) || (p.Attempts > 0 && !p.Committing) {
			continue
		}
		result := p.Runtime.run().(MessageSentMsg)
		if result.Err != nil && firstErr == nil {
			firstErr = result.Err
		}
		if result.CleanupErr != nil && firstErr == nil {
			firstErr = fmt.Errorf("message sent, but draft cleanup failed: %w", result.CleanupErr)
		}
	}
	return firstErr
}

func (m Model) HasPendingSends() bool { return len(m.pendingSends) > 0 }
