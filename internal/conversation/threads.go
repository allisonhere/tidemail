// Package conversation holds TideMail's conversation model: Message-ID /
// In-Reply-To / References threading, participant parsing, and Waiting on
// Them. It is pure (no database or UI access) so the message list, attention
// views, and plugin queries all share one thread engine.
package conversation

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/allisonhere/tidemail/internal/db"
)

var messageIDTokenRe = regexp.MustCompile(`<[^<>\s]+>`)

// Thread is one conversation: its messages oldest first, and the newest one
// as Representative.
type Thread struct {
	Key            string
	Representative db.Message
	Messages       []db.Message
	Count          int
	UnreadCount    int
}

// BuildThreads groups messages into conversations by Message-ID, In-Reply-To,
// and References, newest conversation first. A message without a Message-ID
// forms its own conversation unless another message references it.
func BuildThreads(messages []db.Message) []Thread {
	if len(messages) == 0 {
		return nil
	}

	parent := map[string]string{}
	msgKeys := make([]string, len(messages))
	ensure := func(id string) string {
		id = NormalizeMessageID(id)
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
		for _, ref := range MessageIDList(msg.References) {
			union(key, ref)
		}
		for _, ref := range MessageIDList(msg.InReplyTo) {
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

	threads := make([]Thread, 0, len(byRoot))
	for key, msgs := range byRoot {
		sort.SliceStable(msgs, func(i, j int) bool {
			if !msgs[i].Date.Equal(msgs[j].Date) {
				return msgs[i].Date.Before(msgs[j].Date)
			}
			return msgs[i].ID < msgs[j].ID
		})
		t := Thread{
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

func rowThreadKey(msg db.Message) string {
	if msg.ID != 0 {
		return "row:" + strconv.FormatInt(msg.ID, 10)
	}
	return "uid:" + strconv.FormatInt(int64(msg.MailboxID), 10) + ":" + strconv.FormatInt(int64(msg.UID), 10)
}

// MessageIDList returns the normalized Message-IDs in a header value.
func MessageIDList(s string) []string {
	var ids []string
	for _, match := range messageIDTokenRe.FindAllString(s, -1) {
		if id := NormalizeMessageID(match); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) > 0 {
		return ids
	}
	if id := NormalizeMessageID(s); id != "" {
		return []string{id}
	}
	return nil
}

// NormalizeMessageID lowercases a Message-ID and wraps it in angle brackets,
// or returns "" if s is not a usable ID.
func NormalizeMessageID(s string) string {
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
