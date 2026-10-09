package db

// MailboxPref is a folder's local view preference.
type MailboxPref struct {
	Hidden bool
	// Order is the folder's position among its siblings; 0 means never
	// reordered, which sorts after every explicitly ordered sibling.
	Order int
}

// ListMailboxPrefs returns every stored preference, by account then folder name.
func (db *DB) ListMailboxPrefs() (map[int64]map[string]MailboxPref, error) {
	rows, err := db.Query(`SELECT account_id, name, hidden, sort_order FROM mailbox_prefs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]map[string]MailboxPref{}
	for rows.Next() {
		var (
			accountID int64
			name      string
			hidden    int
			order     int
		)
		if err := rows.Scan(&accountID, &name, &hidden, &order); err != nil {
			return nil, err
		}
		if out[accountID] == nil {
			out[accountID] = map[string]MailboxPref{}
		}
		out[accountID][name] = MailboxPref{Hidden: hidden != 0, Order: order}
	}
	return out, rows.Err()
}

// SetMailboxHidden shows or hides a folder in the sidebar.
func (db *DB) SetMailboxHidden(accountID int64, name string, hidden bool) error {
	h := 0
	if hidden {
		h = 1
	}
	_, err := db.Exec(`
		INSERT INTO mailbox_prefs (account_id, name, hidden) VALUES (?, ?, ?)
		ON CONFLICT(account_id, name) DO UPDATE SET hidden = excluded.hidden`, accountID, name, h)
	return err
}

// SetMailboxOrders stores the sibling order of several folders at once.
func (db *DB) SetMailboxOrders(accountID int64, orders map[string]int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	for name, order := range orders {
		if _, err := tx.Exec(`
			INSERT INTO mailbox_prefs (account_id, name, sort_order) VALUES (?, ?, ?)
			ON CONFLICT(account_id, name) DO UPDATE SET sort_order = excluded.sort_order`,
			accountID, name, order); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteMailboxPrefs forgets the preferences of a folder that no longer exists.
func (db *DB) DeleteMailboxPrefs(accountID int64, name string) error {
	_, err := db.Exec(`DELETE FROM mailbox_prefs WHERE account_id = ? AND name = ?`, accountID, name)
	return err
}
