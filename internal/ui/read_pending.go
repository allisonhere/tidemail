package ui

import (
	"context"
	"fmt"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	imapClient "github.com/allisonhere/tidemail/internal/imap"
	tea "github.com/charmbracelet/bubbletea"
)

type readStoreFunc func(context.Context, string, []uint32, bool) error
type readSessionFunc func(context.Context, config.AccountConfig, func(readStoreFunc) error) error

type pendingRead struct {
	Token uint64
	Read  bool
}
type readRemoteCompletedMsg struct {
	Token uint64
	IDs   []int64
	Err   error
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
	var readIDs, unreadIDs []int64
	seen := make(map[int64]bool)
	for _, msg := range messages {
		if _, pending := m.pendingReads[msg.ID]; pending || seen[msg.ID] {
			continue
		}
		seen[msg.ID] = true
		read := !msg.Read
		if target != nil {
			read = *target
		}
		entry := MessageReadBatchEntry{MessageID: msg.ID, MailboxID: msg.MailboxID, WasRead: msg.Read, Read: read}
		entries = append(entries, entry)
		if read {
			readIDs = append(readIDs, msg.ID)
		} else {
			unreadIDs = append(unreadIDs, msg.ID)
		}
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
	// Persist before publishing success or dispatching IMAP. These small writes
	// intentionally preserve the existing database error semantics.
	if err := m.db.MarkReadBatch(readIDs, unreadIDs); err != nil {
		m.setStatus("mark read failed: "+err.Error(), true)
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
			m.pendingReads[entry.MessageID] = pendingRead{Token: token, Read: entry.Read}
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
			if mailboxName == "" || account.IMAPHost == "" || len(messageUIDs(b.messages)) == 0 {
				return result
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
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
						// Repair a stale sync write while still holding the account session.
						// This is cache repair only: completion never repeats UI deltas.
						var r, u []int64
						if group.read {
							r = messageIDs(group.messages)
						} else {
							u = messageIDs(group.messages)
						}
						err = database.MarkReadBatch(r, u)
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
	if target != nil && len(entries) == 1 {
		e := entries[0]
		m.applyMessageReadUpdatedMsg(MessageReadUpdatedMsg{MessageID: e.MessageID, MailboxID: e.MailboxID, WasRead: e.WasRead, Read: e.Read, Advance: advance})
	} else {
		m.applyMessageReadBatchUpdatedMsg(MessageReadBatchUpdatedMsg{Entries: entries})
	}
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
	return "◌"
}

func (m *Model) applyMessageReadBatchUpdatedMsg(msg MessageReadBatchUpdatedMsg) tea.Cmd {
	readByID := make(map[int64]MessageReadBatchEntry, len(msg.Entries))
	unreadDeltas := make(map[int64]int64)
	for _, entry := range msg.Entries {
		readByID[entry.MessageID] = entry
		if entry.MailboxID != 0 && entry.WasRead != entry.Read {
			if entry.Read {
				unreadDeltas[entry.MailboxID]--
			} else {
				unreadDeltas[entry.MailboxID]++
			}
		}
	}
	for i := range m.messages {
		if entry, ok := readByID[m.messages[i].ID]; ok {
			m.messages[i].Read = entry.Read
		}
	}
	for mailboxID, delta := range unreadDeltas {
		m.adjustMailboxUnreadCount(mailboxID, delta)
	}
	if len(msg.Entries) > 0 {
		m.applyFilter()
		if len(m.filteredMessages) == 0 {
			m.messageCursor = 0
			m.listOffset = 0
			m.clearViewportMessage()
		} else {
			rowCount := m.activeMessageRowCount()
			m.messageCursor = clamp(m.messageCursor, 0, max(0, rowCount-1))
			m.listOffset = clamp(m.listOffset, 0, max(0, rowCount-1))
			m.setViewportForCurrentRow()
		}
	}
	if msg.Err != nil {
		m.setStatus(fmt.Sprintf("mark read failed: %v", msg.Err), true)
		return m.clearStatusCmd()
	}
	return nil

}

func (m *Model) applyMessageReadUpdatedMsg(msg MessageReadUpdatedMsg) tea.Cmd {
	if msg.Err != nil {
		m.setStatus(fmt.Sprintf("mark read failed: %v", msg.Err), true)
		return m.clearStatusCmd()
	}
	for i := range m.messages {
		if m.messages[i].ID == msg.MessageID {
			m.messages[i].Read = msg.Read
			break
		}
	}
	if msg.MailboxID != 0 && msg.WasRead != msg.Read {
		delta := int64(1)
		if msg.Read {
			delta = -1
		}
		m.adjustMailboxUnreadCount(msg.MailboxID, delta)
	}
	m.applyFilter()
	if len(m.filteredMessages) == 0 {
		m.messageCursor = 0
		m.listOffset = 0
		m.clearViewportMessage()
		return nil
	}
	rowCount := m.activeMessageRowCount()
	if idx := m.indexOfFilteredMessage(msg.MessageID); msg.Advance && idx >= 0 && idx == m.messageCursor && idx < rowCount-1 {
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
	return nil

}
