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

// DeletePluginAnnotations removes one plugin's annotations from one message.
// Other plugins' annotations and the message itself are untouched; no
// matching rows is success.
func (db *DB) DeletePluginAnnotations(pluginID string, messageID int64) error {
	if pluginID == "" || messageID <= 0 {
		return errors.New("delete plugin annotations: plugin id and message id are required")
	}
	_, err := db.Exec(`DELETE FROM plugin_annotations WHERE plugin_id = ? AND message_id = ?`, pluginID, messageID)
	return err
}

// DeleteMessagePluginAnnotations removes every plugin's annotations from one
// message, leaving the message itself alone.
func (db *DB) DeleteMessagePluginAnnotations(messageID int64) error {
	if messageID <= 0 {
		return errors.New("delete message annotations: message id is required")
	}
	_, err := db.Exec(`DELETE FROM plugin_annotations WHERE message_id = ?`, messageID)
	return err
}

// DeletePluginAnnotationsForPlugin removes one plugin's annotations from every
// message. It never touches messages, and works whether or not the plugin is
// still installed.
func (db *DB) DeletePluginAnnotationsForPlugin(pluginID string) error {
	if pluginID == "" {
		return errors.New("delete plugin annotations: plugin id is required")
	}
	_, err := db.Exec(`DELETE FROM plugin_annotations WHERE plugin_id = ?`, pluginID)
	return err
}

// PluginAnnotationCounts returns how many annotations each plugin ID has
// stored, in one query. Plugin IDs without annotations are absent, so the keys
// are exactly the plugins that have stored data.
func (db *DB) PluginAnnotationCounts() (map[string]int64, error) {
	rows, err := db.Query(`SELECT plugin_id, COUNT(*) FROM plugin_annotations GROUP BY plugin_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var id string
		var n int64
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}
