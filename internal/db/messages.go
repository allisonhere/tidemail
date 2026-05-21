package db

import (
	"encoding/json"
	"time"
)

type Message struct {
	ID            int64
	MailboxID     int64
	UID           uint32
	MessageID     string
	Subject       string
	From          string
	To            string
	CC            string
	ReplyTo       string
	Date          time.Time
	BodyText      string
	BodyHTML      string
	Summary       string
	Flags         []string
	Read          bool
	HasAttachment bool
}

func (db *DB) ListMessages(mailboxID int64) ([]Message, error) {
	rows, err := db.Query(`
		SELECT id, mailbox_id, uid, message_id, subject, from_addr, to_addr, cc_addr,
		       reply_to, date, body_text, body_html, summary, flags, read, has_attachment
		FROM messages WHERE mailbox_id = ?
		ORDER BY date DESC, id DESC`, mailboxID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

func (db *DB) ListUnreadMessages(mailboxID int64) ([]Message, error) {
	rows, err := db.Query(`
		SELECT id, mailbox_id, uid, message_id, subject, from_addr, to_addr, cc_addr,
		       reply_to, date, body_text, body_html, summary, flags, read, has_attachment
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
		       messages.subject, messages.from_addr, messages.to_addr, messages.cc_addr,
		       messages.reply_to, messages.date, messages.body_text, messages.body_html,
		       messages.summary, messages.flags, messages.read, messages.has_attachment
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

func (db *DB) SearchMessages(mailboxID int64, query string) ([]Message, error) {
	q := "%" + query + "%"
	rows, err := db.Query(`
		SELECT id, mailbox_id, uid, message_id, subject, from_addr, to_addr, cc_addr,
		       reply_to, date, body_text, body_html, summary, flags, read, has_attachment
		FROM messages WHERE mailbox_id = ? AND (subject LIKE ? OR from_addr LIKE ? OR body_text LIKE ?)
		ORDER BY date DESC, id DESC`, mailboxID, q, q, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

func (db *DB) GetMessage(id int64) (Message, error) {
	rows, err := db.Query(`
		SELECT id, mailbox_id, uid, message_id, subject, from_addr, to_addr, cc_addr,
		       reply_to, date, body_text, body_html, summary, flags, read, has_attachment
		FROM messages WHERE id = ?`, id)
	if err != nil {
		return Message{}, err
	}
	defer rows.Close()
	msgs, err := scanMessages(rows)
	if err != nil || len(msgs) == 0 {
		return Message{}, err
	}
	return msgs[0], nil
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
			(mailbox_id, uid, message_id, subject, from_addr, to_addr, cc_addr,
			 reply_to, date, body_text, body_html, summary, flags, read, has_attachment)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(mailbox_id, uid) DO UPDATE SET
			message_id     = excluded.message_id,
			subject        = excluded.subject,
			from_addr      = excluded.from_addr,
			to_addr        = excluded.to_addr,
			cc_addr        = excluded.cc_addr,
			reply_to       = excluded.reply_to,
			date           = excluded.date,
			body_text      = CASE WHEN excluded.body_text != '' THEN excluded.body_text ELSE body_text END,
			body_html      = CASE WHEN excluded.body_html != '' THEN excluded.body_html ELSE body_html END,
			flags          = excluded.flags,
			read           = excluded.read,
			has_attachment = excluded.has_attachment
	`, m.MailboxID, m.UID, m.MessageID, m.Subject, m.From, m.To, m.CC,
		m.ReplyTo, dateUnix, m.BodyText, m.BodyHTML, m.Summary,
		string(flagsJSON), read, att)
	return err
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
	_, err := db.Exec(`UPDATE messages SET read = 1 WHERE mailbox_id = ?`, mailboxID)
	return err
}

func (db *DB) SaveSummary(id int64, summary string) error {
	_, err := db.Exec(`UPDATE messages SET summary = ? WHERE id = ?`, summary, id)
	return err
}

func (db *DB) DeleteMessage(id int64) error {
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
			&m.ID, &m.MailboxID, &m.UID, &m.MessageID, &m.Subject,
			&m.From, &m.To, &m.CC, &m.ReplyTo, &dateUnix,
			&m.BodyText, &m.BodyHTML, &m.Summary, &flagsJSON, &read, &att,
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
