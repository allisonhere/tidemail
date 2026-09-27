package db

import (
	"errors"
	"strings"
	"time"
)

// Needs You: inbox messages whose effective classification is actionable.
// User classification overrides suppress or add signals without changing the
// plugin annotations that produced them.

// Conventional annotation values that mark a message as actionable. They are
// compared case-insensitively after trimming. The UI's row badges use the same
// lists, so a badge and Needs You always agree.
var (
	NeedsReplyValues = []string{"true", "yes", "1"}
	UrgencyValues    = []string{"high", "urgent", "critical"}
	ImportanceValues = []string{"high"}
)

// Attention signal keys.
const (
	AttentionNeedsReply = "needs_reply"
	AttentionUrgency    = "urgency"
	AttentionImportance = "importance"
)

// Attention is which actionable signals a message has.
type Attention struct {
	NeedsReply bool
	Urgent     bool
	Important  bool
}

// Any reports whether the message qualifies for Needs You.
func (a Attention) Any() bool { return a.NeedsReply || a.Urgent || a.Important }

// AttentionOf derives the signals from a message's annotations, with exactly
// the rules the Needs You query uses.
func AttentionOf(anns []PluginAnnotation) Attention {
	var a Attention
	for _, ann := range anns {
		v := strings.ToLower(strings.TrimSpace(ann.Value))
		switch ann.Key {
		case AttentionNeedsReply:
			a.NeedsReply = a.NeedsReply || contains(NeedsReplyValues, v)
		case AttentionUrgency:
			a.Urgent = a.Urgent || contains(UrgencyValues, v)
		case AttentionImportance:
			a.Important = a.Important || contains(ImportanceValues, v)
		}
	}
	return a
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// signalExists is an EXISTS test for one signal on messages.id. Using EXISTS
// means several plugins agreeing never duplicates rows or weight.
func signalExists(key string, values []string) (string, []any) {
	args := []any{key}
	for _, v := range values {
		args = append(args, v)
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(values)), ",")
	return `EXISTS (SELECT 1 FROM plugin_annotations pa
		WHERE pa.message_id = messages.id AND pa.key = ?
		AND lower(trim(pa.value)) IN (` + placeholders + `))`, args
}

// effectiveSignalExists applies a user override to one plugin-derived signal.
// The same expression is reused for filtering and ranking.
func effectiveSignalExists(key string, values []string, overrideValues []string) (string, []any) {
	pluginExpr, pluginArgs := signalExists(key, values)
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(overrideValues)), ",")
	args := []any{key}
	for _, value := range overrideValues {
		args = append(args, value)
	}
	args = append(args, key)
	args = append(args, pluginArgs...)
	return `(EXISTS (SELECT 1 FROM classification_overrides co
		WHERE co.message_id = messages.id AND co.key = ? AND co.value IN (` + placeholders + `))
		OR (NOT EXISTS (SELECT 1 FROM classification_overrides co
			WHERE co.message_id = messages.id AND co.key = ?)
			AND ` + pluginExpr + `))`, args
}

// needsYouFilter returns the WHERE clause, the ranking expression, and their
// arguments in order: WHERE args first, then ORDER BY args.
func needsYouFilter(unreadOnly bool) (where string, order string, whereArgs, orderArgs []any) {
	urgent, ua := effectiveSignalExists(AttentionUrgency, UrgencyValues, []string{"high"})
	reply, ra := effectiveSignalExists(AttentionNeedsReply, NeedsReplyValues, []string{"true"})
	important, ia := effectiveSignalExists(AttentionImportance, ImportanceValues, []string{"high"})

	where = inboxMailboxPredicate + `
		AND NOT EXISTS (SELECT 1 FROM message_attention_overrides o
			WHERE o.message_id = messages.id AND o.dismissed = 1)
		AND NOT EXISTS (SELECT 1 FROM snoozes s
			WHERE s.target_type = 'message' AND s.target_key = ` + messageKeySQL + `)
		AND (` + urgent + ` OR ` + reply + ` OR ` + important + `)`
	if unreadOnly {
		where += " AND messages.read = 0"
	}
	whereArgs = append(append(append([]any{}, ua...), ra...), ia...)

	// Internal ranking only: urgent 4, needs reply 3, important 2, each
	// counted once however many plugins agree; then newest first.
	order = `(` + urgent + `) * 4 + (` + reply + `) * 3 + (` + important + `) * 2 DESC,
		messages.date DESC, messages.id DESC`
	orderArgs = append(append(append([]any{}, ua...), ra...), ia...)
	return where, order, whereArgs, orderArgs
}

// ListNeedsYou returns actionable, non-dismissed inbox messages across
// accounts, most urgent first. Trash, Sent, Drafts, archives and other
// folders are excluded because only inbox mailboxes are considered.
func (db *DB) ListNeedsYou(unreadOnly bool) ([]Message, error) {
	where, order, whereArgs, orderArgs := needsYouFilter(unreadOnly)
	rows, err := db.Query(`
		SELECT `+messageColumns+`
		FROM messages
		JOIN mailboxes ON mailboxes.id = messages.mailbox_id
		WHERE `+where+`
		ORDER BY `+order, append(whereArgs, orderArgs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

// CountNeedsYou counts actionable, non-dismissed inbox messages, read or not.
func (db *DB) CountNeedsYou() (int, error) {
	where, _, whereArgs, _ := needsYouFilter(false)
	var n int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM messages
		JOIN mailboxes ON mailboxes.id = messages.mailbox_id
		WHERE `+where, whereArgs...).Scan(&n)
	return n, err
}

// SetNeedsYouDismissed dismisses a message from Needs You, or restores it.
// It never touches the message, its flags, or any plugin annotation.
func (db *DB) SetNeedsYouDismissed(messageID int64, dismissed bool) error {
	if messageID <= 0 {
		return errors.New("needs you: message id is required")
	}
	if !dismissed {
		_, err := db.Exec(`DELETE FROM message_attention_overrides WHERE message_id = ?`, messageID)
		return err
	}
	_, err := db.Exec(`
		INSERT INTO message_attention_overrides (message_id, dismissed, updated_at)
		VALUES (?, 1, ?)
		ON CONFLICT(message_id) DO UPDATE SET dismissed = 1, updated_at = excluded.updated_at`,
		messageID, time.Now().Unix())
	return err
}

// DismissedFromNeedsYou returns the IDs of dismissed messages.
func (db *DB) DismissedFromNeedsYou() (map[int64]bool, error) {
	rows, err := db.Query(`SELECT message_id FROM message_attention_overrides WHERE dismissed = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}
