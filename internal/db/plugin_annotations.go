package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// PluginAnnotation is one key/value a plugin attached to a cached message. The
// db package stores annotations generically and never interprets them; the
// plugin package validates them before they get here.
type PluginAnnotation struct {
	PluginID   string
	MessageID  int64
	Key        string
	Value      string
	Confidence *float64 // nil when the plugin gave none
	UpdatedAt  time.Time
}

// annotationBatchSize keeps IN (...) lists well under SQLite's variable limit.
const annotationBatchSize = 500

// ReplacePluginAnnotations makes anns the complete set of pluginID's
// annotations on messageID, atomically. An empty anns clears that set. Other
// plugins' annotations on the message are untouched, and on any error the
// previous set is kept.
func (db *DB) ReplacePluginAnnotations(pluginID string, messageID int64, anns []PluginAnnotation) (err error) {
	if pluginID == "" || messageID <= 0 {
		return errors.New("replace plugin annotations: plugin id and message id are required")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if _, err = tx.Exec(`DELETE FROM plugin_annotations WHERE plugin_id = ? AND message_id = ?`, pluginID, messageID); err != nil {
		return fmt.Errorf("replace plugin annotations: %w", err)
	}
	now := time.Now().Unix()
	for _, a := range anns {
		var confidence sql.NullFloat64
		if a.Confidence != nil {
			confidence = sql.NullFloat64{Float64: *a.Confidence, Valid: true}
		}
		if _, err = tx.Exec(`
			INSERT INTO plugin_annotations (plugin_id, message_id, key, value, confidence, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)`,
			pluginID, messageID, a.Key, a.Value, confidence, now); err != nil {
			return fmt.Errorf("replace plugin annotations: %w", err)
		}
	}
	return tx.Commit()
}

// ListPluginAnnotations returns every plugin's annotations on one message,
// ordered by plugin ID then key.
func (db *DB) ListPluginAnnotations(messageID int64) ([]PluginAnnotation, error) {
	byMessage, err := db.ListPluginAnnotationsForMessages([]int64{messageID})
	if err != nil {
		return nil, err
	}
	return byMessage[messageID], nil
}

// ListPluginAnnotationsForMessages loads annotations for many messages in a
// few batched queries, keyed by message ID. Messages without annotations are
// absent from the map.
func (db *DB) ListPluginAnnotationsForMessages(messageIDs []int64) (map[int64][]PluginAnnotation, error) {
	out := map[int64][]PluginAnnotation{}
	for start := 0; start < len(messageIDs); start += annotationBatchSize {
		batch := messageIDs[start:min(start+annotationBatchSize, len(messageIDs))]
		args := make([]any, len(batch))
		for i, id := range batch {
			args[i] = id
		}
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		rows, err := db.Query(`
			SELECT plugin_id, message_id, key, value, confidence, updated_at
			FROM plugin_annotations
			WHERE message_id IN (`+placeholders+`)
			ORDER BY message_id, plugin_id, key`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var a PluginAnnotation
			var confidence sql.NullFloat64
			var updated int64
			if err := rows.Scan(&a.PluginID, &a.MessageID, &a.Key, &a.Value, &confidence, &updated); err != nil {
				rows.Close()
				return nil, err
			}
			if confidence.Valid {
				c := confidence.Float64
				a.Confidence = &c
			}
			a.UpdatedAt = time.Unix(updated, 0)
			out[a.MessageID] = append(out[a.MessageID], a)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
