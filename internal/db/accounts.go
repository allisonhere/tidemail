package db

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Account struct {
	ID       int64
	Name     string
	Position int
	Color    string
}

type Mailbox struct {
	ID          int64
	AccountID   int64
	Name        string
	DisplayName string
	Delimiter   string
	Flags       []string
	UnreadCount int64
	LastSynced  time.Time
}

func (db *DB) ListAccounts() ([]Account, error) {
	rows, err := db.Query(`SELECT id, name, position, color FROM accounts ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var accounts []Account
	for rows.Next() {
		var a Account
		if err := rows.Scan(&a.ID, &a.Name, &a.Position, &a.Color); err != nil {
			return nil, err
		}
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}

func (db *DB) GetAccount(id int64) (Account, error) {
	var a Account
	err := db.QueryRow(`SELECT id, name, position, color FROM accounts WHERE id = ?`, id).
		Scan(&a.ID, &a.Name, &a.Position, &a.Color)
	return a, err
}

func (db *DB) AddAccount(name, color string) (int64, error) {
	var maxPos int
	db.QueryRow(`SELECT COALESCE(MAX(position),0) FROM accounts`).Scan(&maxPos) //nolint:errcheck
	res, err := db.Exec(`INSERT INTO accounts (name, position, color) VALUES (?, ?, ?)`,
		name, maxPos+1, color)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (db *DB) UpdateAccount(id int64, name, color string) error {
	_, err := db.Exec(`UPDATE accounts SET name = ?, color = ? WHERE id = ?`, name, color, id)
	return err
}

func (db *DB) DeleteAccount(id int64) error {
	_, err := db.Exec(`DELETE FROM accounts WHERE id = ?`, id)
	return err
}

func (db *DB) ListMailboxes(accountID int64) ([]Mailbox, error) {
	rows, err := db.Query(`
		SELECT id, account_id, name, display_name, delimiter, flags, unread_count, last_synced
		FROM mailboxes WHERE account_id = ? ORDER BY name`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var mailboxes []Mailbox
	for rows.Next() {
		var m Mailbox
		var flagsJSON string
		var lastSynced int64
		if err := rows.Scan(&m.ID, &m.AccountID, &m.Name, &m.DisplayName,
			&m.Delimiter, &flagsJSON, &m.UnreadCount, &lastSynced); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(flagsJSON), &m.Flags) //nolint:errcheck
		if lastSynced > 0 {
			m.LastSynced = time.Unix(lastSynced, 0)
		}
		mailboxes = append(mailboxes, m)
	}
	return mailboxes, rows.Err()
}

func (db *DB) GetMailbox(id int64) (Mailbox, error) {
	var m Mailbox
	var flagsJSON string
	var lastSynced int64
	err := db.QueryRow(`
		SELECT id, account_id, name, display_name, delimiter, flags, unread_count, last_synced
		FROM mailboxes WHERE id = ?`, id).
		Scan(&m.ID, &m.AccountID, &m.Name, &m.DisplayName,
			&m.Delimiter, &flagsJSON, &m.UnreadCount, &lastSynced)
	if err != nil {
		return m, err
	}
	json.Unmarshal([]byte(flagsJSON), &m.Flags) //nolint:errcheck
	if lastSynced > 0 {
		m.LastSynced = time.Unix(lastSynced, 0)
	}
	return m, nil
}

func (db *DB) UpsertMailbox(m Mailbox) (int64, error) {
	flagsJSON, _ := json.Marshal(m.Flags)
	displayName := m.DisplayName
	if displayName == "" {
		displayName = m.Name
	}
	res, err := db.Exec(`
		INSERT INTO mailboxes (account_id, name, display_name, delimiter, flags)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(account_id, name) DO UPDATE SET
			display_name = excluded.display_name,
			delimiter    = excluded.delimiter,
			flags        = excluded.flags
	`, m.AccountID, m.Name, displayName, m.Delimiter, string(flagsJSON))
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil || id == 0 {
		db.QueryRow(`SELECT id FROM mailboxes WHERE account_id = ? AND name = ?`,
			m.AccountID, m.Name).Scan(&id) //nolint:errcheck
	}
	return id, nil
}

func (db *DB) DeleteMailbox(id int64) error {
	_, err := db.Exec(`DELETE FROM mailboxes WHERE id = ?`, id)
	return err
}

func (db *DB) SetMailboxUnreadCount(mailboxID, count int64) error {
	_, err := db.Exec(`UPDATE mailboxes SET unread_count = ? WHERE id = ?`, count, mailboxID)
	return err
}

func (db *DB) SetMailboxLastSynced(mailboxID int64, t time.Time) error {
	_, err := db.Exec(`UPDATE mailboxes SET last_synced = ? WHERE id = ?`, t.Unix(), mailboxID)
	return err
}

func (db *DB) FindArchiveMailbox(accountID int64) (Mailbox, error) {
	mailboxes, err := db.ListMailboxes(accountID)
	if err != nil {
		return Mailbox{}, err
	}
	var nameMatch *Mailbox
	for i, mb := range mailboxes {
		for _, flag := range mb.Flags {
			if strings.EqualFold(flag, `\Archive`) {
				return mb, nil
			}
		}
		if nameMatch == nil && (isCommonArchiveMailboxName(mb.Name) || isCommonArchiveMailboxName(mb.DisplayName)) {
			nameMatch = &mailboxes[i]
		}
	}
	if nameMatch != nil {
		return *nameMatch, nil
	}
	return Mailbox{}, fmt.Errorf("archive mailbox not found")
}

func (db *DB) FindTrashMailbox(accountID int64) (Mailbox, error) {
	mailboxes, err := db.ListMailboxes(accountID)
	if err != nil {
		return Mailbox{}, err
	}
	var nameMatch *Mailbox
	for i, mb := range mailboxes {
		for _, flag := range mb.Flags {
			if strings.EqualFold(flag, `\Trash`) {
				return mb, nil
			}
		}
		if nameMatch == nil && (isCommonTrashMailboxName(mb.Name) || isCommonTrashMailboxName(mb.DisplayName)) {
			nameMatch = &mailboxes[i]
		}
	}
	if nameMatch != nil {
		return *nameMatch, nil
	}
	return Mailbox{}, fmt.Errorf("trash mailbox not found")
}

func isCommonArchiveMailboxName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return n == "archive" || n == "archives" || strings.HasSuffix(n, "/archive") || strings.HasSuffix(n, "/archives") || n == "all mail" || strings.HasSuffix(n, "/all mail")
}

func isCommonTrashMailboxName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return n == "trash" || n == "deleted items" || n == "deleted messages" || strings.HasSuffix(n, "/trash") || strings.HasSuffix(n, "/deleted items") || strings.HasSuffix(n, "/deleted messages")
}
