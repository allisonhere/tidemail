package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	imapClient "github.com/allisonhere/tidemail/internal/imap"
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
	dueAt := msg.ScheduledAt
	if dueAt.IsZero() {
		dueAt = time.Now().Add(time.Duration(m.cfg.Display.SendDelaySeconds) * time.Second)
	}
	delay := time.Until(dueAt)
	id, err := m.db.EnqueueOutbox(db.OutboxItem{
		AccountConfigID: msg.Account.ID,
		AccountName:     msg.Account.Name, AccountUser: msg.Account.User, DraftID: draft.ID,
		Subject: msg.Msg.Subject, Recipients: joinRecipients(msg.Msg), MessageJSON: payload, DraftJSON: draftJSON,
		MaxAttempts: config.NormalizeSendMaxAttempts(m.cfg.Display.SendMaxAttempts), NextAttempt: dueAt.Unix(),
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

func newOutboxRuntime(database *db.DB, sessions *imapClient.SessionPool, id int64, account config.AccountConfig, cleanup tea.Cmd) *sendRuntime {
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
		// Stamp Date and Message-ID once, so the copy appended to Sent is
		// byte-identical to what goes out over SMTP.
		msg.EnsureIdentity(account.From)
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
		// Only once delivery is recorded: an APPEND is decoration, and must
		// never sit between delivery and the durable record, or a crash inside
		// it leaves a delivered message looking unsent and eligible for resend.
		if err == nil {
			result.SentCopyErr = appendToSentFolder(database, sessions, account, msg)
		}
		if err == nil && item.DraftID != 0 && cleanup != nil {
			result.CleanupErr = cleanup().(DraftDeletedMsg).Err
		}
		return result
	}}
}

// serverFilesSentMail reports whether the provider files submitted mail into
// Sent by itself, in which case appending our own copy would show the user two
// of everything. Gmail does this for anything submitted through its SMTP —
// including Workspace accounts on a custom address — so match the host as well
// as the provider name.
func serverFilesSentMail(acfg config.AccountConfig) bool {
	host := strings.ToLower(strings.TrimSpace(acfg.SMTPHost))
	if strings.HasSuffix(host, "smtp.gmail.com") || strings.HasSuffix(host, "smtp-relay.gmail.com") {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(acfg.Provider), "Gmail")
}

// appendToSentFolder puts a copy of a just-delivered message in the account's
// Sent folder. Any error is returned for reporting only — the mail is already
// delivered and recorded, so there is nothing to retry and nothing to fail.
func appendToSentFolder(database *db.DB, sessions *imapClient.SessionPool, acfg config.AccountConfig, msg smtp.OutgoingMessage) error {
	if database == nil || sessions == nil || strings.TrimSpace(acfg.IMAPHost) == "" {
		return nil
	}
	if serverFilesSentMail(acfg) {
		return nil
	}
	accountID, err := database.AccountIDByConfigID(acfg.ID)
	if err != nil {
		return err
	}
	// Report rather than guess: creating folders on someone's server as a side
	// effect of sending mail is too aggressive.
	sent, err := database.FindSentMailbox(accountID)
	if err != nil {
		return err
	}
	raw := smtp.BuildRaw(acfg, msg)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return sessions.Do(ctx, acfg, func(client *imapClient.Client) error {
		return client.AppendSent(ctx, sent.Name, raw, msg.Date)
	})
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
