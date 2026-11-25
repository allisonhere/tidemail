package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/allisonhere/tidemail/internal/conversation"
	"github.com/allisonhere/tidemail/internal/db"
)

// threadQueryChunk bounds the row IDs read back by one query.
const threadQueryChunk = 100

// cachedThread uses the same header graph as the TUI, across all cached folders.
// It reads only graph headers for unrelated messages and never returns bodies.
func cachedThread(database *db.DB, id int64, metadata func(db.Message) (map[string]any, error)) (map[string]any, error) {
	messages, err := threadCandidates(database, id)
	if err != nil {
		return nil, err
	}
	for _, thread := range conversation.BuildThreads(messages) {
		found := false
		for _, m := range thread.Messages {
			if m.ID == id {
				found = true
				break
			}
		}
		if !found {
			continue
		}
		items := []map[string]any{}
		seen := map[string]bool{}
		// Graph construction precedes dedupe so copies can contribute references.
		for _, m := range thread.Messages {
			// The same rule as --dedupe on list and search.
			key := db.MessageKey(m)
			if seen[key] {
				continue
			}
			seen[key] = true
			full, e := database.GetMessage(m.ID)
			if e != nil {
				return nil, e
			}
			data, e := metadata(full)
			if e != nil {
				return nil, e
			}
			data["in_reply_to"] = full.InReplyTo
			data["references"] = full.References
			items = append(items, data)
		}
		return map[string]any{"items": items, "total": len(items)}, nil
	}
	return nil, fmt.Errorf("thread not found")
}

// threadCandidates reads the graph headers of every cached message in one
// pass and returns the rows in the same connected component as the message.
// BuildThreads groups messages by exactly these components (Message-ID,
// In-Reply-To and References links), so threading the returned rows gives the
// same thread as threading the whole cache. Only one key per row is kept during
// the scan; full headers are read again for the component alone.
func threadCandidates(database *db.DB, id int64) ([]db.Message, error) {
	parent := map[string]string{}
	find := func(k string) string {
		for {
			p, ok := parent[k]
			if !ok || p == k {
				return k
			}
			if gp := parent[p]; gp != "" {
				parent[k] = gp
			}
			k = p
		}
	}
	union := func(a, b string) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[rb] = ra
		}
	}
	rowKeys := map[int64]string{}
	rows, err := database.Query("SELECT id, message_id, in_reply_to, references_text FROM messages")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var m db.Message
		if err = rows.Scan(&m.ID, &m.MessageID, &m.InReplyTo, &m.References); err != nil {
			rows.Close()
			return nil, err
		}
		// The same row key as BuildThreads uses for a message without an ID.
		key := conversation.NormalizeMessageID(m.MessageID)
		if key == "" {
			key = "row:" + strconv.FormatInt(m.ID, 10)
		}
		rowKeys[m.ID] = key
		for _, ref := range append(conversation.MessageIDList(m.References), conversation.MessageIDList(m.InReplyTo)...) {
			union(key, ref)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	key, ok := rowKeys[id]
	if !ok {
		return nil, nil
	}
	root := find(key)
	var ids []any
	for rowID, k := range rowKeys {
		if find(k) == root {
			ids = append(ids, rowID)
		}
	}
	var messages []db.Message
	for len(ids) > 0 {
		batch := ids[:min(len(ids), threadQueryChunk)]
		ids = ids[len(batch):]
		found, err := scanThreadRows(database, "SELECT id, message_id, in_reply_to, references_text, date FROM messages WHERE id IN (?"+strings.Repeat(",?", len(batch)-1)+")", batch...)
		if err != nil {
			return nil, err
		}
		messages = append(messages, found...)
	}
	return messages, nil
}
func scanThreadRows(database *db.DB, query string, args ...any) ([]db.Message, error) {
	rows, err := database.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var messages []db.Message
	for rows.Next() {
		var m db.Message
		var date int64
		if err := rows.Scan(&m.ID, &m.MessageID, &m.InReplyTo, &m.References, &date); err != nil {
			return nil, err
		}
		m.Date = time.Unix(date, 0)
		messages = append(messages, m)
	}
	return messages, rows.Err()
}
