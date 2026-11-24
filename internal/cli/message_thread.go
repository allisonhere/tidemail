package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/allisonhere/tidemail/internal/conversation"
	"github.com/allisonhere/tidemail/internal/db"
)

// threadQueryChunk bounds the Message-IDs matched by one query.
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
			key := conversation.NormalizeMessageID(m.MessageID)
			if key != "" && seen[key] {
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

// threadCandidates returns the graph headers of the message and of every cached
// message linked to it through Message-ID, In-Reply-To or References, directly
// or transitively. Each round asks only for rows that mention a newly found ID,
// so the whole cache is never loaded. The substring match can include a few
// unrelated rows; BuildThreads then separates them, so the result is the same
// as building threads over the whole cache.
func threadCandidates(database *db.DB, id int64) ([]db.Message, error) {
	const columns = "SELECT id, message_id, in_reply_to, references_text, date FROM "
	byID := map[int64]db.Message{}
	known := map[string]bool{}
	var frontier []string
	add := func(rows []db.Message) {
		for _, m := range rows {
			if _, ok := byID[m.ID]; ok {
				continue
			}
			byID[m.ID] = m
			ids := append(conversation.MessageIDList(m.References), conversation.MessageIDList(m.InReplyTo)...)
			if own := conversation.NormalizeMessageID(m.MessageID); own != "" {
				ids = append(ids, own)
			}
			for _, key := range ids {
				bare := strings.Trim(key, "<>")
				if !known[bare] {
					known[bare] = true
					frontier = append(frontier, bare)
				}
			}
		}
	}
	first, err := scanThreadRows(database, columns+"messages WHERE id = ?", id)
	if err != nil {
		return nil, err
	}
	add(first)
	for len(frontier) > 0 {
		batch := frontier[:min(len(frontier), threadQueryChunk)]
		frontier = frontier[len(batch):]
		where := make([]string, len(batch))
		args := make([]any, len(batch))
		for i, key := range batch {
			where[i] = "instr(haystack, ?) > 0"
			args[i] = key
		}
		rows, err := scanThreadRows(database, columns+"(SELECT *, lower(message_id || ' ' || in_reply_to || ' ' || references_text) AS haystack FROM messages) WHERE "+strings.Join(where, " OR "), args...)
		if err != nil {
			return nil, err
		}
		add(rows)
	}
	messages := make([]db.Message, 0, len(byID))
	for _, m := range byID {
		messages = append(messages, m)
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
