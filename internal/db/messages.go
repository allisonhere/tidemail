package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Message struct {
	ID             int64
	MailboxID      int64
	UID            uint32
	MessageID      string
	InReplyTo      string
	References     string
	Subject        string
	From           string
	To             string
	CC             string
	ReplyTo        string
	Date           time.Time
	BodyText       string
	BodyHTML       string
	Summary        string
	Flags          []string
	Read           bool
	HasAttachment  bool
	Headers        string       // auth-related headers parsed from MIME
	AttachmentData []Attachment // transient, not stored in messages table
	AccountName    string       // transient, populated for cross-mailbox search results
	MailboxName    string       // transient, populated for cross-mailbox search results
}

func (db *DB) ListMessages(mailboxID int64) ([]Message, error) {
	rows, err := db.Query(`
		SELECT id, mailbox_id, uid, message_id, in_reply_to, references_text, subject, from_addr, to_addr, cc_addr,
		       reply_to, date, body_text, body_html, summary,		flags, read, has_attachment, headers
		FROM messages WHERE mailbox_id = ?
		ORDER BY date DESC, id DESC`, mailboxID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

func (db *DB) ListMessagesUnreadFirst(mailboxID int64) ([]Message, error) {
	rows, err := db.Query(`
		SELECT id, mailbox_id, uid, message_id, in_reply_to, references_text, subject, from_addr, to_addr, cc_addr,
		       reply_to, date, body_text, body_html, summary,		flags, read, has_attachment, headers
		FROM messages WHERE mailbox_id = ?
		ORDER BY read ASC, date DESC, id DESC`, mailboxID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

func (db *DB) CountMessages(mailboxID int64) (int64, error) {
	var n int64
	err := db.QueryRow(`SELECT COUNT(*) FROM messages WHERE mailbox_id = ?`, mailboxID).Scan(&n)
	return n, err
}

func (db *DB) ListUnreadMessages(mailboxID int64) ([]Message, error) {
	rows, err := db.Query(`
		SELECT id, mailbox_id, uid, message_id, in_reply_to, references_text, subject, from_addr, to_addr, cc_addr,
		       reply_to, date, body_text, body_html, summary,		flags, read, has_attachment, headers
		FROM messages WHERE mailbox_id = ? AND read = 0
		ORDER BY date DESC, id DESC`, mailboxID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

func (db *DB) ListUnifiedInbox(unreadOnly bool) ([]Message, error) {
	readClause := ""
	if unreadOnly {
		readClause = " AND messages.read = 0"
	}
	rows, err := db.Query(`
		SELECT messages.id, messages.mailbox_id, messages.uid, messages.message_id,
		       messages.in_reply_to, messages.references_text, messages.subject, messages.from_addr, messages.to_addr, messages.cc_addr,
		       messages.reply_to, messages.date, messages.body_text, messages.body_html,
		       messages.summary, messages.flags, messages.read, messages.has_attachment, messages.headers
		FROM messages
		JOIN mailboxes ON mailboxes.id = messages.mailbox_id
		WHERE (
			lower(mailboxes.name) = 'inbox'
			OR lower(mailboxes.display_name) = 'inbox'
			OR instr(lower(mailboxes.flags), '\inbox') > 0
		)` + readClause + `
		ORDER BY messages.date DESC, messages.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

func (db *DB) ListUnifiedInboxUnreadFirst(unreadOnly bool) ([]Message, error) {
	readClause := ""
	if unreadOnly {
		readClause = " AND messages.read = 0"
	}
	rows, err := db.Query(`
		SELECT messages.id, messages.mailbox_id, messages.uid, messages.message_id,
		       messages.in_reply_to, messages.references_text, messages.subject, messages.from_addr, messages.to_addr, messages.cc_addr,
		       messages.reply_to, messages.date, messages.body_text, messages.body_html,
		       messages.summary, messages.flags, messages.read, messages.has_attachment, messages.headers
		FROM messages
		JOIN mailboxes ON mailboxes.id = messages.mailbox_id
		WHERE (
			lower(mailboxes.name) = 'inbox'
			OR lower(mailboxes.display_name) = 'inbox'
			OR instr(lower(mailboxes.flags), '\inbox') > 0
		)` + readClause + `
		ORDER BY messages.read ASC, messages.date DESC, messages.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

func (db *DB) SearchMessages(mailboxID int64, query string) ([]Message, error) {
	q := "%" + query + "%"
	rows, err := db.Query(`
		SELECT id, mailbox_id, uid, message_id, in_reply_to, references_text, subject, from_addr, to_addr, cc_addr,
		       reply_to, date, body_text, body_html, summary,		flags, read, has_attachment, headers
		FROM messages WHERE mailbox_id = ? AND (subject LIKE ? OR from_addr LIKE ? OR body_text LIKE ?)
		ORDER BY date DESC, id DESC`, mailboxID, q, q, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

func (db *DB) SearchAllMessages(query string, unreadFirst bool) ([]Message, error) {
	match := ftsQuery(query)
	if match == "" {
		return nil, nil
	}
	orderBy := "bm25(messages_fts), messages.date DESC, messages.id DESC"
	if unreadFirst {
		orderBy = "messages.read ASC, bm25(messages_fts), messages.date DESC, messages.id DESC"
	}
	rows, err := db.Query(`
		SELECT messages.id, messages.mailbox_id, messages.uid, messages.message_id,
		       messages.in_reply_to, messages.references_text, messages.subject, messages.from_addr, messages.to_addr, messages.cc_addr,
		       messages.reply_to, messages.date, messages.body_text, messages.body_html, messages.summary,
		       messages.flags, messages.read, messages.has_attachment, messages.headers,
		       accounts.name, COALESCE(NULLIF(mailboxes.display_name, ''), mailboxes.name)
		FROM messages_fts
		JOIN messages ON messages.id = messages_fts.rowid
		JOIN mailboxes ON mailboxes.id = messages.mailbox_id
		JOIN accounts ON accounts.id = mailboxes.account_id
		WHERE messages_fts MATCH ?
		ORDER BY `+orderBy, match)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessagesWithContext(rows)
}

func (db *DB) GetMessage(id int64) (Message, error) {
	var m Message
	var flagsJSON string
	var dateUnix int64
	var read, att int
	err := db.QueryRow(`
		SELECT id, mailbox_id, uid, message_id, in_reply_to, references_text, subject, from_addr, to_addr, cc_addr,
		       reply_to, date, body_text, body_html, summary,		flags, read, has_attachment, headers
		FROM messages WHERE id = ?`, id).
		Scan(&m.ID, &m.MailboxID, &m.UID, &m.MessageID, &m.InReplyTo, &m.References, &m.Subject,
			&m.From, &m.To, &m.CC, &m.ReplyTo, &dateUnix,
			&m.BodyText, &m.BodyHTML, &m.Summary, &flagsJSON, &read, &att,
			&m.Headers)
	if err != nil {
		return Message{}, err
	}
	json.Unmarshal([]byte(flagsJSON), &m.Flags) //nolint:errcheck
	if dateUnix > 0 {
		m.Date = time.Unix(dateUnix, 0)
	}
	m.Read = read != 0
	m.HasAttachment = att != 0
	return m, nil
}

func (db *DB) UpsertMessage(m Message) error {
	flagsJSON, _ := json.Marshal(m.Flags)
	read := 0
	if m.Read {
		read = 1
	}
	att := 0
	if m.HasAttachment {
		att = 1
	}
	var dateUnix int64
	if !m.Date.IsZero() {
		dateUnix = m.Date.Unix()
	}
	_, err := db.Exec(`
		INSERT INTO messages
			(mailbox_id, uid, message_id, in_reply_to, references_text, subject, from_addr, to_addr, cc_addr,
			 reply_to, date, body_text, body_html, summary, flags, read, has_attachment, headers)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(mailbox_id, uid) DO UPDATE SET
			message_id     = excluded.message_id,
			in_reply_to    = excluded.in_reply_to,
			references_text = excluded.references_text,
			subject        = excluded.subject,
			from_addr      = excluded.from_addr,
			to_addr        = excluded.to_addr,
			cc_addr        = excluded.cc_addr,
			reply_to       = excluded.reply_to,
			date           = excluded.date,
			body_text      = CASE WHEN excluded.body_text != '' THEN excluded.body_text ELSE body_text END,
			body_html      = CASE WHEN excluded.body_html != '' THEN excluded.body_html ELSE body_html END,
			flags          = excluded.flags,
			has_attachment = excluded.has_attachment,
			headers        = CASE WHEN excluded.headers != '' THEN excluded.headers ELSE headers END
	`, m.MailboxID, m.UID, m.MessageID, m.InReplyTo, m.References, m.Subject, m.From, m.To, m.CC,
		m.ReplyTo, dateUnix, m.BodyText, m.BodyHTML, m.Summary,
		string(flagsJSON), read, att, m.Headers)
	if err != nil {
		return err
	}

	msgID := m.ID
	if msgID == 0 {
		if err := db.QueryRow(`SELECT id FROM messages WHERE mailbox_id = ? AND uid = ?`, m.MailboxID, m.UID).Scan(&msgID); err != nil {
			return err
		}
	}
	if err := db.upsertMessageFTS(msgID); err != nil {
		return err
	}

	// Save attachments if any
	if len(m.AttachmentData) > 0 {
		// Get the message ID (works with INSERT or ON CONFLICT UPDATE)
		if msgID != 0 {
			// Delete old attachments and re-insert
			if delErr := db.DeleteAttachmentsForMessage(msgID); delErr != nil {
				return delErr
			}
			for _, a := range m.AttachmentData {
				if _, err := db.SaveAttachment(msgID, a); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (db *DB) MessageDeletedLocally(mailboxID int64, uid uint32, messageID string) (bool, error) {
	var n int
	messageID = strings.TrimSpace(messageID)
	if messageID != "" {
		err := db.QueryRow(`
			SELECT COUNT(1)
			FROM deleted_messages
			WHERE mailbox_id = ?
			  AND message_id != ''
			  AND message_id = ?`,
			mailboxID, messageID).Scan(&n)
		return n > 0, err
	}
	err := db.QueryRow(`
		SELECT COUNT(1)
		FROM deleted_messages
		WHERE mailbox_id = ?
		  AND uid != 0
		  AND uid = ?
		  AND message_id = ''`,
		mailboxID, uid).Scan(&n)
	return n > 0, err
}

func (db *DB) PruneDeletedMessageTombstones() error {
	cutoff := time.Now().Add(-90 * 24 * time.Hour).Unix()
	_, err := db.Exec(`DELETE FROM deleted_messages WHERE deleted_at != 0 AND deleted_at < ?`, cutoff)
	return err
}

// MessageExists reports whether a message with the given mailbox/uid is already
// stored. Used to distinguish genuinely-new mail from re-fetches: IMAP SINCE is
// date-granular, so a poll re-downloads messages we already hold.
func (db *DB) MessageExists(mailboxID int64, uid uint32) (bool, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(1) FROM messages WHERE mailbox_id = ? AND uid = ?`, mailboxID, uid).Scan(&n)
	return n > 0, err
}

func (db *DB) MarkRead(id int64, read bool) error {
	v := 0
	if read {
		v = 1
	}
	_, err := db.Exec(`UPDATE messages SET read = ? WHERE id = ?`, v, id)
	return err
}

func (db *DB) MarkAllRead(mailboxID int64) error {
	_, err := db.Exec(`UPDATE messages SET read = 1 WHERE mailbox_id = ? AND read = 0`, mailboxID)
	return err
}

func (db *DB) SaveSummary(id int64, summary string) error {
	_, err := db.Exec(`UPDATE messages SET summary = ? WHERE id = ?`, summary, id)
	return err
}

func (db *DB) DeleteMessage(id int64) error {
	var mailboxID int64
	var uid uint32
	var messageID string
	if err := db.QueryRow(`SELECT mailbox_id, uid, message_id FROM messages WHERE id = ?`, id).Scan(&mailboxID, &uid, &messageID); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("delete message: message %d not found", id)
		}
		return fmt.Errorf("delete message lookup: %w", err)
	}
	if mailboxID != 0 && (uid != 0 || messageID != "") {
		_, err := db.Exec(`
			INSERT OR IGNORE INTO deleted_messages (mailbox_id, uid, message_id, deleted_at)
			VALUES (?, ?, ?, ?)`,
			mailboxID, uid, messageID, time.Now().Unix())
		if err != nil {
			return err
		}
	}
	_ = db.DeleteAttachmentsForMessage(id)
	if err := db.deleteMessageFTS(id); err != nil {
		return err
	}
	_, err := db.Exec(`DELETE FROM messages WHERE id = ?`, id)
	return err
}

func (db *DB) MoveMessage(id, mailboxID int64) error {
	_, err := db.Exec(`UPDATE messages SET mailbox_id = ? WHERE id = ?`, mailboxID, id)
	return err
}

func (db *DB) CountUnread(mailboxID int64) (int64, error) {
	var n int64
	err := db.QueryRow(`SELECT COUNT(*) FROM messages WHERE mailbox_id = ? AND read = 0`, mailboxID).Scan(&n)
	return n, err
}

func scanMessagesWithContext(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]Message, error) {
	var msgs []Message
	for rows.Next() {
		var m Message
		var flagsJSON string
		var dateUnix int64
		var read, att int
		if err := rows.Scan(
			&m.ID, &m.MailboxID, &m.UID, &m.MessageID, &m.InReplyTo, &m.References, &m.Subject,
			&m.From, &m.To, &m.CC, &m.ReplyTo, &dateUnix,
			&m.BodyText, &m.BodyHTML, &m.Summary, &flagsJSON, &read, &att,
			&m.Headers, &m.AccountName, &m.MailboxName,
		); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(flagsJSON), &m.Flags) //nolint:errcheck
		if dateUnix > 0 {
			m.Date = time.Unix(dateUnix, 0)
		}
		m.Read = read != 0
		m.HasAttachment = att != 0
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

func ftsQuery(query string) string {
	parts := strings.Fields(strings.TrimSpace(query))
	if len(parts) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.ReplaceAll(part, `"`, `""`)
		if part == "" {
			continue
		}
		quoted = append(quoted, `"`+part+`"`)
	}
	return strings.Join(quoted, " AND ")
}

func (db *DB) upsertMessageFTS(id int64) error {
	if _, err := db.Exec(`DELETE FROM messages_fts WHERE rowid = ?`, id); err != nil {
		return err
	}
	_, err := db.Exec(`
		INSERT INTO messages_fts(rowid, subject, from_addr, to_addr, cc_addr, body_text)
		SELECT id, subject, from_addr, to_addr, cc_addr, body_text
		FROM messages
		WHERE id = ?
	`, id)
	return err
}

func (db *DB) deleteMessageFTS(id int64) error {
	_, err := db.Exec(`DELETE FROM messages_fts WHERE rowid = ?`, id)
	return err
}

func scanMessages(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]Message, error) {
	var msgs []Message
	for rows.Next() {
		var m Message
		var flagsJSON string
		var dateUnix int64
		var read, att int
		if err := rows.Scan(
			&m.ID, &m.MailboxID, &m.UID, &m.MessageID, &m.InReplyTo, &m.References, &m.Subject,
			&m.From, &m.To, &m.CC, &m.ReplyTo, &dateUnix,
			&m.BodyText, &m.BodyHTML, &m.Summary, &flagsJSON, &read, &att,
			&m.Headers,
		); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(flagsJSON), &m.Flags) //nolint:errcheck
		if dateUnix > 0 {
			m.Date = time.Unix(dateUnix, 0)
		}
		m.Read = read != 0
		m.HasAttachment = att != 0
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// ListAddresses returns all unique email addresses seen in From, To, and CC
// fields across all messages, suitable for compose autocomplete.
func (db *DB) ListAddresses() ([]string, error) {
	rows, err := db.Query(`
		SELECT DISTINCT addr FROM (
			SELECT from_addr AS addr FROM messages WHERE from_addr != ''
			UNION
			SELECT to_addr AS addr FROM messages WHERE to_addr != ''
			UNION
			SELECT cc_addr AS addr FROM messages WHERE cc_addr != ''
		) ORDER BY addr`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var addrs []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			return addrs, err
		}
		// Split comma-separated addresses
		for _, part := range strings.Split(a, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				addrs = append(addrs, part)
			}
		}
	}
	return addrs, rows.Err()
}
