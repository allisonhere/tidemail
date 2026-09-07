package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/smtp"
	tea "github.com/charmbracelet/bubbletea"
)

const outboxRetryDelay = time.Minute

type pendingSend struct {
	ID         uint64
	Account    config.AccountConfig
	Msg        smtp.OutgoingMessage
	DraftID    int64
	Compose    ComposeModel
	Attempts   int
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
	if msg.Msg.From == "" {
		msg.Msg.From = msg.Account.From
		if msg.Msg.From == "" {
			msg.Msg.From = msg.Account.User
		}
	}
	payload, err := json.Marshal(msg.Msg)
	if err != nil {
		m.compose = snapshot
		m.setStatus(err.Error(), true)
		return m, nil
	}
	draftJSON, err := json.Marshal(draft)
	if err != nil {
		m.compose = snapshot
		m.setStatus(err.Error(), true)
		return m, nil
	}
	delay := m.cfg.Display.SendDelaySeconds
	id, err := m.db.EnqueueOutbox(db.OutboxItem{
		AccountName: msg.Account.Name, AccountUser: msg.Account.User, DraftID: draft.ID,
		Subject: msg.Msg.Subject, Recipients: joinRecipients(msg.Msg), MessageJSON: payload, DraftJSON: draftJSON,
		MaxAttempts: config.NormalizeSendMaxAttempts(m.cfg.Display.SendMaxAttempts), NextAttempt: time.Now().Add(time.Duration(delay) * time.Second).Unix(),
	})
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
		Runtime: newOutboxRuntime(m.db, id, msg.Account, m.deleteDraftCmd(draft.ID)),
	})
	m.refreshOutbox()
	if delay <= 0 {
		cmd := m.commitPendingSend(uint64(id))
		return m, cmd
	}
	m.setStatus(fmt.Sprintf("sending in %ds · ctrl+z to undo · O opens Outbox", delay), false)
	return m, outboxTick(uint64(id), time.Duration(delay)*time.Second)
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
		if m.pendingSends[i].Committing {
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

func newOutboxRuntime(database *db.DB, id int64, account config.AccountConfig, cleanup tea.Cmd) *sendRuntime {
	return &sendRuntime{command: func() tea.Msg {
		result := MessageSentMsg{PendingID: uint64(id)}
		item, err := database.GetOutbox(id)
		if err != nil {
			result.Err = err
			return result
		}
		var msg smtp.OutgoingMessage
		if err = json.Unmarshal(item.MessageJSON, &msg); err != nil {
			result.Err = err
			return result
		}
		claimed, err := database.ClaimOutbox(id)
		if err != nil {
			result.Err = err
			return result
		}
		if !claimed {
			result.Skipped = true
			return result
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err = smtp.Send(ctx, account, msg)
		cancel()
		state, next, message := db.OutboxSent, int64(0), ""
		if err != nil {
			result.Err = err
			message = config.RedactSecrets(err.Error(), config.Config{Accounts: []config.AccountConfig{account}})
			state = db.OutboxFailed
			if errors.Is(err, smtp.ErrDeliveryUncertain) {
				state = db.OutboxUncertain
			} else if smtp.CanRetry(err) && item.Attempts+1 < item.MaxAttempts {
				state = db.OutboxQueued
				next = time.Now().Add(outboxRetryDelay).Unix()
			}
		}
		// Record acceptance before cleanup. Restart must never resend a known success.
		if saveErr := database.FinishOutbox(id, state, message, next); saveErr != nil {
			if err == nil {
				result.CleanupErr = fmt.Errorf("record sent status: %w", saveErr)
			} else {
				result.Err = fmt.Errorf("%v; save delivery status: %w", err, saveErr)
			}
			return result
		}
		if err == nil && item.DraftID != 0 && cleanup != nil {
			result.CleanupErr = cleanup().(DraftDeletedMsg).Err
		}
		return result
	}}
}

// Complete first sends and in-flight deliveries on quit. Scheduled retries are
// already durable and wait until the next session instead of delaying shutdown.
func (m Model) FlushPendingSends() error {
	var firstErr error
	for _, p := range m.pendingSends {
		if p.Attempts > 0 && !p.Committing {
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
