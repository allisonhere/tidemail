package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/smtp"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func joinRecipients(msg smtp.OutgoingMessage) string {
	recipients := append([]string{}, msg.To...)
	recipients = append(recipients, msg.CC...)
	recipients = append(recipients, msg.BCC...)
	return strings.Join(recipients, ", ")
}

func (m *Model) refreshOutbox() {
	if m.db == nil {
		return
	}
	items, err := m.db.ListOutbox()
	if err != nil {
		m.outboxStatus = "load failed: " + err.Error()
		return
	}
	m.outboxItems = items
	m.outboxCursor = clamp(m.outboxCursor, 0, max(0, len(items)-1))
}

func (m *Model) openOutbox() {
	m.outboxStatus = ""
	m.outboxConfirmID = 0
	m.refreshOutbox()
	m.overlay = overlayOutbox
}

func (m Model) outboxAccount(item db.OutboxItem) (config.AccountConfig, bool) {
	for _, account := range m.cfg.Accounts {
		if account.Name == item.AccountName && account.User == item.AccountUser {
			return account, true
		}
	}
	return config.AccountConfig{}, false
}

func (m *Model) scheduleOutbox(id int64) tea.Cmd {
	for _, p := range m.pendingSends {
		if p.ID == uint64(id) {
			return nil
		}
	}
	item, err := m.db.GetOutbox(id)
	if err != nil {
		m.outboxStatus = err.Error()
		return nil
	}
	if item.State != db.OutboxQueued {
		return nil
	}
	account, ok := m.outboxAccount(item)
	if !ok {
		m.outboxStatus = "Sender account unavailable; add or restore it before retrying."
		if err := m.db.FailQueuedOutbox(id, m.outboxStatus); err != nil {
			m.outboxStatus = err.Error()
		}
		m.refreshOutbox()
		return nil
	}
	var msg smtp.OutgoingMessage
	var draft db.Draft
	if err = json.Unmarshal(item.MessageJSON, &msg); err == nil {
		err = json.Unmarshal(item.DraftJSON, &draft)
	}
	if err != nil {
		if saveErr := m.db.FailQueuedOutbox(id, "Cannot read saved message: "+err.Error()); saveErr != nil {
			m.outboxStatus = saveErr.Error()
		}
		m.refreshOutbox()
		return nil
	}
	m.pendingSends = append(m.pendingSends, pendingSend{
		ID: uint64(id), Account: account, Msg: msg, DraftID: item.DraftID, Attempts: item.Attempts,
		Compose: NewComposeFromDraft(draft, m.cfg.Accounts, m.addressBook),
		Runtime: newOutboxRuntime(m.db, id, account, m.deleteDraftCmd(item.DraftID)),
	})
	delay := time.Until(time.Unix(item.NextAttempt, 0))
	if delay <= 0 {
		return m.commitPendingSend(uint64(id))
	}
	return outboxTick(uint64(id), delay)
}

func (m *Model) resumeOutbox() tea.Cmd {
	if m.db == nil {
		return nil
	}
	if err := m.db.RecoverOutbox(); err != nil {
		m.setStatus("outbox recovery failed: "+err.Error(), true)
		return nil
	}
	m.refreshOutbox()
	var cmds []tea.Cmd
	for _, item := range m.outboxItems {
		if item.State == db.OutboxQueued {
			cmds = append(cmds, m.scheduleOutbox(item.ID))
		}
	}
	return tea.Batch(cmds...)
}

func (m Model) handleOutboxSent(msg MessageSentMsg) (tea.Model, tea.Cmd) {
	m.removePendingSend(msg.PendingID)
	m.refreshOutbox()
	if msg.Skipped {
		return m, nil
	}
	if msg.CleanupErr != nil {
		m.setStatus(fmt.Sprintf("message sent, but cleanup failed: %v", msg.CleanupErr), true)
		return m, m.clearStatusCmd()
	}
	if msg.Err != nil {
		cmd := m.scheduleOutbox(int64(msg.PendingID))
		if item, err := m.db.GetOutbox(int64(msg.PendingID)); err == nil && item.State == db.OutboxQueued {
			m.setStatus(fmt.Sprintf("send failed · retry %d/%d in 1 minute · O opens Outbox", item.Attempts+1, item.MaxAttempts), true)
		} else {
			m.setStatus("send failed · saved in Outbox (O): "+msg.Err.Error(), true)
		}
		return m, tea.Batch(cmd, m.clearStatusCmd())
	}
	m.setStatus("message sent", false)
	return m, m.clearStatusCmd()
}

func (m Model) outboxSending(id int64) bool {
	for _, p := range m.pendingSends {
		if p.ID == uint64(id) && p.Committing {
			return true
		}
	}
	return false
}

func (m Model) handleOutboxKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.outboxConfirmID != 0 {
		id := m.outboxConfirmID
		m.outboxConfirmID = 0
		if key.String() == "y" {
			return m.retryOutbox(id)
		}
		m.outboxStatus = "Retry canceled."
		return m, nil
	}
	switch {
	case keyMatches(key, m.keys.Cancel), key.String() == "q":
		m.overlay = overlayNone
		return m, nil
	case keyMatches(key, m.keys.Up):
		m.outboxCursor = max(0, m.outboxCursor-1)
		m.outboxStatus = ""
		return m, nil
	case keyMatches(key, m.keys.Down):
		m.outboxCursor = min(max(0, len(m.outboxItems)-1), m.outboxCursor+1)
		m.outboxStatus = ""
		return m, nil
	}
	if len(m.outboxItems) == 0 {
		return m, nil
	}
	item := m.outboxItems[m.outboxCursor]
	if item.State == db.OutboxSending || m.outboxSending(item.ID) {
		m.outboxStatus = "Wait for this delivery attempt to finish."
		return m, nil
	}
	switch key.String() {
	case "r":
		if item.State == db.OutboxUncertain {
			m.outboxConfirmID = item.ID
			m.outboxStatus = "Delivery may have succeeded. Retry anyway? y / n"
			return m, nil
		}
		if item.State != db.OutboxFailed {
			m.outboxStatus = "Only failed messages need retrying."
			return m, nil
		}
		return m.retryOutbox(item.ID)
	case "e", "enter":
		if item.State == db.OutboxSent {
			m.outboxStatus = "This message has already been sent."
			return m, nil
		}
		if _, ok := m.outboxAccount(item); !ok {
			m.outboxStatus = "Restore the sender account before editing."
			return m, nil
		}
		draft, err := m.db.EditOutbox(item.ID)
		if err != nil {
			m.outboxStatus = err.Error()
			return m, nil
		}
		m.removePendingSend(uint64(item.ID))
		m.refreshOutbox()
		m.compose = NewComposeFromDraft(draft, m.cfg.Accounts, m.addressBook)
		m.overlay = overlayCompose
	case "d":
		if item.State != db.OutboxSent {
			m.outboxStatus = "Use e to cancel delivery and move this message to Drafts."
			return m, nil
		}
		if err := m.db.DeleteOutbox(item.ID); err != nil {
			m.outboxStatus = err.Error()
			return m, nil
		}
		m.refreshOutbox()
	}
	return m, nil
}

func (m Model) retryOutbox(id int64) (tea.Model, tea.Cmd) {
	item, err := m.db.GetOutbox(id)
	if err != nil {
		m.outboxStatus = err.Error()
		return m, nil
	}
	if _, ok := m.outboxAccount(item); !ok {
		m.outboxStatus = "Restore the sender account before retrying."
		return m, nil
	}
	if err := m.db.RetryOutbox(id, config.NormalizeSendMaxAttempts(m.cfg.Display.SendMaxAttempts)); err != nil {
		m.outboxStatus = err.Error()
		return m, nil
	}
	m.outboxStatus = "Retrying with the current attempt limit."
	m.refreshOutbox()
	cmd := m.scheduleOutbox(id)
	return m, cmd
}

func (m Model) renderOutbox(width, height int, chrome managerChrome) string {
	width = max(1, width)
	height = max(1, height)
	style := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.text).Width(width)
	line := func(text string) string {
		return style.Render(clampView(unescapeDisplayText(text), width, 1, chrome.baseBg))
	}
	rows := []string{}
	visible := max(1, height-7)
	start := max(0, m.outboxCursor-visible+1)
	for i := start; i < min(len(m.outboxItems), start+visible); i++ {
		item := m.outboxItems[i]
		subject := item.Subject
		if subject == "" {
			subject = "(no subject)"
		}
		text := fmt.Sprintf("%-9s %d/%d  %s", item.State, item.Attempts, item.MaxAttempts, subject)
		rowStyle := style
		prefix := "  "
		if i == m.outboxCursor {
			prefix = "> "
			rowStyle = rowStyle.Background(chrome.highlight).Foreground(chrome.highlightFg)
		}
		rows = append(rows, rowStyle.Render(clampView(prefix+unescapeDisplayText(text), width, 1, chrome.baseBg)))
	}
	if len(m.outboxItems) == 0 {
		rows = append(rows, line("No queued or sent messages yet."))
	}
	body := padBlock(rows, visible, width, chrome.baseBg)
	details := []string{}
	if len(m.outboxItems) > 0 {
		item := m.outboxItems[m.outboxCursor]
		details = append(details, line("From: "+item.AccountUser), line("To: "+item.Recipients))
		detail := item.LastError
		if item.State == db.OutboxQueued {
			detail = "Next attempt: " + time.Unix(item.NextAttempt, 0).Local().Format("Jan 2 15:04:05")
			if item.LastError != "" {
				detail += " · " + item.LastError
			}
		}
		if item.State == db.OutboxUncertain {
			detail = "Check Sent mail before retrying; delivery may have succeeded."
		}
		details = append(details, line(detail))
	}
	footer := line("r retry · e/enter edit · d clear sent · esc close")
	return clampView(strings.Join([]string{line("State     Tries  Subject"), body, padBlock(details, 3, width, chrome.baseBg), line(m.outboxStatus), footer}, "\n"), width, height, chrome.baseBg)
}
