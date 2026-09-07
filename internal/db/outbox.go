package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	OutboxQueued    = "queued"
	OutboxSending   = "sending"
	OutboxFailed    = "failed"
	OutboxSent      = "sent"
	OutboxUncertain = "uncertain"
)

var ErrOutboxBusy = errors.New("message is sending or no longer available")

type OutboxItem struct {
	ID                       int64
	AccountName, AccountUser string
	DraftID                  int64
	Subject, Recipients      string
	MessageJSON, DraftJSON   []byte
	State                    string
	Attempts, MaxAttempts    int
	NextAttempt              int64
	LastError                string
	CreatedAt, UpdatedAt     int64
}

// Only message content and account identity are persisted, never credentials.
func (db *DB) EnqueueOutbox(item OutboxItem) (int64, error) {
	now := time.Now().Unix()
	res, err := db.Exec(`INSERT INTO outbox
  (account_name, account_user, draft_id, subject, recipients, message_json, draft_json,
   max_attempts, next_attempt, created_at, updated_at)
  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.AccountName, item.AccountUser, item.DraftID, item.Subject, item.Recipients,
		item.MessageJSON, item.DraftJSON, max(1, item.MaxAttempts), item.NextAttempt, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

const outboxColumns = `id, account_name, account_user, draft_id, subject, recipients,
 state, attempts, max_attempts, next_attempt, last_error, created_at, updated_at`

func scanOutbox(row interface{ Scan(...any) error }, payload bool) (OutboxItem, error) {
	var item OutboxItem
	dest := []any{&item.ID, &item.AccountName, &item.AccountUser, &item.DraftID, &item.Subject, &item.Recipients,
		&item.State, &item.Attempts, &item.MaxAttempts, &item.NextAttempt, &item.LastError, &item.CreatedAt, &item.UpdatedAt}
	if payload {
		dest = append(dest, &item.MessageJSON, &item.DraftJSON)
	}
	err := row.Scan(dest...)
	return item, err
}

func (db *DB) ListOutbox() ([]OutboxItem, error) {
	rows, err := db.Query(`SELECT ` + outboxColumns + ` FROM outbox ORDER BY CASE WHEN state='sent' THEN 1 ELSE 0 END, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []OutboxItem
	for rows.Next() {
		item, err := scanOutbox(rows, false)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (db *DB) GetOutbox(id int64) (OutboxItem, error) {
	return scanOutbox(db.QueryRow(`SELECT `+outboxColumns+`, message_json, draft_json FROM outbox WHERE id=?`, id), true)
}

// Claim before SMTP begins. A stale timer, canceled item, or second worker
// cannot send the same queue entry concurrently.
func (db *DB) ClaimOutbox(id int64) (bool, error) {
	res, err := db.Exec(`UPDATE outbox SET state='sending', attempts=attempts+1, updated_at=?
  WHERE id=? AND state='queued' AND attempts < max_attempts`, time.Now().Unix(), id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (db *DB) FinishOutbox(id int64, state, message string, next int64) error {
	res, err := db.Exec(`UPDATE outbox SET state=?, last_error=?, next_attempt=?, updated_at=?,
        message_json=CASE WHEN ?='sent' THEN x'' ELSE message_json END,
        draft_json=CASE WHEN ?='sent' THEN x'' ELSE draft_json END
        WHERE id=? AND state='sending'`,
		state, message, next, time.Now().Unix(), state, state, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrOutboxBusy
	}
	return nil
}

// An interrupted SMTP exchange may already have delivered the message. Require
// an explicit retry after restart instead of silently risking a duplicate.
func (db *DB) RecoverOutbox() error {
	_, err := db.Exec(`UPDATE outbox SET state='uncertain', last_error='Delivery was interrupted. Check Sent mail before retrying.', updated_at=? WHERE state='sending'`, time.Now().Unix())
	return err
}

func (db *DB) RetryOutbox(id int64, maxAttempts int) error {
	res, err := db.Exec(`UPDATE outbox SET state='queued', attempts=0, max_attempts=?, next_attempt=0, last_error='', updated_at=?
  WHERE id=? AND state IN ('failed','uncertain')`, max(1, maxAttempts), time.Now().Unix(), id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrOutboxBusy
	}
	return nil
}

func (db *DB) DeleteOutbox(id int64) error {
	res, err := db.Exec(`DELETE FROM outbox WHERE id=? AND state!='sending'`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrOutboxBusy
	}
	return nil
}

// Move the unsent message back into Drafts atomically, preserving attachments.
// A crash cannot leave it absent from both places or still eligible for sending.
func (db *DB) EditOutbox(id int64) (Draft, error) {
	tx, err := db.Begin()
	if err != nil {
		return Draft{}, err
	}
	defer tx.Rollback() //nolint:errcheck
	var payload []byte
	var state string
	if err := tx.QueryRow(`SELECT draft_json, state FROM outbox WHERE id=?`, id).Scan(&payload, &state); err != nil {
		return Draft{}, err
	}
	if state == OutboxSending || state == OutboxSent {
		return Draft{}, ErrOutboxBusy
	}
	var draft Draft
	if err := json.Unmarshal(payload, &draft); err != nil {
		return Draft{}, fmt.Errorf("read outbox draft: %w", err)
	}
	if draft.ID != 0 {
		var found int
		err := tx.QueryRow(`SELECT id FROM drafts WHERE id=?`, draft.ID).Scan(&found)
		if errors.Is(err, sql.ErrNoRows) {
			draft.ID = 0
		} else if err != nil {
			return Draft{}, err
		}
	}
	draft.Dirty = true
	draft.UpdatedAt = time.Now()
	draft.ID, err = saveDraft(tx, draft)
	if err != nil {
		return Draft{}, err
	}
	if _, err := tx.Exec(`DELETE FROM outbox WHERE id=?`, id); err != nil {
		return Draft{}, err
	}
	if err := tx.Commit(); err != nil {
		return Draft{}, err
	}
	return draft, nil
}

func (db *DB) FailQueuedOutbox(id int64, message string) error {
	_, err := db.Exec(`UPDATE outbox SET state='failed', last_error=?, updated_at=? WHERE id=? AND state='queued'`, message, time.Now().Unix(), id)
	return err
}
