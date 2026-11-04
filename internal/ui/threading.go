package ui

import (
	"sort"

	"github.com/allisonhere/tidemail/internal/conversation"
	"github.com/allisonhere/tidemail/internal/db"
)

// messageThread is a conversation from TideMail's shared thread engine.
type messageThread = conversation.Thread

func buildMessageThreads(messages []db.Message) []messageThread {
	return conversation.BuildThreads(messages)
}

func (m *Model) threadedMessagesEnabled() bool {
	return m.cfg.Display.ThreadedConversations && !m.selectedDraftsMailbox()
}

// messageRow references one real message; index zero is the conversation parent.
// The shared thread engine remains chronological; only its UI result is ranked.
type messageRow struct {
	ThreadIndex  int
	MessageIndex int
}

// Expansion belongs to one folder or search result, never to persistent mail state.
type conversationView struct {
	kind      sidebarRowKind
	mailboxID int64
	query     string
}

func (m *Model) conversationView() conversationView {
	if m.searchActive() {
		return conversationView{query: m.searchQuery}
	}
	if m.sidebarCursor >= 0 && m.sidebarCursor < len(m.sidebarRows) {
		row := m.sidebarRows[m.sidebarCursor]
		return conversationView{kind: row.kind, mailboxID: row.mailboxID}
	}
	return conversationView{}
}

func (m *Model) rebuildMessageThreads() {
	if !m.threadedMessagesEnabled() {
		m.messageThreads = nil
		m.messageRows = nil
		m.expandedMessages = nil
		return
	}
	threads := buildMessageThreads(m.filteredMessages)
	virtual := m.selectedNeedsYou() || m.selectedWaiting() || m.selectedSnoozed()
	var positions map[int64]int
	if virtual {
		positions = make(map[int64]int, len(m.filteredMessages))
		for i, msg := range m.filteredMessages {
			positions[msg.ID] = i
		}
	}
	less := func(a, b db.Message) bool {
		if virtual {
			return positions[a.ID] < positions[b.ID]
		}
		if m.starredFirst && a.Starred != b.Starred {
			return a.Starred
		}
		if m.cfg.Display.UnreadFirst && a.Read != b.Read {
			return !a.Read
		}
		if !a.Date.Equal(b.Date) {
			return a.Date.After(b.Date)
		}
		return a.ID > b.ID
	}
	for i := range threads {
		t := &threads[i]
		sort.SliceStable(t.Messages, func(i, j int) bool { return less(t.Messages[i], t.Messages[j]) })
		t.Representative = t.Messages[0]
	}
	sort.SliceStable(threads, func(i, j int) bool {
		if m.starredFirst && threadStarred(threads[i]) != threadStarred(threads[j]) {
			return threadStarred(threads[i])
		}
		return less(threads[i].Representative, threads[j].Representative)
	})
	m.messageThreads = threads
	m.rebuildMessageRows()
}

func (m *Model) threadExpanded(thread messageThread) bool {
	for _, msg := range thread.Messages {
		if m.expandedMessages[msg.ID] {
			return true
		}
	}
	return false
}

func (m *Model) rebuildMessageRows() {
	m.messageRows = nil
	for i, thread := range m.messageThreads {
		m.messageRows = append(m.messageRows, messageRow{ThreadIndex: i})
		if !m.threadExpanded(thread) {
			continue
		}
		for j, msg := range thread.Messages {
			m.expandedMessages[msg.ID] = true // include new arrivals
			if j > 0 {
				m.messageRows = append(m.messageRows, messageRow{ThreadIndex: i, MessageIndex: j})
			}
		}
	}
}

func (m *Model) rowMessage(index int) *db.Message {
	if m.threadedMessagesEnabled() {
		if index < 0 || index >= len(m.messageRows) {
			return nil
		}
		row := m.messageRows[index]
		return &m.messageThreads[row.ThreadIndex].Messages[row.MessageIndex]
	}
	if index < 0 || index >= len(m.filteredMessages) {
		return nil
	}
	return &m.filteredMessages[index]
}

// currentParentThread is deliberately separate from ordinary message actions.
func (m *Model) currentParentThread() *messageThread {
	if !m.threadedMessagesEnabled() || m.messageCursor < 0 || m.messageCursor >= len(m.messageRows) {
		return nil
	}
	row := m.messageRows[m.messageCursor]
	if row.MessageIndex != 0 {
		return nil
	}
	return &m.messageThreads[row.ThreadIndex]
}

// Collapsed rows select a conversation; expanded rows select an exact email.
func (m *Model) currentConversationCollapsed() bool {
	thread := m.currentParentThread()
	return thread != nil && thread.Count > 1 && !m.threadExpanded(*thread)
}

// A SQL snapshot can see a successful write before its completion event arrives.
// Preserve the collapsed read selection during that pending window as well.
func (m *Model) preserveCollapsedReadSelection(readReload bool) bool {
	if !m.currentConversationCollapsed() {
		return false
	}
	thread := m.currentParentThread()
	return readReload || m.rowReadPending(thread.Representative, *thread)
}

// A collapsed selection follows its representative without revealing children.
// Explicit message requests still use indexOfFilteredMessage directly.
func (m *Model) indexOfMessageSelection(messageID int64, collapsed bool) int {
	if collapsed && m.threadedMessagesEnabled() {
		for i, row := range m.messageRows {
			if row.MessageIndex != 0 {
				continue
			}
			for _, msg := range m.messageThreads[row.ThreadIndex].Messages {
				if msg.ID == messageID {
					return i
				}
			}
		}
		return -1
	}
	return m.indexOfFilteredMessage(messageID)
}

func (m *Model) toggleCurrentConversation() bool {
	thread := m.currentParentThread()
	if thread == nil || thread.Count < 2 {
		return false
	}
	id := thread.Representative.ID
	expanded := m.threadExpanded(*thread)
	if m.expandedMessages == nil {
		m.expandedMessages = make(map[int64]bool)
	}
	for _, msg := range thread.Messages {
		if expanded {
			delete(m.expandedMessages, msg.ID)
		} else {
			m.expandedMessages[msg.ID] = true
		}
	}
	m.rebuildMessageRows()
	m.messageCursor = m.indexOfFilteredMessage(id)
	m.keepMessageCursorVisible()
	return true
}

// Keep the existing cursor/offset model for every visible row.
func (m *Model) keepMessageCursorVisible() {
	count := m.activeMessageRowCount()
	m.messageCursor = clamp(m.messageCursor, 0, max(0, count-1))
	m.listOffset = clamp(m.listOffset, 0, max(0, count-1))
	visible := max(1, m.articleRowsVisible())
	if m.messageCursor < m.listOffset {
		m.listOffset = m.messageCursor
	} else if m.messageCursor >= m.listOffset+visible {
		m.listOffset = m.messageCursor - visible + 1
	}
}

// threadStarred reports whether any message in the thread is starred.
func threadStarred(t messageThread) bool {
	for _, msg := range t.Messages {
		if msg.Starred {
			return true
		}
	}
	return false
}

func (m *Model) activeMessageRowCount() int {
	if m.threadedMessagesEnabled() {
		return len(m.messageRows)
	}
	return len(m.filteredMessages)
}

func (m *Model) currentRowMessage() *db.Message {
	return m.rowMessage(m.messageCursor)
}

// Ordinary actions always address real selected emails, including parents.
func (m Model) currentRowMessages() []db.Message {
	if msg := m.currentRowMessage(); msg != nil {
		return []db.Message{*msg}
	}
	return nil
}

func (m Model) selectedActionMessages() []db.Message {
	if !m.hasSelection() {
		return m.currentRowMessages()
	}
	msgs := make([]db.Message, 0, len(m.selectedMessages))
	for _, msg := range m.filteredMessages {
		if m.selectedMessages[msg.ID] {
			msgs = append(msgs, msg)
		}
	}
	return msgs
}

func (m *Model) toggleCurrentRowSelection() {
	msgs := m.currentRowMessages()
	if len(msgs) == 0 {
		return
	}
	allSelected := true
	for _, msg := range msgs {
		if !m.selectedMessages[msg.ID] {
			allSelected = false
			break
		}
	}
	for _, msg := range msgs {
		if allSelected {
			delete(m.selectedMessages, msg.ID)
		} else {
			m.selectedMessages[msg.ID] = true
		}
	}
}

func (m *Model) messageRowSelected(msg db.Message, _ messageThread) bool {
	return m.selectedMessages[msg.ID]
}
