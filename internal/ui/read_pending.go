package ui

import (
	"context"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	imapClient "github.com/allisonhere/tidemail/internal/imap"
	tea "github.com/charmbracelet/bubbletea"
)

type readStoreFunc func(context.Context, string, []uint32, bool) error
type readSessionFunc func(context.Context, config.AccountConfig, func(readStoreFunc) error) error

type pendingRead struct {
	Token     uint64
	Read      bool
	MailboxID int64
}
type readRemoteCompletedMsg struct {
	Token   uint64
	IDs     []int64
	Success []MessageReadBatchEntry
	Err     error
}

// acceptReadChanges runs on the UI goroutine. Reserving IDs here closes the
// duplicate-request window before any asynchronous command starts.
// A nil target retains the selected-message per-message toggle semantics.
func (m *Model) acceptReadChanges(messages []db.Message, target *bool, advance bool) tea.Cmd {
	if m.pendingReads == nil {
		m.pendingReads = make(map[int64]pendingRead)
	}
	type batch struct {
		messages []db.Message
		entries  []MessageReadBatchEntry
	}
	batches := make(map[int64]*batch)
	var order []int64
	var entries []MessageReadBatchEntry
	seen := make(map[int64]bool)
	for _, msg := range messages {
		if m.messageActionPending(msg.ID) || m.pendingBulkReads[msg.MailboxID] != 0 || seen[msg.ID] {
			continue
		}
		seen[msg.ID] = true
		read := !msg.Read
		if target != nil {
			read = *target
		}
		entry := MessageReadBatchEntry{MessageID: msg.ID, MailboxID: msg.MailboxID, WasRead: msg.Read, Read: read}
		entries = append(entries, entry)
		if batches[msg.MailboxID] == nil {
			batches[msg.MailboxID] = &batch{}
			order = append(order, msg.MailboxID)
		}
		b := batches[msg.MailboxID]
		b.messages = append(b.messages, msg)
		b.entries = append(b.entries, entry)
	}
	if len(entries) == 0 {
		return nil
	}
	if m.db == nil {
		m.setStatus("mark read failed: database unavailable", true)
		return m.clearStatusCmd()
	}
	var cmds []tea.Cmd
	for _, mailboxID := range order {
		b := batches[mailboxID]
		mailbox := m.mailboxByID(mailboxID)
		account, cfgErr := m.accountCfgForMailbox(mailboxID)
		// Copy model-owned mailbox data before handing it to a worker.
		var mailboxName string
		if mailbox != nil {
			mailboxName = mailbox.Name
		}
		m.nextReadOperation++
		token := m.nextReadOperation
		ids := messageIDs(b.messages)
		for _, entry := range b.entries {
			m.pendingReads[entry.MessageID] = pendingRead{Token: token, Read: entry.Read, MailboxID: entry.MailboxID}
		}
		database, sessions := m.db, m.sessions
		withSession := m.readSession
		if withSession == nil {
			withSession = func(ctx context.Context, account config.AccountConfig, fn func(readStoreFunc) error) error {
				return sessions.Do(ctx, account, func(client *imapClient.Client) error { return fn(client.MarkSeenUIDs) })
			}
		}
		cmds = append(cmds, func() tea.Msg {
			result := readRemoteCompletedMsg{Token: token, IDs: ids, Err: cfgErr}
			if cfgErr != nil {
				return result
			}
			var reads, unreads []db.Message
			for i, msg := range b.messages {
				if b.entries[i].Read {
					reads = append(reads, msg)
				} else {
					unreads = append(unreads, msg)
				}
			}
			persist := func(messages []db.Message, read bool) error {
				var r, u []int64
				if read {
					r = messageIDs(messages)
				} else {
					u = messageIDs(messages)
				}
				if err := database.MarkReadBatch(r, u); err != nil {
					return err
				}
				for _, msg := range messages {
					result.Success = append(result.Success, MessageReadBatchEntry{MessageID: msg.ID, MailboxID: msg.MailboxID, WasRead: msg.Read, Read: read})
				}
				return nil
			}
			if mailboxName == "" || account.IMAPHost == "" || len(messageUIDs(b.messages)) == 0 {
				result.Err = persist(reads, true)
				if err := persist(unreads, false); result.Err == nil {
					result.Err = err
				}
				return result
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			ctx = uidWriteContext(ctx, database, mailboxID, mailboxName)
			result.Err = withSession(ctx, account, func(store readStoreFunc) error {
				var firstErr error
				for _, group := range []struct {
					messages []db.Message
					read     bool
				}{{reads, true}, {unreads, false}} {
					if len(group.messages) == 0 {
						continue
					}
					err := store(ctx, mailboxName, messageUIDs(group.messages), group.read)
					if err == nil {
						// Publish confirmed server state before releasing the session,
						// so an earlier sync snapshot cannot overwrite this write.
						err = persist(group.messages, group.read)
					}
					if firstErr == nil {
						firstErr = err
					}
				}
				return firstErr
			})
			return result
		})
	}
	if target != nil && len(entries) == 1 && advance {
		m.advanceReadCursor(entries[0].MessageID)
	}
	cmds = append(cmds, m.ensureReadAnimation())
	return tea.Batch(cmds...)
}

func (m *Model) rowReadPending(msg db.Message, thread messageThread) bool {
	if _, ok := m.pendingReads[msg.ID]; ok {
		return true
	}
	for _, member := range thread.Messages {
		if _, ok := m.pendingReads[member.ID]; ok {
			return true
		}
	}
	return false
}

func (m *Model) pendingReadIndicator() string {
	if m.styles.PlainUI || !m.iconsEnabled() {
		return "~"
	}
	return readAnimationFrames[m.readAnimationFrame]
}

func (m *Model) advanceReadCursor(messageID int64) {
	rowCount := m.activeMessageRowCount()
	if idx := m.indexOfFilteredMessage(messageID); idx >= 0 && idx == m.messageCursor && idx < rowCount-1 {
		m.messageCursor = idx + 1
		visible := max(1, m.articleRowsVisible())
		if m.messageCursor >= m.listOffset+visible {
			m.listOffset = m.messageCursor - visible + 1
		}
	} else {
		m.messageCursor = clamp(m.messageCursor, 0, max(0, rowCount-1))
		m.listOffset = clamp(m.listOffset, 0, max(0, rowCount-1))
	}
	m.setViewportForCurrentRow()
}

// Read feedback has its own low-frequency clock. Reuse the existing Braille
// assets without slowing any unrelated spinner or waking at its 10 FPS rate.
func (m *Model) ensureReadAnimation() tea.Cmd {
	if (len(m.pendingReads) == 0 && !m.deleteFeedbackActive()) || m.readAnimationRunning {
		return nil
	}
	m.readAnimationRunning = true
	m.readAnimationGeneration++
	return readAnimationTick(m.readAnimationGeneration)
}

// Reload through the normal view-specific SQL ordering and ID-based selection.
func (m *Model) reloadReadMessagesCmd() tea.Cmd {
	if m.searchActive() {
		return m.searchAllMessagesCmd(m.searchQuery)
	}
	return m.visibleMessagesCmd()
}

func (m *Model) finishReadChanges(msg readRemoteCompletedMsg) tea.Cmd {
	matched := make(map[int64]bool, len(msg.IDs))
	for _, id := range msg.IDs {
		if pending, ok := m.pendingReads[id]; ok && pending.Token == msg.Token {
			delete(m.pendingReads, id)
			matched[id] = true
		}
	}
	if (len(m.pendingReads) == 0 && !m.deleteFeedbackActive()) && m.readAnimationRunning {
		m.readAnimationRunning = false
		m.readAnimationGeneration++
	}
	if len(matched) == 0 {
		return nil
	}
	mailboxes := make(map[int64]bool)
	currentID := int64(0)
	if current := m.currentRowMessage(); current != nil {
		currentID = current.ID
	}
	for _, entry := range msg.Success {
		if matched[entry.MessageID] {
			mailboxes[entry.MailboxID] = true
			// Unlock against the confirmed state, even if the SQL reload has
			// not arrived when the user immediately toggles this message again.
			for i := range m.messages {
				if m.messages[i].ID == entry.MessageID {
					m.messages[i].Read = entry.Read
					break
				}
			}
		}
	}
	var cmds []tea.Cmd
	if len(mailboxes) > 0 {
		m.readVersion++
		m.applyFilter()
		if idx := m.indexOfFilteredMessage(currentID); idx >= 0 {
			m.messageCursor = idx
		} else {
			m.messageCursor = clamp(m.messageCursor, 0, max(0, m.activeMessageRowCount()-1))
		}
		m.listOffset = clamp(m.listOffset, 0, max(0, m.activeMessageRowCount()-1))
		if m.activeMessageRowCount() == 0 {
			m.clearViewportMessage()
		} else {
			m.setViewportForCurrentRow()
		}
		// Recount instead of applying captured deltas: another completed operation
		// or a sync may already have changed the cached state before this event.
		for id := range mailboxes {
			if err := m.recountMailboxUnread(id); err != nil && msg.Err == nil {
				msg.Err = err
			}
		}
		cmds = append(cmds, m.reloadReadMessagesCmd())
	}
	if msg.Err != nil {
		m.setStatus("mark read failed: "+msg.Err.Error(), true)
		cmds = append(cmds, m.clearStatusCmd())
	}
	return tea.Batch(cmds...)
}

// recountMailboxUnread uses confirmed cache state instead of completion-order
// deltas. Suppressed remote deletions may still have a failed local cache row.
func (m *Model) recountMailboxUnread(id int64) error {
	count, err := m.db.CountUnread(id)
	if err != nil {
		return err
	}
	for messageID, ref := range m.confirmedDeleteCache {
		if ref.MailboxID != id {
			continue
		}
		msg, err := m.db.GetMessage(messageID)
		if err == nil && msg.MailboxID == id && !msg.Read {
			count--
		}
	}
	count = max(0, count)
	if err := m.db.SetMailboxUnreadCount(id, count); err != nil {
		return err
	}
	for i := range m.mailboxes {
		if m.mailboxes[i].ID == id {
			m.mailboxes[i].UnreadCount = count
			break
		}
	}
	return nil
}
