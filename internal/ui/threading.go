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

func (m Model) threadedMessagesEnabled() bool {
	return m.cfg.Display.ThreadedConversations && !m.selectedDraftsMailbox()
}

func (m *Model) rebuildMessageThreads() {
	if m.threadedMessagesEnabled() {
		threads := buildMessageThreads(m.filteredMessages)
		if m.selectedNeedsYou() || m.selectedWaiting() || m.selectedSnoozed() {
			// These views arrive in their own order (attention rank, longest
			// wait, wake time); keep it instead of newest-first.
			keepListOrder(threads, m.filteredMessages)
		}
		if m.starredFirst {
			// buildMessageThreads orders by date; float threads containing a
			// starred message to the top while preserving that date order among
			// equals (mirrors sortFilteredStarred for the non-threaded list).
			sort.SliceStable(threads, func(i, j int) bool {
				return threadStarred(threads[i]) && !threadStarred(threads[j])
			})
		}
		m.messageThreads = threads
		return
	}
	m.messageThreads = nil
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

func (m Model) activeMessageRowCount() int {
	if m.threadedMessagesEnabled() {
		return len(m.messageThreads)
	}
	return len(m.filteredMessages)
}

func (m Model) currentRowMessage() *db.Message {
	if m.threadedMessagesEnabled() {
		if m.messageCursor < 0 || m.messageCursor >= len(m.messageThreads) {
			return nil
		}
		return &m.messageThreads[m.messageCursor].Representative
	}
	if m.messageCursor < 0 || m.messageCursor >= len(m.filteredMessages) {
		return nil
	}
	return &m.filteredMessages[m.messageCursor]
}

func (m Model) currentRowMessages() []db.Message {
	if m.threadedMessagesEnabled() {
		if m.messageCursor < 0 || m.messageCursor >= len(m.messageThreads) {
			return nil
		}
		return append([]db.Message(nil), m.messageThreads[m.messageCursor].Messages...)
	}
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

func (m Model) messageRowSelected(msg db.Message, thread messageThread) bool {
	if m.threadedMessagesEnabled() && thread.Count > 0 {
		for _, item := range thread.Messages {
			if m.selectedMessages[item.ID] {
				return true
			}
		}
		return false
	}
	return m.selectedMessages[msg.ID]
}

// keepListOrder sorts threads by where their first message appears in list.
func keepListOrder(threads []messageThread, list []db.Message) {
	pos := make(map[int64]int, len(list))
	for i, msg := range list {
		if _, ok := pos[msg.ID]; !ok {
			pos[msg.ID] = i
		}
	}
	first := func(t messageThread) int {
		best := len(list)
		for _, msg := range t.Messages {
			if p, ok := pos[msg.ID]; ok && p < best {
				best = p
			}
		}
		return best
	}
	sort.SliceStable(threads, func(i, j int) bool { return first(threads[i]) < first(threads[j]) })
}
