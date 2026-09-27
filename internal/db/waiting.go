package db

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

// Waiting on Them storage. Qualification itself happens in the UI package,
// on top of TideMail's existing Go-side threading; the database supplies the
// header rows to thread and the user's "Stop waiting" choices.

// ExcludedFromWaiting reports folders whose messages never count toward
// Waiting on Them: Trash, Junk/Spam, and Drafts.
func ExcludedFromWaiting(mb Mailbox) bool {
	for _, flag := range mb.Flags {
		switch strings.ToLower(flag) {
		case `\trash`, `\junk`, `\drafts`:
			return true
		}
	}
	for _, name := range []string{mb.Name, mb.DisplayName} {
		if isCommonTrashMailboxName(name) || isCommonJunkMailboxName(name) || isCommonDraftsMailboxName(name) {
			return true
		}
		// Also match the last path segment for "/" and "." delimited servers
		// ("INBOX.Trash", "[Gmail]/Spam").
		if excludedFolderNames[lastSegment(name)] {
			return true
		}
	}
	return false
}

var excludedFolderNames = map[string]bool{
	"trash": true, "deleted items": true, "deleted messages": true, "bin": true,
	"junk": true, "spam": true, "junk email": true, "junk e-mail": true, "drafts": true,
}

func lastSegment(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	if i := strings.LastIndexAny(n, "/."); i >= 0 {
		n = n[i+1:]
	}
	return strings.TrimSpace(n)
}

func parseFlags(flagsJSON string) []string {
	var flags []string
	_ = json.Unmarshal([]byte(flagsJSON), &flags)
	return flags
}

// IsSentMailbox reports a Sent folder, by flag or common name.
func IsSentMailbox(mb Mailbox) bool {
	for _, flag := range mb.Flags {
		if strings.EqualFold(flag, `\Sent`) {
			return true
		}
	}
	return isCommonSentMailboxName(mb.Name) || isCommonSentMailboxName(mb.DisplayName)
}

// headerColumns are the columns ListWaitingCandidates reads: enough to
// thread and judge conversations, without bodies.
const headerColumns = `messages.id, messages.mailbox_id, messages.uid, messages.message_id,
	messages.in_reply_to, messages.references_text, messages.subject, messages.from_addr,
	messages.to_addr, messages.cc_addr, messages.date, messages.flags, messages.read`

// ListWaitingCandidates returns header-only rows for every cached message
// outside Trash, Junk, and Drafts, across accounts. Bodies, HTML, raw
// headers, and summaries are not loaded.
func (db *DB) ListWaitingCandidates() ([]Message, error) {
	rows, err := db.Query(`SELECT id, account_id, name, display_name, delimiter, flags, unread_count, last_synced FROM mailboxes`)
	if err != nil {
		return nil, err
	}
	var excluded []string
	for rows.Next() {
		var mb Mailbox
		var flagsJSON string
		var lastSynced int64
		if err := rows.Scan(&mb.ID, &mb.AccountID, &mb.Name, &mb.DisplayName, &mb.Delimiter, &flagsJSON, &mb.UnreadCount, &lastSynced); err != nil {
			rows.Close()
			return nil, err
		}
		mb.Flags = parseFlags(flagsJSON)
		if ExcludedFromWaiting(mb) {
			excluded = append(excluded, strconv.FormatInt(mb.ID, 10))
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	where := ""
	if len(excluded) > 0 {
		// IDs are integers from our own table, not user input.
		where = "WHERE messages.mailbox_id NOT IN (" + strings.Join(excluded, ",") + ")"
	}
	rows, err = db.Query(`SELECT ` + headerColumns + ` FROM messages ` + where)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		var flagsJSON string
		var date int64
		var read int
		if err := rows.Scan(&m.ID, &m.MailboxID, &m.UID, &m.MessageID, &m.InReplyTo, &m.References,
			&m.Subject, &m.From, &m.To, &m.CC, &date, &flagsJSON, &read); err != nil {
			return nil, err
		}
		if date != 0 {
			m.Date = time.Unix(date, 0)
		}
		m.Flags = parseFlags(flagsJSON)
		m.Read = read != 0
		out = append(out, m)
	}
	return out, rows.Err()
}

// ListMessagesByIDs returns full message rows for ids, in the given order.
// Missing IDs are skipped.
func (db *DB) ListMessagesByIDs(ids []int64) ([]Message, error) {
	byID := map[int64]Message{}
	for start := 0; start < len(ids); start += annotationBatchSize {
		batch := ids[start:min(start+annotationBatchSize, len(ids))]
		args := make([]any, len(batch))
		for i, id := range batch {
			args[i] = id
		}
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		rows, err := db.Query(`SELECT `+messageColumns+` FROM messages WHERE messages.id IN (`+placeholders+`)`, args...)
		if err != nil {
			return nil, err
		}
		msgs, err := scanMessages(rows)
		rows.Close()
		if err != nil {
			return nil, err
		}
		for _, m := range msgs {
			byID[m.ID] = m
		}
	}
	out := make([]Message, 0, len(ids))
	for _, id := range ids {
		if m, ok := byID[id]; ok {
			out = append(out, m)
		}
	}
	return out, nil
}

// "Stop waiting" is remembered per waiting cycle: the key is the message that
// started the current wait (the user's latest message in the thread; see
// WaitingKey). A reply from someone else ends that wait naturally, and the
// user's next message has a new key, so an old dismissal never hides a new
// wait.

// WaitingKey identifies a waiting cycle by its message: the normalized
// Message-ID, which is shared by every folder copy of the message, or the row
// ID when a message has none.
func WaitingKey(m Message) string {
	id := strings.ToLower(strings.Trim(strings.TrimSpace(m.MessageID), "<>"))
	if id != "" && !strings.ContainsAny(id, " \t\r\n") {
		return "<" + id + ">"
	}
	return "row:" + strconv.FormatInt(m.ID, 10)
}

// SetWaitingStopped records or clears "Stop waiting" for waiting cycles.
func (db *DB) SetWaitingStopped(keys []string, stopped bool) (err error) {
	for _, k := range keys {
		if strings.TrimSpace(k) == "" {
			return errors.New("waiting: empty key")
		}
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
	now := time.Now().Unix()
	for _, k := range keys {
		if stopped {
			_, err = tx.Exec(`INSERT INTO waiting_dismissals (message_key, dismissed_at) VALUES (?, ?)
				ON CONFLICT(message_key) DO UPDATE SET dismissed_at = excluded.dismissed_at`, k, now)
		} else {
			_, err = tx.Exec(`DELETE FROM waiting_dismissals WHERE message_key = ?`, k)
		}
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// WaitingStopped returns the keys of stopped waiting cycles.
func (db *DB) WaitingStopped() (map[string]bool, error) {
	rows, err := db.Query(`SELECT message_key FROM waiting_dismissals`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out[k] = true
	}
	return out, rows.Err()
}

// PruneWaitingDismissals drops dismissals whose message is no longer cached.
// It runs at startup; a stale row is harmless in the meantime.
func (db *DB) PruneWaitingDismissals() error {
	_, err := db.Exec(`
		DELETE FROM waiting_dismissals
		WHERE (message_key LIKE 'row:%'
		       AND CAST(substr(message_key, 5) AS INTEGER) NOT IN (SELECT id FROM messages))
		   OR (message_key LIKE '<%'
		       AND message_key NOT IN (SELECT '<' || lower(trim(trim(message_id), '<>')) || '>' FROM messages WHERE message_id != ''))`)
	return err
}
