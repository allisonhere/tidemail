package ui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/smtp"
)

// pendingSend is an outgoing message held for the send-delay undo window.
// Compose keeps the full overlay snapshot so undo restores the editor exactly
// as it was, mirroring the pendingDestructiveAction pattern.
type pendingSend struct {
	ID         uint64
	Account    config.AccountConfig
	Msg        smtp.OutgoingMessage
	DraftID    int64
	Compose    ComposeModel
	Committing bool
	Runtime    *sendRuntime // set once committing; lets exit flush wait for the in-flight send
}

type sendRuntime struct {
	done chan struct{}
	err  error
}

// handleSendQueued receives a message the compose overlay wants sent. With a
// zero delay it dispatches immediately; otherwise it closes compose, parks the
// message, and starts the undo grace timer.
func (m Model) handleSendQueued(msg SendQueuedMsg) (Model, tea.Cmd) {
	snapshot := m.compose
	snapshot.busy = false
	snapshot.statusMsg = ""
	draftID := snapshot.draftID
	m.compose = ComposeModel{}
	m.overlay = overlayNone

	delay := m.cfg.Display.SendDelaySeconds
	if delay <= 0 {
		m.setStatus("sending...", false)
		return m, sendMessageCmd(msg.Account, msg.Msg, draftID, 0)
	}

	m.nextSendID++
	id := m.nextSendID
	m.pendingSends = append(m.pendingSends, pendingSend{
		ID:      id,
		Account: msg.Account,
		Msg:     msg.Msg,
		DraftID: draftID,
		Compose: snapshot,
	})
	m.setStatus(fmt.Sprintf("sending in %ds · ctrl+z to undo", delay), false)
	return m, tea.Tick(time.Duration(delay)*time.Second, func(time.Time) tea.Msg {
		return CommitSendMsg{ID: id}
	})
}

// commitPendingSend dispatches a pending send whose grace period elapsed. An
// undone entry is simply gone from the slice, so the stale tick is a no-op.
func (m *Model) commitPendingSend(id uint64) tea.Cmd {
	for i := range m.pendingSends {
		p := &m.pendingSends[i]
		if p.ID != id || p.Committing {
			continue
		}
		p.Committing = true
		p.Runtime = &sendRuntime{done: make(chan struct{})}
		m.setStatus("sending...", false)
		return sendMessageWithRuntimeCmd(p.Account, p.Msg, p.DraftID, p.ID, p.Runtime)
	}
	return nil
}

// undoLatestPendingSend cancels the newest not-yet-committing send and
// restores its compose overlay. Returns false when there is nothing to undo
// (or an overlay is open that restoring compose would clobber).
func (m *Model) undoLatestPendingSend() bool {
	if m.overlay != overlayNone {
		return false
	}
	for i := len(m.pendingSends) - 1; i >= 0; i-- {
		if m.pendingSends[i].Committing {
			continue
		}
		p := m.pendingSends[i]
		m.pendingSends = append(m.pendingSends[:i], m.pendingSends[i+1:]...)
		m.compose = p.Compose
		m.overlay = overlayCompose
		m.setStatus("send canceled", false)
		return true
	}
	return false
}

func (m *Model) removePendingSend(id uint64) {
	if id == 0 {
		return
	}
	for i := range m.pendingSends {
		if m.pendingSends[i].ID == id {
			m.pendingSends = append(m.pendingSends[:i], m.pendingSends[i+1:]...)
			return
		}
	}
}

func sendMessageWithRuntimeCmd(acfg config.AccountConfig, msg smtp.OutgoingMessage, draftID int64, pendingID uint64, rt *sendRuntime) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := smtp.Send(ctx, acfg, msg)
		rt.err = err
		close(rt.done)
		return MessageSentMsg{Err: err, DraftID: draftID, PendingID: pendingID}
	}
}

// FlushPendingSends delivers queued sends before the session closes: it waits
// for in-flight commits and synchronously sends anything still in its grace
// window, so a quit never drops an email the user believes is sent.
func (m Model) FlushPendingSends() error {
	var firstErr error
	for _, p := range m.pendingSends {
		if p.Committing {
			if p.Runtime != nil {
				<-p.Runtime.done
				if p.Runtime.err != nil && firstErr == nil {
					firstErr = p.Runtime.err
				}
			}
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := smtp.Send(ctx, p.Account, p.Msg)
		cancel()
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (m Model) HasPendingSends() bool {
	return len(m.pendingSends) > 0
}
