package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/mailtag"
)

func (db *DB) SaveMailTag(d mailtag.Definition) error {
	if err := mailtag.ValidateKeyword(d.Keyword); err != nil {
		return err
	}
	d.Name = strings.TrimSpace(d.Name)
	if err := mailtag.ValidateName(d.Name); err != nil {
		return err
	}
	if d.Color != "" {
		color, ok := config.NormalizeTagHex(d.Color)
		if !ok {
			return fmt.Errorf("tag color must be #rgb or #rrggbb")
		}
		d.Color = color
	}
	_, err := db.Exec(`INSERT INTO mail_tags(account_id, keyword, name, name_search, color) VALUES (?, ?, ?, ?, ?)
 ON CONFLICT(account_id, keyword) DO UPDATE SET name=excluded.name, name_search=excluded.name_search, color=excluded.color`, d.AccountID, d.Keyword, d.Name, strings.ToLower(d.Name), d.Color)
	return err
}

func (db *DB) ListMailTags() ([]mailtag.Definition, error) {
	rows, err := db.Query(`SELECT account_id, keyword, name, color FROM mail_tags ORDER BY account_id, lower(name), keyword`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []mailtag.Definition
	for rows.Next() {
		var d mailtag.Definition
		if err := rows.Scan(&d.AccountID, &d.Keyword, &d.Name, &d.Color); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func discoverMailTags(tx *sql.Tx, mailboxID int64, flags []string) error {
	for _, keyword := range mailtag.Keywords(flags) {
		d := mailtag.Default(0, keyword)
		if _, err := tx.Exec(`INSERT INTO mail_tags(account_id, keyword, name, name_search, color)
   SELECT account_id, ?, ?, ?, ? FROM mailboxes WHERE id=?
   ON CONFLICT(account_id, keyword) DO NOTHING`, d.Keyword, d.Name, strings.ToLower(d.Name), d.Color, mailboxID); err != nil {
			return err
		}
	}
	return nil
}

// ApplyServerFlags updates only flag data. Plugin classifications and local
// overrides are independent; read and starred are reconciled by their owners.
func (db *DB) ApplyServerFlags(mailboxID int64, flagsByUID map[uint32][]string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	var discovered []string
	for uid, flags := range flagsByUID {
		encoded, err := json.Marshal(flags)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE messages SET flags=? WHERE mailbox_id=? AND uid=? AND flags != ?`, string(encoded), mailboxID, uid, string(encoded)); err != nil {
			return err
		}
		discovered = append(discovered, flags...)
	}
	if err := discoverMailTags(tx, mailboxID, discovered); err != nil {
		return err
	}
	return tx.Commit()
}

// DiscoverCachedMailTags also covers databases created before mail tag support.
func (db *DB) DiscoverCachedMailTags() error {
	rows, err := db.Query(`SELECT DISTINCT mailbox_id, flags FROM messages WHERE flags != '[]' AND flags != 'null'`)
	if err != nil {
		return err
	}
	type cached struct {
		mailboxID int64
		flags     []string
	}
	var items []cached
	for rows.Next() {
		var item cached
		var raw string
		if err := rows.Scan(&item.mailboxID, &raw); err != nil {
			rows.Close()
			return err
		}
		if err := json.Unmarshal([]byte(raw), &item.flags); err != nil {
			rows.Close()
			return err
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	for _, item := range items {
		if err := discoverMailTags(tx, item.mailboxID, item.flags); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO settings(key, value) VALUES ('mail_tags_backfill_v1', 'done')
 ON CONFLICT(key) DO UPDATE SET value='done'`); err != nil {
		return err
	}
	return tx.Commit()
}

// RefreshMailFlags prevents an older asynchronous list result from restoring
// flags captured before a tag edit. Deleted or moved rows are left untouched.
func (db *DB) RefreshMailFlags(messages []Message) error {
	if len(messages) == 0 {
		return nil
	}
	args := make([]any, len(messages))
	byID := make(map[int64]int, len(messages))
	for i, message := range messages {
		args[i] = message.ID
		byID[message.ID] = i
	}
	rows, err := db.Query(`SELECT id, mailbox_id, uid, flags FROM messages WHERE id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")+`)`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, mailboxID int64
		var uid uint32
		var raw string
		if err := rows.Scan(&id, &mailboxID, &uid, &raw); err != nil {
			return err
		}
		i := byID[id]
		if messages[i].MailboxID == mailboxID && messages[i].UID == uid {
			if err := json.Unmarshal([]byte(raw), &messages[i].Flags); err != nil {
				return err
			}
		}
	}
	return rows.Err()
}
