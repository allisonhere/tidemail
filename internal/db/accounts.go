package db

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Account struct {
	ID int64
	// ConfigID is the stable ID of the [[account]] block in config.toml that
	// owns this row. It is the join key; Name is only what the user sees.
	ConfigID string
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
	rows, err := db.Query(`SELECT id, config_id, name, position, color FROM accounts ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var accounts []Account
	for rows.Next() {
		var a Account
		if err := rows.Scan(&a.ID, &a.ConfigID, &a.Name, &a.Position, &a.Color); err != nil {
			return nil, err
		}
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}

func (db *DB) GetAccount(id int64) (Account, error) {
	var a Account
	err := db.QueryRow(`SELECT id, config_id, name, position, color FROM accounts WHERE id = ?`, id).
		Scan(&a.ID, &a.ConfigID, &a.Name, &a.Position, &a.Color)
	return a, err
}

func (db *DB) AddAccount(configID, name, color string) (int64, error) {
	var maxPos int
	db.QueryRow(`SELECT COALESCE(MAX(position),0) FROM accounts`).Scan(&maxPos) //nolint:errcheck
	res, err := db.Exec(`INSERT INTO accounts (config_id, name, position, color) VALUES (?, ?, ?, ?)`,
		configID, name, maxPos+1, color)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateAccount renames an account and re-stamps its config link. The link is
// written here too so an account row adopted from a pre-config_id database
// stops relying on its name the first time it is edited.
func (db *DB) UpdateAccount(id int64, configID, name, color string) error {
	_, err := db.Exec(`UPDATE accounts SET config_id = ?, name = ?, color = ? WHERE id = ?`,
		configID, name, color, id)
	return err
}

// SaveAccountWithMailboxes writes an account and its discovered mailboxes as a
// single transaction. A failed mailbox upsert must not leave a renamed account
// or a partially-created mailbox set behind.
func (db *DB) SaveAccountWithMailboxes(editID int64, configID, name, color string, mailboxes []Mailbox) (Account, []Mailbox, error) {
	tx, err := db.Begin()
	if err != nil {
		return Account{}, nil, err
	}
	defer func() { _ = tx.Rollback() }() // commit below owns the success path

	accountID := editID
	if editID != 0 {
		if _, err := tx.Exec(`UPDATE accounts SET config_id = ?, name = ?, color = ? WHERE id = ?`, configID, name, color, editID); err != nil {
			return Account{}, nil, err
		}
	} else {
		var maxPos int
		if err := tx.QueryRow(`SELECT COALESCE(MAX(position),0) FROM accounts`).Scan(&maxPos); err != nil {
			return Account{}, nil, err
		}
		res, err := tx.Exec(`INSERT INTO accounts (config_id, name, position, color) VALUES (?, ?, ?, ?)`, configID, name, maxPos+1, color)
		if err != nil {
			return Account{}, nil, err
		}
		accountID, err = res.LastInsertId()
		if err != nil {
			return Account{}, nil, err
		}
	}

	var account Account
	if err := tx.QueryRow(`SELECT id, config_id, name, position, color FROM accounts WHERE id = ?`, accountID).
		Scan(&account.ID, &account.ConfigID, &account.Name, &account.Position, &account.Color); err != nil {
		return Account{}, nil, err
	}

	saved := make([]Mailbox, 0, len(mailboxes))
	for _, mailbox := range mailboxes {
		mailbox.AccountID = accountID
		flagsJSON, _ := json.Marshal(mailbox.Flags)
		if mailbox.DisplayName == "" {
			mailbox.DisplayName = mailbox.Name
		}
		if err := tx.QueryRow(`
			INSERT INTO mailboxes (account_id, name, display_name, delimiter, flags)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(account_id, name) DO UPDATE SET
				display_name = excluded.display_name,
				delimiter    = excluded.delimiter,
				flags        = excluded.flags
			RETURNING id
		`, mailbox.AccountID, mailbox.Name, mailbox.DisplayName, mailbox.Delimiter, string(flagsJSON)).Scan(&mailbox.ID); err != nil {
			return Account{}, nil, err
		}
		saved = append(saved, mailbox)
	}

	if err := tx.Commit(); err != nil {
		return Account{}, nil, err
	}
	return account, saved, nil
}

// AccountLink pairs a config account's stable ID with the display name that
// older builds used as the link between config.toml and this database.
type AccountLink struct {
	ConfigID string
	Name     string
}

// MigrateAccountConfigIDs backfills config_id on rows written before the column
// existed, matching on the display name. A name shared by more than one config
// block or more than one account row is left unlinked rather than guessed: an
// ambiguous row shows up as an account needing repair, which is recoverable,
// whereas a wrong link syncs one account with another's credentials.
func (db *DB) MigrateAccountConfigIDs(links []AccountLink) error {
	byName := map[string][]string{}
	for _, l := range links {
		name := strings.TrimSpace(l.Name)
		if name == "" || strings.TrimSpace(l.ConfigID) == "" {
			continue
		}
		byName[name] = append(byName[name], l.ConfigID)
	}
	rows, err := db.Query(`SELECT id, name FROM accounts WHERE config_id = ''`)
	if err != nil {
		return err
	}
	type pending struct {
		id   int64
		name string
	}
	var unlinked []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.name); err != nil {
			rows.Close()
			return err
		}
		unlinked = append(unlinked, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	seen := map[string]int{}
	for _, p := range unlinked {
		seen[strings.TrimSpace(p.name)]++
	}
	for _, p := range unlinked {
		name := strings.TrimSpace(p.name)
		ids := byName[name]
		if len(ids) != 1 || seen[name] != 1 {
			continue
		}
		if _, err := db.Exec(`UPDATE accounts SET config_id = ? WHERE id = ? AND config_id = ''`,
			ids[0], p.id); err != nil {
			return err
		}
	}
	return nil
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
	var id int64
	err := db.QueryRow(`
		INSERT INTO mailboxes (account_id, name, display_name, delimiter, flags)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(account_id, name) DO UPDATE SET
			display_name = excluded.display_name,
			delimiter    = excluded.delimiter,
			flags        = excluded.flags
		RETURNING id
	`, m.AccountID, m.Name, displayName, m.Delimiter, string(flagsJSON)).Scan(&id)
	if err != nil {
		return 0, err
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

func (db *DB) FindJunkMailbox(accountID int64) (Mailbox, error) {
	mailboxes, err := db.ListMailboxes(accountID)
	if err != nil {
		return Mailbox{}, err
	}
	var nameMatch *Mailbox
	for i, mb := range mailboxes {
		for _, flag := range mb.Flags {
			if strings.EqualFold(flag, `\Junk`) {
				return mb, nil
			}
		}
		if nameMatch == nil && (isCommonJunkMailboxName(mb.Name) || isCommonJunkMailboxName(mb.DisplayName)) {
			nameMatch = &mailboxes[i]
		}
	}
	if nameMatch != nil {
		return *nameMatch, nil
	}
	return Mailbox{}, fmt.Errorf("junk mailbox not found")
}

func (db *DB) FindDraftsMailbox(accountID int64) (Mailbox, error) {
	mailboxes, err := db.ListMailboxes(accountID)
	if err != nil {
		return Mailbox{}, err
	}
	var nameMatch *Mailbox
	for i, mb := range mailboxes {
		for _, flag := range mb.Flags {
			if strings.EqualFold(flag, `\Drafts`) {
				return mb, nil
			}
		}
		if nameMatch == nil && (isCommonDraftsMailboxName(mb.Name) || isCommonDraftsMailboxName(mb.DisplayName)) {
			nameMatch = &mailboxes[i]
		}
	}
	if nameMatch != nil {
		return *nameMatch, nil
	}
	return Mailbox{}, fmt.Errorf("drafts mailbox not found")
}

// AccountIDByName resolves an account row from its configured name.
// AccountIDByConfigID resolves the account row owned by a config block. It
// replaces the old name lookup, which returned an arbitrary row when two
// accounts shared a display name.
func (db *DB) AccountIDByConfigID(configID string) (int64, error) {
	if strings.TrimSpace(configID) == "" {
		return 0, fmt.Errorf("account not found: no config id")
	}
	var id int64
	if err := db.QueryRow(`SELECT id FROM accounts WHERE config_id = ?`, configID).Scan(&id); err != nil {
		return 0, fmt.Errorf("account %q not found: %w", configID, err)
	}
	return id, nil
}

// FindSentMailbox locates the account's Sent folder, preferring the \Sent
// special-use flag and falling back to the conventional names. Servers differ:
// Gmail uses "[Gmail]/Sent Mail", Dovecot "INBOX.Sent", Exchange "Sent Items".
func (db *DB) FindSentMailbox(accountID int64) (Mailbox, error) {
	mailboxes, err := db.ListMailboxes(accountID)
	if err != nil {
		return Mailbox{}, err
	}
	var nameMatch *Mailbox
	for i, mb := range mailboxes {
		for _, flag := range mb.Flags {
			if strings.EqualFold(flag, `\Sent`) {
				return mb, nil
			}
		}
		if nameMatch == nil && (isCommonSentMailboxName(mb.Name) || isCommonSentMailboxName(mb.DisplayName)) {
			nameMatch = &mailboxes[i]
		}
	}
	if nameMatch != nil {
		return *nameMatch, nil
	}
	return Mailbox{}, fmt.Errorf("sent mailbox not found")
}

// isCommonSentMailboxName covers both hierarchy delimiters, since a dot-
// delimited server yields "INBOX.Sent" where Gmail yields "[Gmail]/Sent Mail".
func isCommonSentMailboxName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	switch n {
	case "sent", "sent items", "sent mail", "sent messages":
		return true
	}
	for _, suffix := range []string{"/sent", ".sent", "/sent items", ".sent items",
		"/sent mail", ".sent mail", "/sent messages", ".sent messages"} {
		if strings.HasSuffix(n, suffix) {
			return true
		}
	}
	return false
}

func isCommonDraftsMailboxName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return n == "drafts" || strings.HasSuffix(n, "/drafts") || strings.HasSuffix(n, ".drafts")
}

func isCommonJunkMailboxName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return n == "junk" || n == "spam" || n == "junk email" || n == "junk e-mail" ||
		strings.HasSuffix(n, "/junk") || strings.HasSuffix(n, "/spam") || strings.HasSuffix(n, "/junk email")
}

func isCommonArchiveMailboxName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return n == "archive" || n == "archives" || strings.HasSuffix(n, "/archive") || strings.HasSuffix(n, "/archives") || n == "all mail" || strings.HasSuffix(n, "/all mail")
}

func isCommonTrashMailboxName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return n == "trash" || n == "deleted items" || n == "deleted messages" || strings.HasSuffix(n, "/trash") || strings.HasSuffix(n, "/deleted items") || strings.HasSuffix(n, "/deleted messages")
}
