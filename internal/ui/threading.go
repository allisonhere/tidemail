package ui

import (
	"regexp"
	"sort"
	"strings"

	"github.com/allisonhere/tide/internal/db"
)

var messageIDTokenRe = regexp.MustCompile(`<[^<>\s]+>`)

type messageThread struct {
	Key            string
	Representative db.Message
	Messages       []db.Message
	Count          int
	UnreadCount    int
}

func buildMessageThreads(messages []db.Message) []messageThread {
	if len(messages) == 0 {
		return nil
	}

	parent := map[string]string{}
	msgKeys := make([]string, len(messages))
	ensure := func(id string) string {
		id = normalizeMessageID(id)
		if id == "" {
			return ""
		}
		if _, ok := parent[id]; !ok {
			parent[id] = id
		}
		return id
	}
	var find func(string) string
	find = func(id string) string {
		p := parent[id]
		if p == "" || p == id {
			return id
		}
		root := find(p)
		parent[id] = root
		return root
	}
	union := func(a, b string) {
		a = ensure(a)
		b = ensure(b)
		if a == "" || b == "" {
			return
		}
		ra := find(a)
		rb := find(b)
		if ra != rb {
			parent[rb] = ra
		}
	}

	for i, msg := range messages {
		key := ensure(msg.MessageID)
		if key == "" {
			key = ensure(rowThreadKey(msg))
		}
		msgKeys[i] = key
		for _, ref := range messageIDList(msg.References) {
			union(key, ref)
		}
		for _, ref := range messageIDList(msg.InReplyTo) {
			union(key, ref)
		}
	}

	byRoot := map[string][]db.Message{}
	for i, msg := range messages {
		root := find(msgKeys[i])
		if root == "" {
			root = msgKeys[i]
		}
		byRoot[root] = append(byRoot[root], msg)
	}

	threads := make([]messageThread, 0, len(byRoot))
	for key, msgs := range byRoot {
		sort.SliceStable(msgs, func(i, j int) bool {
			if !msgs[i].Date.Equal(msgs[j].Date) {
				return msgs[i].Date.Before(msgs[j].Date)
			}
			return msgs[i].ID < msgs[j].ID
		})
		t := messageThread{
			Key:            key,
			Representative: msgs[len(msgs)-1],
			Messages:       msgs,
			Count:          len(msgs),
		}
		for _, msg := range msgs {
			if !msg.Read {
				t.UnreadCount++
			}
		}
		threads = append(threads, t)
	}

	sort.SliceStable(threads, func(i, j int) bool {
		a := threads[i].Representative
		b := threads[j].Representative
		if !a.Date.Equal(b.Date) {
			return a.Date.After(b.Date)
		}
		return a.ID > b.ID
	})
	return threads
}

func (m Model) threadedMessagesEnabled() bool {
	return m.cfg.Display.ThreadedConversations && !m.selectedDraftsMailbox()
}

func (m *Model) rebuildMessageThreads() {
	if m.threadedMessagesEnabled() {
		m.messageThreads = buildMessageThreads(m.filteredMessages)
		return
	}
	m.messageThreads = nil
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

func rowThreadKey(msg db.Message) string {
	if msg.ID != 0 {
		return "row:" + strconvFormatInt(msg.ID)
	}
	return "uid:" + strconvFormatInt(int64(msg.MailboxID)) + ":" + strconvFormatInt(int64(msg.UID))
}

func messageIDList(s string) []string {
	var ids []string
	for _, match := range messageIDTokenRe.FindAllString(s, -1) {
		if id := normalizeMessageID(match); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) > 0 {
		return ids
	}
	if id := normalizeMessageID(s); id != "" {
		return []string{id}
	}
	return nil
}

func normalizeMessageID(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.Trim(s, "<>")
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, " \t\r\n") {
		return ""
	}
	return strings.ToLower("<" + s + ">")
}

func strconvFormatInt(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
