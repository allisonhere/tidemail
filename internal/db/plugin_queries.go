package db

import (
	"strconv"
	"strings"
	"time"
)

// Read-only helpers for plugin queries (internal/pluginquery). They load
// header columns only: bodies, HTML, raw headers, and summaries are never
// selected.

// MailboxInfo is a mailbox with its account's display name.
type MailboxInfo struct {
	Mailbox
	AccountName string
}

// ListAllMailboxes returns every mailbox across accounts.
func (db *DB) ListAllMailboxes() ([]MailboxInfo, error) {
	rows, err := db.Query(`
		SELECT mailboxes.id, mailboxes.account_id, mailboxes.name, mailboxes.display_name,
		       mailboxes.delimiter, mailboxes.flags, accounts.name
		FROM mailboxes JOIN accounts ON accounts.id = mailboxes.account_id
		ORDER BY accounts.position, accounts.id, mailboxes.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MailboxInfo
	for rows.Next() {
		var mb MailboxInfo
		var flagsJSON string
		if err := rows.Scan(&mb.ID, &mb.AccountID, &mb.Name, &mb.DisplayName, &mb.Delimiter, &flagsJSON, &mb.AccountName); err != nil {
			return nil, err
		}
		mb.Flags = parseFlags(flagsJSON)
		out = append(out, mb)
	}
	return out, rows.Err()
}

// IsInboxMailbox is inboxMailboxPredicate for a loaded mailbox.
func IsInboxMailbox(mb Mailbox) bool {
	if strings.EqualFold(mb.Name, "inbox") || strings.EqualFold(mb.DisplayName, "inbox") {
		return true
	}
	for _, f := range mb.Flags {
		if strings.Contains(strings.ToLower(f), `\inbox`) {
			return true
		}
	}
	return false
}

// HeaderQuery selects header rows. Zero values mean "no restriction", except
// MailboxIDs: nil means every mailbox, an empty non-nil slice means none.
type HeaderQuery struct {
	MailboxIDs []int64
	// IDs restricts the rows to these message row IDs when non-nil.
	IDs           []int64
	Read          *bool
	Starred       *bool
	HasAttachment *bool
	// DateFrom and DateTo bound the date to [DateFrom, DateTo).
	DateFrom, DateTo time.Time
	Ascending        bool
	// After continues a date/id keyset: rows strictly after (AfterDate,
	// AfterID) in the query's order. Used only when AfterID > 0.
	AfterDate int64
	AfterID   int64
	// Limit caps the rows; 0 means no limit.
	Limit int
}

// headerQueryColumns are headerColumns plus the flags plugins may request.
const headerQueryColumns = headerColumns + `, messages.reply_to, messages.starred, messages.has_attachment`

// ListHeaders returns header-only rows matching q, ordered by date then ID.
func (db *DB) ListHeaders(q HeaderQuery) ([]Message, error) {
	if (q.MailboxIDs != nil && len(q.MailboxIDs) == 0) || (q.IDs != nil && len(q.IDs) == 0) {
		return nil, nil
	}
	var where []string
	var args []any
	if q.MailboxIDs != nil {
		// Integers from our own table, not user input. The unary + keeps
		// SQLite from choosing the mailbox index here, so date-ordered pages
		// walk idx_messages_date and stop at LIMIT instead of sorting every
		// matching row.
		where = append(where, "+messages.mailbox_id IN ("+joinInts(q.MailboxIDs)+")")
	}
	if q.IDs != nil {
		// Parsed integers, never strings from the plugin.
		where = append(where, "messages.id IN ("+joinInts(q.IDs)+")")
	}
	for _, f := range []struct {
		col string
		v   *bool
	}{{"messages.read", q.Read}, {"messages.starred", q.Starred}, {"messages.has_attachment", q.HasAttachment}} {
		if f.v != nil {
			where = append(where, f.col+" = ?")
			args = append(args, boolInt(*f.v))
		}
	}
	if !q.DateFrom.IsZero() {
		where = append(where, "messages.date >= ?")
		args = append(args, q.DateFrom.Unix())
	}
	if !q.DateTo.IsZero() {
		where = append(where, "messages.date < ?")
		args = append(args, q.DateTo.Unix())
	}
	order := "messages.date DESC, messages.id DESC"
	if q.Ascending {
		order = "messages.date ASC, messages.id ASC"
	}
	if q.AfterID > 0 {
		if q.Ascending {
			where = append(where, "(messages.date > ? OR (messages.date = ? AND messages.id > ?))")
		} else {
			where = append(where, "(messages.date < ? OR (messages.date = ? AND messages.id < ?))")
		}
		args = append(args, q.AfterDate, q.AfterDate, q.AfterID)
	}
	query := `SELECT ` + headerQueryColumns + ` FROM messages`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY " + order
	if q.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, q.Limit)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		var flagsJSON string
		var date int64
		var read, starred, attach int
		if err := rows.Scan(&m.ID, &m.MailboxID, &m.UID, &m.MessageID, &m.InReplyTo, &m.References,
			&m.Subject, &m.From, &m.To, &m.CC, &date, &flagsJSON, &read, &m.ReplyTo, &starred, &attach); err != nil {
			return nil, err
		}
		if date != 0 {
			m.Date = time.Unix(date, 0)
		}
		m.Flags = parseFlags(flagsJSON)
		m.Read, m.Starred, m.HasAttachment = read != 0, starred != 0, attach != 0
		out = append(out, m)
	}
	return out, rows.Err()
}

// NeedsYouIDs returns the row IDs currently in Needs You.
func (db *DB) NeedsYouIDs() (map[int64]bool, error) {
	where, _, whereArgs, _ := needsYouFilter(false)
	rows, err := db.Query(`
		SELECT messages.id FROM messages
		JOIN mailboxes ON mailboxes.id = messages.mailbox_id
		WHERE `+where, whereArgs...)
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

// CountCorrectedMessages counts messages with at least one classification
// override.
func (db *DB) CountCorrectedMessages() (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(DISTINCT message_id) FROM classification_overrides`).Scan(&n)
	return n, err
}

func joinInts(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(parts, ",")
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
