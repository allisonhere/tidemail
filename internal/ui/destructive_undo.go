package ui

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	imapClient "github.com/allisonhere/tidemail/internal/imap"
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
	Deadline   time.Time
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
	ID          uint64
	Kind        destructiveActionKind
	Succeeded   []MessageRef
	CacheFailed []MessageRef
	CacheErr    error
	RemoteErr   error
	Failed      []db.Message
	Err         error
}

func (m *Model) scheduleArchive(msgs []db.Message) tea.Cmd {
	entries := make([]pendingDestructiveEntry, 0, len(msgs))
	for _, msg := range msgs {
		if m.messageActionPending(msg.ID) || m.pendingBulkReads[msg.MailboxID] != 0 {
			continue
		}
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
		acfg, err := m.accountCfgForMailbox(msg.MailboxID)
		if err != nil {
			m.setStatus("archive failed: "+err.Error(), true)
			return m.clearStatusCmd()
		}
		entries = append(entries, pendingDestructiveEntry{Message: msg, Source: *source, Target: target, Account: acfg})
	}
	return m.scheduleDestructive(destructiveArchive, entries)
}

func (m *Model) scheduleMove(msgs []db.Message, target db.Mailbox) tea.Cmd {
	entries := make([]pendingDestructiveEntry, 0, len(msgs))
	for _, msg := range msgs {
		if m.messageActionPending(msg.ID) || m.pendingBulkReads[msg.MailboxID] != 0 {
			continue
		}
		source := m.mailboxByID(msg.MailboxID)
		if source == nil || source.AccountID != target.AccountID {
			m.setStatus("move failed: source and target must be in one account", true)
			continue
		}
		acfg, err := m.accountCfgForMailbox(msg.MailboxID)
		if err != nil {
			m.setStatus("move failed: "+err.Error(), true)
			return m.clearStatusCmd()
		}
		entries = append(entries, pendingDestructiveEntry{Message: msg, Source: *source, Target: target, Account: acfg})
	}
	return m.scheduleDestructive(destructiveMove, entries)
}

func (m *Model) scheduleDelete(msgs []db.Message) tea.Cmd {
	entries := make([]pendingDestructiveEntry, 0, len(msgs))
	for _, msg := range msgs {
		if m.messageActionPending(msg.ID) || m.pendingBulkReads[msg.MailboxID] != 0 {
			continue
		}
		source := m.mailboxByID(msg.MailboxID)
		if source == nil {
			m.setStatus("delete failed: mailbox not found", true)
			continue
		}
		acfg, err := m.accountCfgForMailbox(msg.MailboxID)
		if err != nil {
			m.setStatus("delete failed: "+err.Error(), true)
			return m.clearStatusCmd()
		}
		entry := pendingDestructiveEntry{Message: msg, Source: *source, Account: acfg}
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
	m.pendingDestructiveActions = append(m.pendingDestructiveActions, pendingDestructiveAction{ID: id, Kind: kind, Entries: entries, Deadline: time.Now().Add(destructiveUndoWindow)})
	m.clearSelection()
	m.applyFilter()
	m.clampMessageViewAfterDestructiveChange()
	timer := tea.Tick(destructiveUndoWindow, func(time.Time) tea.Msg { return CommitDestructiveActionMsg{ID: id} })
	if kind == destructiveDelete {
		if current := m.currentRowMessage(); current != nil {
			m.advanceReadCursor(current.ID)
		}
	}
	// The read clock also redraws the undo gauge in the status bar.
	return tea.Batch(timer, m.ensureReadAnimation())
}

func (m *Model) undoLatestDestructive() tea.Cmd {
	for i := len(m.pendingDestructiveActions) - 1; i >= 0; i-- {
		action := m.pendingDestructiveActions[i]
		if action.Committing {
			continue
		}
		currentID := int64(0)
		if current := m.currentRowMessage(); current != nil {
			currentID = current.ID
		}
		m.removeDestructiveAction(action.ID)
		m.applyFilter()
		if action.Kind != destructiveDelete {
			m.focusRestoredMessage(action.Entries)
		} else if idx := m.indexOfFilteredMessage(currentID); idx >= 0 {
			// A pending delete never left the list and advanced the highlight;
			// undo keeps it on the same message even if rebuilding rows moved it.
			m.messageCursor = idx
			m.keepMessageCursorVisible()
		}
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
	return executeDestructiveBatches(action, database, func(entries []pendingDestructiveEntry) error {
		return executeDestructiveRemote(action.Kind, database, sessions, entries)
	})
}

// Keep remote confirmation and local result classification independently testable.
func executeDestructiveBatches(action pendingDestructiveAction, database *db.DB, remote func([]pendingDestructiveEntry) error) DestructiveActionResultMsg {
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
		if err := remote(entries); err != nil {
			if result.RemoteErr == nil {
				result.RemoteErr = err
			}
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
			} else if entry.Account.IMAPHost != "" && entry.Message.UID != 0 {
				err = database.EvictMessage(entry.Message.ID)
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
				if action.Kind == destructiveDelete {
					if result.CacheErr == nil {
						result.CacheErr = err
					}
					result.CacheFailed = append(result.CacheFailed, MessageRef{ID: entry.Message.ID, MailboxID: entry.Message.MailboxID})
				}
			}
			result.Succeeded = append(result.Succeeded, MessageRef{ID: entry.Message.ID, MailboxID: entry.Message.MailboxID})
		}
	}
	return result
}

func executeDestructiveRemote(kind destructiveActionKind, database *db.DB, sessions *imapClient.SessionPool, entries []pendingDestructiveEntry) error {
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
	ctx = uidWriteContext(ctx, database, entry.Source.ID, entry.Source.Name)
	return sessions.Do(ctx, entry.Account, func(client *imapClient.Client) error {
		if kind == destructiveDelete && entry.Target.ID == 0 {
			return client.DeleteMessages(ctx, entry.Source.Name, uids)
		}
		return client.MoveMessages(ctx, entry.Source.Name, uids, entry.Target.Name)
	})
}

func (m *Model) handleDestructiveResult(result DestructiveActionResultMsg) tea.Cmd {
	// The working pill is on screen unless another action's undo pill is.
	workingShown := m.latestUndoableDestructive() == nil
	action, ok := m.removeDestructiveAction(result.ID)
	if !ok {
		return nil
	}
	currentID := int64(0)
	if current := m.currentRowMessage(); current != nil {
		currentID = current.ID
	}
	if m.threadedMessagesEnabled() {
		removed := make(map[int64]bool, len(result.Succeeded))
		for _, ref := range result.Succeeded {
			removed[ref.ID] = true
		}
		if removed[currentID] {
			// Keep the conversation selected when its representative is deleted.
			members := m.currentRowMessages()
			if thread := m.currentParentThread(); thread != nil {
				members = thread.Messages
			}
			for _, member := range members {
				if !removed[member.ID] {
					currentID = member.ID
					break
				}
			}
		}
	}
	if len(result.Succeeded) > 0 {
		// Reject list snapshots captured before this confirmed removal.
		m.readVersion++
	}
	for _, ref := range result.CacheFailed {
		if m.confirmedDeleteCache == nil {
			m.confirmedDeleteCache = make(map[int64]MessageRef)
		}
		m.confirmedDeleteCache[ref.ID] = ref
	}
	mailboxes := make(map[int64]bool)
	for _, ref := range result.Succeeded {
		mailboxes[ref.MailboxID] = true
		if m.needsYou.messageIDs[ref.ID] {
			delete(m.needsYou.messageIDs, ref.ID)
			m.needsYou.count--
		}
		m.removeMessageFromMemory(ref.ID)
	}
	for id := range mailboxes {
		if err := m.recountMailboxUnread(id); err != nil && result.Err == nil {
			result.Err = err
		}
	}
	m.applyFilter()
	if idx := m.indexOfFilteredMessage(currentID); idx >= 0 {
		m.messageCursor = idx
	}
	m.clampMessageViewAfterDestructiveChange()
	verb := destructivePastTense(action.Kind)
	switch {
	case len(result.CacheFailed) > 0 && len(result.Failed) > 0:
		failedErr := result.RemoteErr
		if failedErr == nil {
			failedErr = result.Err
		}
		m.setStatus(fmt.Sprintf("%s %d; %d failed: %v; local cache update failed for %d: %v", verb, len(result.Succeeded), len(result.Failed), failedErr, len(result.CacheFailed), result.CacheErr), true)
	case len(result.CacheFailed) > 0:
		m.setStatus(fmt.Sprintf("%s %d; %d failed; local cache update failed for %d: %v", verb, len(result.Succeeded), len(result.Failed), len(result.CacheFailed), result.CacheErr), true)
	case result.Err != nil && len(result.Failed) == 0:
		m.setStatus(fmt.Sprintf("%s; local cache update failed: %v", verb, result.Err), true)
	case len(result.Failed) > 0 && len(result.Succeeded) == 0:
		m.setStatus(fmt.Sprintf("%s failed: %v", action.Kind, result.Err), true)
	case len(result.Failed) > 0:
		m.setStatus(fmt.Sprintf("%s %d; %d failed and restored: %v", verb, len(result.Succeeded), len(result.Failed), result.Err), true)
	case len(result.Succeeded) > 1:
		m.setStatusPill(fmt.Sprintf("%s %d", verb, len(result.Succeeded)))
	default:
		m.setStatusPill(verb)
	}
	if workingShown && (m.statusPill || m.statusErr) {
		// Fade the yellow working pill into the green or red result.
		return tea.Batch(m.clearStatusCmd(), m.startPillFade(alertPillYellow))
	}
	return m.clearStatusCmd()
}

func (m *Model) removeDestructiveAction(id uint64) (pendingDestructiveAction, bool) {
	for i, action := range m.pendingDestructiveActions {
		if action.ID == id {
			for _, entry := range action.Entries {
				delete(m.pendingUnreadExcluded, entry.Message.ID)
			}
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
		if action.Kind == destructiveDelete {
			continue
		}
		for _, entry := range action.Entries {
			if entry.Message.MailboxID == mailboxID && !entry.Message.Read && !m.pendingUnreadExcluded[entry.Message.ID] {
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
	return destructiveVerb(*action, false) + " · ctrl+z undo"
}

// committingDestructive returns the latest action whose undo window has passed
// and is now waiting on the server, or nil.
func (m Model) committingDestructive() *pendingDestructiveAction {
	for i := len(m.pendingDestructiveActions) - 1; i >= 0; i-- {
		if m.pendingDestructiveActions[i].Committing {
			return &m.pendingDestructiveActions[i]
		}
	}
	return nil
}

// destructiveWorkingPrompt is the status pill shown while an action waits on
// the server, after its undo window: "deleting ⠋", "moving to Work ⠋". Plain
// themes get an ellipsis instead of the spinner.
func (m Model) destructiveWorkingPrompt() string {
	action := m.committingDestructive()
	if action == nil {
		return ""
	}
	verb := destructiveVerb(*action, true)
	if m.styles.PlainUI {
		return verb + "…"
	}
	return verb + " " + readAnimationFrames[m.readAnimationFrame%len(readAnimationFrames)]
}

// destructiveVerb describes an action for the status bar, as it waits out its
// undo window ("delete pending", "moved 2 to Work") or, when working, while the
// server confirms it ("deleting", "moving 2 to Work").
func destructiveVerb(action pendingDestructiveAction, working bool) string {
	verb := destructivePastTense(action.Kind)
	switch {
	case working:
		verb = destructivePresentTense(action.Kind)
	case action.Kind == destructiveDelete:
		verb = "delete pending"
	}
	if action.Kind == destructiveMove && len(action.Entries) > 0 {
		target := action.Entries[0].Target.DisplayName
		if target == "" {
			target = action.Entries[0].Target.Name
		}
		if len(action.Entries) > 1 {
			return fmt.Sprintf("%s %d to %s", verb, len(action.Entries), cleanDisplayName(target))
		}
		return verb + " to " + cleanDisplayName(target)
	}
	if len(action.Entries) > 1 {
		verb += fmt.Sprintf(" %d", len(action.Entries))
	}
	return verb
}

func destructivePresentTense(kind destructiveActionKind) string {
	switch kind {
	case destructiveArchive:
		return "archiving"
	case destructiveDelete:
		return "deleting"
	default:
		return "moving"
	}
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
	m.keepMessageCursorVisible()
	m.setViewportForCurrentRow()
}

func (m *Model) focusRestoredMessage(entries []pendingDestructiveEntry) {
	if len(entries) == 0 {
		m.clampMessageViewAfterDestructiveChange()
		return
	}
	id := entries[0].Message.ID
	if idx := m.indexOfFilteredMessage(id); idx >= 0 {
		m.messageCursor = idx
		m.keepMessageCursorVisible()
		m.setViewportForCurrentRow()
		return
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

// Delete row feedback, the status bar's undo gauge and the working pill's
// spinner share the read clock; each deadline remains independent.
func (m Model) deleteFeedbackActive() bool {
	return len(m.pendingDestructiveActions) > 0
}

func (m Model) messageActionPending(id int64) bool {
	_, read := m.pendingReads[id]
	_, suppressed := m.confirmedDeleteCache[id]
	return read || suppressed || m.messagePendingDestructive(id)
}

func (m Model) messageHiddenDestructive(id int64) bool {
	if _, suppressed := m.confirmedDeleteCache[id]; suppressed {
		return true
	}
	for _, action := range m.pendingDestructiveActions {
		if action.Kind == destructiveDelete {
			continue
		}
		for _, entry := range action.Entries {
			if entry.Message.ID == id {
				return true
			}
		}
	}
	return false
}

func (m *Model) rowDeleteIndicator(msg db.Message, thread messageThread, now time.Time) string {
	indicator := ""
	for _, action := range m.pendingDestructiveActions {
		if action.Kind != destructiveDelete {
			continue
		}
		for _, entry := range action.Entries {
			affected := entry.Message.ID == msg.ID
			for _, member := range thread.Messages {
				affected = affected || member.ID == entry.Message.ID
			}
			if !affected {
				continue
			}
			if action.Committing {
				return m.pendingReadIndicator()
			}
			seconds := max(1, min(6, int((action.Deadline.Sub(now)+time.Second-1)/time.Second)))
			value := fmt.Sprint(seconds)
			if indicator == "" || value < indicator {
				indicator = value
			}
		}
	}
	return indicator
}

// A failed local write must not undo confirmed remote deletion. Retry only the
// local cleanup on list reloads; no new timer, IMAP request, or persistent queue.
func (m *Model) reconcileConfirmedDeletes(messages []db.Message) []db.Message {
	if len(m.confirmedDeleteCache) == 0 {
		return messages
	}
	filtered := make([]db.Message, 0, len(messages))
	for _, msg := range messages {
		if _, suppressed := m.confirmedDeleteCache[msg.ID]; !suppressed {
			filtered = append(filtered, msg)
		}
	}
	for id, ref := range m.confirmedDeleteCache {
		err := m.db.DeleteMessage(id)
		if err != nil {
			_, lookupErr := m.db.GetMessage(id)
			if !errors.Is(lookupErr, sql.ErrNoRows) {
				continue
			}
		}
		delete(m.confirmedDeleteCache, id)
		if m.needsYou.messageIDs[id] {
			delete(m.needsYou.messageIDs, id)
			m.needsYou.count--
		}
		m.readVersion++
		if err := m.recountMailboxUnread(ref.MailboxID); err != nil {
			m.setStatus("delete cache reconciliation failed: "+err.Error(), true)
		}
	}
	return filtered
}
