package db

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Snooze is TideMail-local: it hides a message or a waiting conversation from
// the attention views (Needs You, Waiting on Them) until a time. It never
// touches the mail, its folder, its flags, or plugin annotations. A snooze is
// active while its row exists; the UI deletes rows when they come due.

// Snooze target types.
const (
	// SnoozeMessage hides one message (by MessageKey) from Needs You.
	SnoozeMessage = "message"
	// SnoozeThread hides a waiting conversation from Waiting on Them. Its key
	// is the current waiting cycle's key (the message that started the wait,
	// as for Stop waiting), so a reply or a new message of the user's ends it.
	SnoozeThread = "thread"
)

// Snooze is one stored snooze.
type Snooze struct {
	TargetType string
	TargetKey  string
	Until      time.Time
	CreatedAt  time.Time
}

// MessageKey identifies a message across folder copies: its normalized
// Message-ID, or its row ID when it has none. messageKeySQL computes the same
// key in SQL.
func MessageKey(m Message) string {
	id := strings.ToLower(strings.Trim(strings.TrimSpace(m.MessageID), "<>"))
	if id != "" && !strings.ContainsAny(id, " \t\r\n") {
		return "<" + id + ">"
	}
	return "row:" + strconv.FormatInt(m.ID, 10)
}

// messageKeySQL is MessageKey for the messages table.
const messageKeySQL = `(CASE
	WHEN trim(trim(messages.message_id), '<>') != '' AND instr(trim(messages.message_id), ' ') = 0
	THEN '<' || lower(trim(trim(messages.message_id), '<>')) || '>'
	ELSE 'row:' || messages.id END)`

func validSnoozeTarget(targetType, targetKey string) error {
	if targetType != SnoozeMessage && targetType != SnoozeThread {
		return fmt.Errorf("snooze: unknown target type %q", targetType)
	}
	if strings.TrimSpace(targetKey) == "" {
		return errors.New("snooze: target key is required")
	}
	return nil
}

// SetSnooze snoozes a target until the given time, replacing any earlier
// snooze of the same target.
func (db *DB) SetSnooze(targetType, targetKey string, until time.Time) error {
	if err := validSnoozeTarget(targetType, targetKey); err != nil {
		return err
	}
	if until.IsZero() || until.Unix() <= 0 {
		return errors.New("snooze: a wake time is required")
	}
	_, err := db.Exec(`
		INSERT INTO snoozes (target_type, target_key, snooze_until, created_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(target_type, target_key) DO UPDATE SET snooze_until = excluded.snooze_until`,
		targetType, targetKey, until.Unix(), time.Now().Unix())
	return err
}

// DeleteSnooze unsnoozes a target; no matching row is success.
func (db *DB) DeleteSnooze(targetType, targetKey string) error {
	if err := validSnoozeTarget(targetType, targetKey); err != nil {
		return err
	}
	_, err := db.Exec(`DELETE FROM snoozes WHERE target_type = ? AND target_key = ?`, targetType, targetKey)
	return err
}

// ListSnoozes returns every stored snooze, earliest wake time first.
func (db *DB) ListSnoozes() ([]Snooze, error) {
	rows, err := db.Query(`SELECT target_type, target_key, snooze_until, created_at FROM snoozes
		ORDER BY snooze_until, target_type, target_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Snooze
	for rows.Next() {
		var s Snooze
		var until, created int64
		if err := rows.Scan(&s.TargetType, &s.TargetKey, &until, &created); err != nil {
			return nil, err
		}
		s.Until, s.CreatedAt = time.Unix(until, 0), time.Unix(created, 0)
		out = append(out, s)
	}
	return out, rows.Err()
}

// NextSnoozeDeadline returns the earliest wake time after now.
func (db *DB) NextSnoozeDeadline(now time.Time) (time.Time, bool, error) {
	var until int64
	err := db.QueryRow(`SELECT COALESCE(MIN(snooze_until), 0) FROM snoozes WHERE snooze_until > ?`, now.Unix()).Scan(&until)
	if err != nil || until == 0 {
		return time.Time{}, false, err
	}
	return time.Unix(until, 0), true, nil
}

// DeleteDueSnoozes expires every snooze due at or before now and reports how
// many it removed.
func (db *DB) DeleteDueSnoozes(now time.Time) (int, error) {
	res, err := db.Exec(`DELETE FROM snoozes WHERE snooze_until <= ?`, now.Unix())
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// DeleteSnoozeKeys removes snoozes of one type by key, e.g. waiting cycles
// that ended.
func (db *DB) DeleteSnoozeKeys(targetType string, keys []string) error {
	for _, k := range keys {
		if err := db.DeleteSnooze(targetType, k); err != nil {
			return err
		}
	}
	return nil
}

// SnoozedMessage is a snoozed target with a message row to show for it: the
// message itself, or the waiting cycle's message for a thread.
type SnoozedMessage struct {
	Snooze
	Message Message
}

// ListSnoozedMessages returns snoozed targets that still have a cached
// message, earliest wake time first. Snoozes whose mail is gone are simply
// absent (and pruned at startup).
func (db *DB) ListSnoozedMessages() ([]SnoozedMessage, error) {
	snoozes, err := db.ListSnoozes()
	if err != nil || len(snoozes) == 0 {
		return nil, err
	}
	keys := map[string]bool{}
	for _, s := range snoozes {
		keys[s.TargetKey] = true
	}
	// One pass over messages whose key is snoozed; the lowest row ID stands
	// in for a message stored in several folders.
	args := make([]any, 0, len(keys))
	for k := range keys {
		args = append(args, k)
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")
	rows, err := db.Query(`SELECT messages.id, `+messageKeySQL+` FROM messages
		WHERE `+messageKeySQL+` IN (`+placeholders+`) ORDER BY messages.id`, args...)
	if err != nil {
		return nil, err
	}
	idByKey := map[string]int64{}
	for rows.Next() {
		var id int64
		var key string
		if err := rows.Scan(&id, &key); err != nil {
			rows.Close()
			return nil, err
		}
		if _, seen := idByKey[key]; !seen {
			idByKey[key] = id
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(idByKey))
	for _, id := range idByKey {
		ids = append(ids, id)
	}
	msgs, err := db.ListMessagesByIDs(ids)
	if err != nil {
		return nil, err
	}
	byID := map[int64]Message{}
	for _, m := range msgs {
		byID[m.ID] = m
	}
	byKey := map[string]Message{}
	for key, id := range idByKey {
		if m, ok := byID[id]; ok {
			byKey[key] = m
		}
	}
	var out []SnoozedMessage
	for _, s := range snoozes {
		if m, ok := byKey[s.TargetKey]; ok {
			out = append(out, SnoozedMessage{Snooze: s, Message: m})
		}
	}
	return out, nil
}

// PruneStaleSnoozes drops snoozes whose message is no longer cached.
func (db *DB) PruneStaleSnoozes() error {
	_, err := db.Exec(`DELETE FROM snoozes WHERE target_key NOT IN (SELECT ` + messageKeySQL + ` FROM messages)`)
	return err
}
