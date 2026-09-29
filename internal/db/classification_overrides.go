package db

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	ClassificationNeedsReply = "needs_reply"
	ClassificationUrgency    = "urgency"
	ClassificationImportance = "importance"
	ClassificationCategory   = "category"
	ClassificationNone       = "none"
	ClassificationPlugin     = "plugin"
)

var customCategory = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,11}$`)

// ClassificationOverride is a local user decision for one semantic field.
// It intentionally has no plugin ID: the decision wins over all plugins.
type ClassificationOverride struct {
	MessageID int64
	Key       string
	Value     string
	UpdatedAt time.Time
}

func ValidateClassificationOverride(key, value string) (string, error) {
	key = strings.ToLower(strings.TrimSpace(key))
	value = strings.ToLower(strings.TrimSpace(value))
	switch key {
	case ClassificationNeedsReply:
		if value != "true" && value != "false" && value != ClassificationPlugin {
			return "", fmt.Errorf("invalid %s override %q", key, value)
		}
	case ClassificationImportance:
		if value == "true" {
			value = "high"
		}
		if value == "false" {
			value = "normal"
		}
		if value != "high" && value != "normal" && value != ClassificationPlugin {
			return "", fmt.Errorf("invalid importance override %q", value)
		}
	case ClassificationUrgency:
		if value != "high" && value != "normal" && value != ClassificationPlugin {
			return "", fmt.Errorf("invalid urgency override %q", value)
		}
	case ClassificationCategory:
		if value != ClassificationPlugin && value != ClassificationNone && !customCategory.MatchString(value) {
			return "", fmt.Errorf("invalid category override %q", value)
		}
	default:
		return "", fmt.Errorf("unsupported classification override key %q", key)
	}
	return value, nil
}

func (db *DB) SetClassificationOverride(messageID int64, key, value string) error {
	if messageID <= 0 {
		return errors.New("classification override: message id is required")
	}
	key = strings.ToLower(strings.TrimSpace(key))
	normalized, err := ValidateClassificationOverride(key, value)
	if err != nil {
		return err
	}
	_, err = db.Exec(`
		INSERT INTO classification_overrides (message_id, key, value, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(message_id, key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		messageID, key, normalized, time.Now().Unix())
	return err
}

func (db *DB) DeleteClassificationOverride(messageID int64, key string) error {
	if messageID <= 0 {
		return errors.New("classification override: message id is required")
	}
	key = strings.ToLower(strings.TrimSpace(key))
	if _, err := ValidateClassificationOverride(key, ClassificationPlugin); err != nil {
		return err
	}
	_, err := db.Exec(`DELETE FROM classification_overrides WHERE message_id = ? AND key = ?`, messageID, key)
	return err
}

func (db *DB) DeleteClassificationOverrides(messageID int64) error {
	if messageID <= 0 {
		return errors.New("classification override: message id is required")
	}
	_, err := db.Exec(`DELETE FROM classification_overrides WHERE message_id = ?`, messageID)
	return err
}

func (db *DB) GetClassificationOverrides(messageID int64) (map[string]ClassificationOverride, error) {
	all, err := db.GetClassificationOverridesForMessages([]int64{messageID})
	return all[messageID], err
}

func (db *DB) GetClassificationOverridesForMessages(messageIDs []int64) (map[int64]map[string]ClassificationOverride, error) {
	out := map[int64]map[string]ClassificationOverride{}
	for start := 0; start < len(messageIDs); start += annotationBatchSize {
		end := min(start+annotationBatchSize, len(messageIDs))
		args := make([]any, end-start)
		for i, id := range messageIDs[start:end] {
			args[i] = id
		}
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")
		rows, err := db.Query(`SELECT message_id, key, value, updated_at FROM classification_overrides WHERE message_id IN (`+placeholders+`) ORDER BY message_id, key`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, updated int64
			var key, value string
			if err := rows.Scan(&id, &key, &value, &updated); err != nil {
				rows.Close()
				return nil, err
			}
			if out[id] == nil {
				out[id] = map[string]ClassificationOverride{}
			}
			out[id][key] = ClassificationOverride{MessageID: id, Key: key, Value: value, UpdatedAt: time.Unix(updated, 0)}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}
