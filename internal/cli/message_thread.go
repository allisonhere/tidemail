package cli

import (
	"fmt"
	"github.com/allisonhere/tidemail/internal/conversation"
	"github.com/allisonhere/tidemail/internal/db"
	"time"
)

// cachedThread uses the same header graph as the TUI, across all cached folders.
// It reads only graph headers for unrelated messages and never returns bodies.
func cachedThread(database *db.DB, id int64, metadata func(db.Message) (map[string]any, error)) (map[string]any, error) {
	rows, err := database.Query("SELECT id, message_id, in_reply_to, references_text, date FROM messages ORDER BY id")
	if err != nil {
		return nil, err
	}
	var messages []db.Message
	for rows.Next() {
		var m db.Message
		var date int64
		if err = rows.Scan(&m.ID, &m.MessageID, &m.InReplyTo, &m.References, &date); err != nil {
			rows.Close()
			return nil, err
		}
		m.Date = time.Unix(date, 0)
		messages = append(messages, m)
	}
	err = rows.Err()
	rows.Close()
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
