package db

import (
	"database/sql"
	"fmt"
	"time"
)

type Draft struct {
	ID              int64
	AccountName     string
	AccountUser     string
	AccountIndex    int
	MailboxID       int64
	RemoteUID       uint32
	RemoteMessageID string
	To              string
	CC              string
	Subject         string
	BodyText        string
	InReplyTo       string
	References      string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	LastRemoteSync  time.Time
	Dirty           bool
	Attachments     []DraftAttachment
}

type DraftAttachment struct {
	ID          int64
	DraftID     int64
	Filename    string
	Path        string
	ContentType string
	Data        []byte
	Size        int64
	Position    int
}

func (db *DB) SaveDraft(d Draft) (int64, error) {
	now := time.Now()
	if d.UpdatedAt.IsZero() {
		d.UpdatedAt = now
	}
	dirty := 0
	if d.Dirty {
		dirty = 1
	}
	createdAt := int64(0)
	if !d.CreatedAt.IsZero() {
		createdAt = d.CreatedAt.Unix()
	}
	updatedAt := d.UpdatedAt.Unix()
	lastRemoteSync := int64(0)
	if !d.LastRemoteSync.IsZero() {
		lastRemoteSync = d.LastRemoteSync.Unix()
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck

	id := d.ID
	if id == 0 {
		if createdAt == 0 {
			createdAt = now.Unix()
		}
		res, err := tx.Exec(`
			INSERT INTO drafts
				(account_name, account_user, account_index, mailbox_id, remote_uid, remote_message_id,
				 to_addr, cc_addr, subject, body_text, in_reply_to, references_text,
				 created_at, updated_at, last_remote_sync, dirty)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			d.AccountName, d.AccountUser, d.AccountIndex, d.MailboxID, d.RemoteUID, d.RemoteMessageID,
			d.To, d.CC, d.Subject, d.BodyText, d.InReplyTo, d.References,
			createdAt, updatedAt, lastRemoteSync, dirty)
		if err != nil {
			return 0, err
		}
		id, err = res.LastInsertId()
		if err != nil {
			return 0, err
		}
	} else {
		// Zero-valued linkage fields keep their stored values: compose autosaves
		// rebuild the draft from screen state and don't carry mailbox/remote
		// linkage or creation time — overwriting those would orphan a mirrored
		// remote draft and re-import it as a duplicate on the next sync.
		if _, err := tx.Exec(`
			UPDATE drafts SET
				account_name = ?, account_user = ?, account_index = ?,
				mailbox_id = CASE WHEN ? != 0 THEN ? ELSE mailbox_id END,
				remote_uid = CASE WHEN ? != 0 THEN ? ELSE remote_uid END,
				remote_message_id = CASE WHEN ? != '' THEN ? ELSE remote_message_id END,
				to_addr = ?, cc_addr = ?,
				subject = ?, body_text = ?, in_reply_to = ?, references_text = ?,
				created_at = CASE WHEN ? != 0 THEN ? ELSE created_at END,
				updated_at = ?,
				last_remote_sync = CASE WHEN ? != 0 THEN ? ELSE last_remote_sync END,
				dirty = ?
			WHERE id = ?`,
			d.AccountName, d.AccountUser, d.AccountIndex,
			d.MailboxID, d.MailboxID,
			d.RemoteUID, d.RemoteUID,
			d.RemoteMessageID, d.RemoteMessageID,
			d.To, d.CC,
			d.Subject, d.BodyText, d.InReplyTo, d.References,
			createdAt, createdAt,
			updatedAt,
			lastRemoteSync, lastRemoteSync,
			dirty, id); err != nil {
			return 0, err
		}
	}

	if _, err := tx.Exec(`DELETE FROM draft_attachments WHERE draft_id = ?`, id); err != nil {
		return 0, err
	}
	for i, a := range d.Attachments {
		pos := a.Position
		if pos == 0 {
			pos = i
		}
		size := a.Size
		if size == 0 {
			size = int64(len(a.Data))
		}
		if _, err := tx.Exec(`
			INSERT INTO draft_attachments
				(draft_id, filename, path, content_type, data, size, position)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id, a.Filename, a.Path, a.ContentType, a.Data, size, pos); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func (db *DB) GetDraft(id int64) (Draft, error) {
	row := db.QueryRow(`
		SELECT id, account_name, account_user, account_index, mailbox_id, remote_uid, remote_message_id,
		       to_addr, cc_addr, subject, body_text, in_reply_to, references_text,
		       created_at, updated_at, last_remote_sync, dirty
		FROM drafts WHERE id = ?`, id)
	d, err := scanDraft(row)
	if err != nil {
		return Draft{}, err
	}
	attachments, err := db.ListDraftAttachments(id)
	if err != nil {
		return Draft{}, err
	}
	d.Attachments = attachments
	return d, nil
}

func (db *DB) ListDrafts(accountName, accountUser string) ([]Draft, error) {
	rows, err := db.Query(`
		SELECT id, account_name, account_user, account_index, mailbox_id, remote_uid, remote_message_id,
		       to_addr, cc_addr, subject, body_text, in_reply_to, references_text,
		       created_at, updated_at, last_remote_sync, dirty
		FROM drafts
		WHERE account_name = ? AND account_user = ?
		ORDER BY updated_at DESC, id DESC`, accountName, accountUser)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var drafts []Draft
	for rows.Next() {
		d, err := scanDraft(rows)
		if err != nil {
			return nil, err
		}
		drafts = append(drafts, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range drafts {
		attachments, err := db.ListDraftAttachments(drafts[i].ID)
		if err != nil {
			return nil, err
		}
		drafts[i].Attachments = attachments
	}
	return drafts, nil
}

func (db *DB) ListDraftAttachments(draftID int64) ([]DraftAttachment, error) {
	rows, err := db.Query(`
		SELECT id, draft_id, filename, path, content_type, data, size, position
		FROM draft_attachments WHERE draft_id = ? ORDER BY position, id`, draftID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DraftAttachment
	for rows.Next() {
		var a DraftAttachment
		if err := rows.Scan(&a.ID, &a.DraftID, &a.Filename, &a.Path, &a.ContentType, &a.Data, &a.Size, &a.Position); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (db *DB) DeleteDraft(id int64) error {
	_, err := db.Exec(`DELETE FROM drafts WHERE id = ?`, id)
	return err
}

func (db *DB) MarkDraftRemoteSynced(id int64, mailboxID int64, uid uint32, messageID string) error {
	_, err := db.Exec(`
		UPDATE drafts
		SET mailbox_id = ?, remote_uid = ?, remote_message_id = ?, last_remote_sync = ?, dirty = 0
		WHERE id = ?`, mailboxID, uid, messageID, time.Now().Unix(), id)
	return err
}

func (db *DB) DraftCount(accountName, accountUser string) (int64, error) {
	var n int64
	err := db.QueryRow(`SELECT COUNT(*) FROM drafts WHERE account_name = ? AND account_user = ?`, accountName, accountUser).Scan(&n)
	return n, err
}

// HasDraftForRemote reports whether a remote draft message is already mirrored
// in the drafts table, matched by Message-ID when available, else mailbox+UID.
func (db *DB) HasDraftForRemote(remoteMessageID string, mailboxID int64, uid uint32) (bool, error) {
	var n int
	err := db.QueryRow(`
		SELECT COUNT(1) FROM drafts
		WHERE (? != '' AND remote_message_id = ?)
		   OR (mailbox_id = ? AND remote_uid != 0 AND remote_uid = ?)`,
		remoteMessageID, remoteMessageID, mailboxID, uid).Scan(&n)
	return n > 0, err
}

// UnmirroredDraftMessageCount counts messages in a drafts mailbox that have not
// been imported into the drafts table yet, so sidebar counts stay accurate
// without double-counting mirrored drafts.
func (db *DB) UnmirroredDraftMessageCount(mailboxID int64) (int64, error) {
	var n int64
	err := db.QueryRow(`
		SELECT COUNT(1) FROM messages m
		WHERE m.mailbox_id = ?
		  AND NOT EXISTS (
			SELECT 1 FROM drafts d
			WHERE (d.remote_message_id != '' AND d.remote_message_id = m.message_id)
			   OR (d.mailbox_id = m.mailbox_id AND d.remote_uid != 0 AND d.remote_uid = m.uid)
		  )`, mailboxID).Scan(&n)
	return n, err
}

// DeleteMessageByUID removes the messages-table mirror of a remote draft (with
// its resync tombstone, via DeleteMessage). No-op when no such row exists.
func (db *DB) DeleteMessageByUID(mailboxID int64, uid uint32) error {
	var id int64
	err := db.QueryRow(`SELECT id FROM messages WHERE mailbox_id = ? AND uid = ?`, mailboxID, uid).Scan(&id)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	return db.DeleteMessage(id)
}

type draftScanner interface {
	Scan(dest ...any) error
}

func scanDraft(row draftScanner) (Draft, error) {
	var d Draft
	var createdAt, updatedAt, lastRemoteSync int64
	var dirty int
	if err := row.Scan(
		&d.ID, &d.AccountName, &d.AccountUser, &d.AccountIndex, &d.MailboxID, &d.RemoteUID, &d.RemoteMessageID,
		&d.To, &d.CC, &d.Subject, &d.BodyText, &d.InReplyTo, &d.References,
		&createdAt, &updatedAt, &lastRemoteSync, &dirty,
	); err != nil {
		if err == sql.ErrNoRows {
			return Draft{}, fmt.Errorf("draft not found")
		}
		return Draft{}, err
	}
	if createdAt > 0 {
		d.CreatedAt = time.Unix(createdAt, 0)
	}
	if updatedAt > 0 {
		d.UpdatedAt = time.Unix(updatedAt, 0)
	}
	if lastRemoteSync > 0 {
		d.LastRemoteSync = time.Unix(lastRemoteSync, 0)
	}
	d.Dirty = dirty != 0
	return d, nil
}
