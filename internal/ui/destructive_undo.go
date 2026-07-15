package ui

import (
	"context"
	"fmt"
	"time"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	imapClient "github.com/allisonhere/tide/internal/imap"
	tea "github.com/charmbracelet/bubbletea"
)

const destructiveUndoWindow = 6 * time.Second

type destructiveActionKind string

const (
	destructiveArchive destructiveActionKind = "archive"
	destructiveMove    destructiveActionKind = "move"
	destructiveDelete  destructiveActionKind = "delete"
)

type pendingDestructiveEntry struct {
	Message db.Message
	Source  db.Mailbox
	Target  db.Mailbox
	Account config.AccountConfig
}

type pendingDestructiveAction struct {
	ID         uint64
	Kind       destructiveActionKind
	Entries    []pendingDestructiveEntry
	Committing bool
	Runtime    *destructiveActionRuntime
}

type destructiveActionRuntime struct {
	done chan struct{}
	err  error
}

type CommitDestructiveActionMsg struct{ ID uint64 }

type DestructiveActionResultMsg struct {
	ID        uint64
	Kind      destructiveActionKind
	Succeeded []MessageRef
	Failed    []db.Message
	Err       error
}

func (m *Model) scheduleArchive(msgs []db.Message) tea.Cmd {
	entries := make([]pendingDestructiveEntry, 0, len(msgs))
	for _, msg := range msgs {
		source := m.mailboxByID(msg.MailboxID)
		if source == nil {
			m.setStatus("archive failed: mailbox not found", true)
			continue
		}
		target, err := m.db.FindArchiveMailbox(source.AccountID)
		if err != nil {
			m.setStatus("archive failed: "+err.Error(), true)
			continue
		}
		entries = append(entries, pendingDestructiveEntry{Message: msg, Source: *source, Target: target, Account: m.accountCfgForMailbox(msg.MailboxID)})
	}
	return m.scheduleDestructive(destructiveArchive, entries)
}

func (m *Model) scheduleMove(msgs []db.Message, target db.Mailbox) tea.Cmd {
	entries := make([]pendingDestructiveEntry, 0, len(msgs))
	for _, msg := range msgs {
		source := m.mailboxByID(msg.MailboxID)
		if source == nil || source.AccountID != target.AccountID {
			m.setStatus("move failed: source and target must be in one account", true)
			continue
		}
		entries = append(entries, pendingDestructiveEntry{Message: msg, Source: *source, Target: target, Account: m.accountCfgForMailbox(msg.MailboxID)})
	}
	return m.scheduleDestructive(destructiveMove, entries)
}

func (m *Model) scheduleDelete(msgs []db.Message) tea.Cmd {
	entries := make([]pendingDestructiveEntry, 0, len(msgs))
	for _, msg := range msgs {
		source := m.mailboxByID(msg.MailboxID)
		if source == nil {
			m.setStatus("delete failed: mailbox not found", true)
			continue
		}
		entry := pendingDestructiveEntry{Message: msg, Source: *source, Account: m.accountCfgForMailbox(msg.MailboxID)}
		if trash, err := m.db.FindTrashMailbox(source.AccountID); err == nil && trash.ID != source.ID && trash.Name != source.Name {
			entry.Target = trash
		}
		entries = append(entries, entry)
	}
	return m.scheduleDestructive(destructiveDelete, entries)
}

func (m *Model) scheduleDestructive(kind destructiveActionKind, entries []pendingDestructiveEntry) tea.Cmd {
	if len(entries) == 0 {
		return m.clearStatusCmd()
	}
	m.nextDestructiveActionID++
	id := m.nextDestructiveActionID
	m.pendingDestructiveActions = append(m.pendingDestructiveActions, pendingDestructiveAction{ID: id, Kind: kind, Entries: entries})
	m.clearSelection()
	m.applyFilter()
	m.clampMessageViewAfterDestructiveChange()
	return tea.Tick(destructiveUndoWindow, func(time.Time) tea.Msg { return CommitDestructiveActionMsg{ID: id} })
}

func (m *Model) undoLatestDestructive() tea.Cmd {
	for i := len(m.pendingDestructiveActions) - 1; i >= 0; i-- {
		action := m.pendingDestructiveActions[i]
		if action.Committing {
			continue
		}
		m.pendingDestructiveActions = append(m.pendingDestructiveActions[:i], m.pendingDestructiveActions[i+1:]...)
		m.applyFilter()
		m.focusRestoredMessage(action.Entries)
		m.setStatus(fmt.Sprintf("restored %d", len(action.Entries)), false)
		return m.clearStatusCmd()
	}
	return nil
}

func (m *Model) beginDestructiveCommit(id uint64) tea.Cmd {
	for i := range m.pendingDestructiveActions {
		if m.pendingDestructiveActions[i].ID != id || m.pendingDestructiveActions[i].Committing {
			continue
		}
		m.pendingDestructiveActions[i].Committing = true
		m.pendingDestructiveActions[i].Runtime = &destructiveActionRuntime{done: make(chan struct{})}
		action := m.pendingDestructiveActions[i]
		return m.commitDestructiveActionCmd(action)
	}
	return nil
}

func (m Model) commitDestructiveActionCmd(action pendingDestructiveAction) tea.Cmd {
	database := m.db
	sessions := m.sessions
	return func() tea.Msg {
		defer close(action.Runtime.done)
		result := executeDestructiveAction(action, database, sessions)
		action.Runtime.err = result.Err
		return result
	}
}

type destructiveBatchKey struct {
	sourceID int64
	targetID int64
}

func executeDestructiveAction(action pendingDestructiveAction, database *db.DB, sessions *imapClient.SessionPool) DestructiveActionResultMsg {
	result := DestructiveActionResultMsg{ID: action.ID, Kind: action.Kind}
	batches := make(map[destructiveBatchKey][]pendingDestructiveEntry)
	var order []destructiveBatchKey
	for _, entry := range action.Entries {
		key := destructiveBatchKey{sourceID: entry.Source.ID, targetID: entry.Target.ID}
		if _, ok := batches[key]; !ok {
			order = append(order, key)
		}
		batches[key] = append(batches[key], entry)
	}
	for _, key := range order {
		entries := batches[key]
		if err := executeDestructiveRemote(action.Kind, sessions, entries); err != nil {
			if result.Err == nil {
				result.Err = err
			}
			for _, entry := range entries {
				result.Failed = append(result.Failed, entry.Message)
			}
			continue
		}
		for _, entry := range entries {
			var err error
			if action.Kind == destructiveDelete {
				err = database.DeleteMessage(entry.Message.ID)
			} else {
				err = database.MoveMessage(entry.Message.ID, entry.Target.ID)
			}
			if err != nil {
				if result.Err == nil {
					result.Err = err
				}
				// A completed remote action remains hidden; sync will reconcile a
				// rare local cache write failure on the next mailbox refresh.
				if entry.Account.IMAPHost == "" || entry.Message.UID == 0 {
					result.Failed = append(result.Failed, entry.Message)
					continue
				}
			}
			result.Succeeded = append(result.Succeeded, MessageRef{ID: entry.Message.ID, MailboxID: entry.Message.MailboxID})
		}
	}
	return result
}

func executeDestructiveRemote(kind destructiveActionKind, sessions *imapClient.SessionPool, entries []pendingDestructiveEntry) error {
	if len(entries) == 0 || entries[0].Account.IMAPHost == "" {
		return nil
	}
	uids := make([]uint32, 0, len(entries))
	for _, entry := range entries {
		if entry.Message.UID != 0 {
			uids = append(uids, entry.Message.UID)
		}
	}
	if len(uids) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	entry := entries[0]
	return sessions.Do(ctx, entry.Account, func(client *imapClient.Client) error {
		if kind == destructiveDelete && entry.Target.ID == 0 {
			return client.DeleteMessages(ctx, entry.Source.Name, uids)
		}
		return client.MoveMessages(ctx, entry.Source.Name, uids, entry.Target.Name)
	})
}

func (m *Model) handleDestructiveResult(result DestructiveActionResultMsg) tea.Cmd {
	action, ok := m.removeDestructiveAction(result.ID)
	if !ok {
		return nil
	}
	for _, ref := range result.Succeeded {
		if m.removeMessageFromMemory(ref.ID) {
			m.adjustMailboxUnreadCount(ref.MailboxID, -1)
		}
	}
	m.applyFilter()
	m.clampMessageViewAfterDestructiveChange()
	verb := destructivePastTense(action.Kind)
	switch {
	case len(result.Failed) > 0 && len(result.Succeeded) == 0:
		m.setStatus(fmt.Sprintf("%s failed: %v", action.Kind, result.Err), true)
	case len(result.Failed) > 0:
		m.setStatus(fmt.Sprintf("%s %d; %d failed and restored: %v", verb, len(result.Succeeded), len(result.Failed), result.Err), true)
	case len(result.Succeeded) > 1:
		m.setStatus(fmt.Sprintf("%s %d", verb, len(result.Succeeded)), false)
	default:
		m.setStatus(verb, false)
	}
	return m.clearStatusCmd()
}

func (m *Model) removeDestructiveAction(id uint64) (pendingDestructiveAction, bool) {
	for i, action := range m.pendingDestructiveActions {
		if action.ID == id {
			m.pendingDestructiveActions = append(m.pendingDestructiveActions[:i], m.pendingDestructiveActions[i+1:]...)
			return action, true
		}
	}
	return pendingDestructiveAction{}, false
}

func (m Model) messagePendingDestructive(id int64) bool {
	for _, action := range m.pendingDestructiveActions {
		for _, entry := range action.Entries {
			if entry.Message.ID == id {
				return true
			}
		}
	}
	return false
}

func (m Model) pendingUnreadCount(mailboxID int64) int64 {
	var count int64
	for _, action := range m.pendingDestructiveActions {
		for _, entry := range action.Entries {
			if entry.Message.MailboxID == mailboxID && !entry.Message.Read {
				count++
			}
		}
	}
	return count
}

func (m Model) latestUndoableDestructive() *pendingDestructiveAction {
	for i := len(m.pendingDestructiveActions) - 1; i >= 0; i-- {
		if !m.pendingDestructiveActions[i].Committing {
			return &m.pendingDestructiveActions[i]
		}
	}
	return nil
}

func (m Model) destructiveUndoPrompt() string {
	action := m.latestUndoableDestructive()
	if action == nil {
		return ""
	}
	verb := destructivePastTense(action.Kind)
	if action.Kind == destructiveMove && len(action.Entries) > 0 {
		target := action.Entries[0].Target.DisplayName
		if target == "" {
			target = action.Entries[0].Target.Name
		}
		if len(action.Entries) > 1 {
			verb = fmt.Sprintf("moved %d to %s", len(action.Entries), cleanDisplayName(target))
		} else {
			verb = "moved to " + cleanDisplayName(target)
		}
	} else if len(action.Entries) > 1 {
		verb += fmt.Sprintf(" %d", len(action.Entries))
	}
	return verb + " · ctrl+z undo"
}

func destructivePastTense(kind destructiveActionKind) string {
	switch kind {
	case destructiveArchive:
		return "archived"
	case destructiveDelete:
		return "deleted"
	default:
		return "moved"
	}
}

func (m *Model) clampMessageViewAfterDestructiveChange() {
	count := m.activeMessageRowCount()
	if count == 0 {
		m.messageCursor = 0
		m.listOffset = 0
		m.clearViewportMessage()
		return
	}
	m.messageCursor = clamp(m.messageCursor, 0, count-1)
	m.listOffset = clamp(m.listOffset, 0, max(0, count-1))
	m.setViewportForCurrentRow()
}

func (m *Model) focusRestoredMessage(entries []pendingDestructiveEntry) {
	if len(entries) == 0 {
		m.clampMessageViewAfterDestructiveChange()
		return
	}
	id := entries[0].Message.ID
	if m.threadedMessagesEnabled() {
		for i, thread := range m.messageThreads {
			for _, msg := range thread.Messages {
				if msg.ID == id {
					m.messageCursor = i
					m.setViewportForCurrentRow()
					return
				}
			}
		}
	} else {
		for i, msg := range m.filteredMessages {
			if msg.ID == id {
				m.messageCursor = i
				m.setViewportForCurrentRow()
				return
			}
		}
	}
	m.clampMessageViewAfterDestructiveChange()
}

// FlushPendingDestructiveActions commits all cleanly pending actions before
// sessions close. It is used after Bubble Tea restores the terminal on exit.
func (m Model) FlushPendingDestructiveActions() error {
	var firstErr error
	for _, action := range m.pendingDestructiveActions {
		if action.Committing {
			if action.Runtime != nil {
				<-action.Runtime.done
				if action.Runtime.err != nil && firstErr == nil {
					firstErr = action.Runtime.err
				}
			}
			continue
		}
		result := executeDestructiveAction(action, m.db, m.sessions)
		if result.Err != nil && firstErr == nil {
			firstErr = result.Err
		}
	}
	return firstErr
}

func (m Model) HasPendingDestructiveActions() bool {
	return len(m.pendingDestructiveActions) > 0
}
