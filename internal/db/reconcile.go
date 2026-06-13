package db

// Sync reconciliation: the incremental SINCE fetch only adds mail, so messages
// removed server-side (archived or deleted from another client) linger locally.
// After each sync the caller fetches the server's full UID set and calls
// ReconcileMailboxUIDs to drop the stragglers, keeping the local cache in step
// with the server. UIDVALIDITY guards the rarer case where a server renumbers a
// mailbox, invalidating every stored UID at once.

// ReconcileMailboxUIDs hard-deletes locally-stored messages whose UID is no
// longer present on the server. Only messages with a real UID are considered —
// local-only rows (uid 0) are left alone. Returns the number removed. The
// server UID set is authoritative, so these are hard deletes (no tombstone):
// a message gone from the server cannot reappear via a later fetch.
func (db *DB) ReconcileMailboxUIDs(mailboxID int64, serverUIDs []uint32) (int, error) {
	server := make(map[uint32]struct{}, len(serverUIDs))
	for _, u := range serverUIDs {
		server[u] = struct{}{}
	}

	rows, err := db.Query(`SELECT id, uid FROM messages WHERE mailbox_id = ? AND uid != 0`, mailboxID)
	if err != nil {
		return 0, err
	}
	var stale []int64
	for rows.Next() {
		var id int64
		var uid uint32
		if err := rows.Scan(&id, &uid); err != nil {
			rows.Close() //nolint:errcheck
			return 0, err
		}
		if _, ok := server[uid]; !ok {
			stale = append(stale, id)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close() //nolint:errcheck
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if len(stale) == 0 {
		return 0, nil
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck
	// Attachments cascade via ON DELETE CASCADE (foreign_keys=ON).
	for _, id := range stale {
		if _, err := tx.Exec(`DELETE FROM messages_fts WHERE rowid = ?`, id); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(`DELETE FROM messages WHERE id = ?`, id); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(stale), nil
}

// ApplyServerReadStates adopts the server's read/unread state for locally-stored
// messages, keyed by UID. This propagates \Seen changes made in another client
// (e.g. Gmail web), which the additive SINCE fetch never re-examines. Only rows
// whose stored read state actually differs are touched; returns how many changed
// so the caller can tell whether the unread count moved. The server is
// authoritative — local mark-read already pushes to the server before updating
// the DB, so there is no unsynced local state to clobber.
func (db *DB) ApplyServerReadStates(mailboxID int64, seenByUID map[uint32]bool) (int, error) {
	if len(seenByUID) == 0 {
		return 0, nil
	}
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck
	changed := 0
	for uid, seen := range seenByUID {
		read := 0
		if seen {
			read = 1
		}
		res, err := tx.Exec(
			`UPDATE messages SET read = ? WHERE mailbox_id = ? AND uid != 0 AND uid = ? AND read != ?`,
			read, mailboxID, uid, read)
		if err != nil {
			return 0, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			changed += int(n)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return changed, nil
}

// MailboxUIDValidity returns the stored UIDVALIDITY for a mailbox (0 if never
// recorded).
func (db *DB) MailboxUIDValidity(mailboxID int64) (uint32, error) {
	var v uint32
	err := db.QueryRow(`SELECT uid_validity FROM mailboxes WHERE id = ?`, mailboxID).Scan(&v)
	return v, err
}

// SetMailboxUIDValidity records a mailbox's UIDVALIDITY.
func (db *DB) SetMailboxUIDValidity(mailboxID int64, v uint32) error {
	_, err := db.Exec(`UPDATE mailboxes SET uid_validity = ? WHERE id = ?`, v, mailboxID)
	return err
}

// ResetMailboxCache clears all cached messages, their delete-tombstones, and the
// last-synced cursor for a mailbox. Used when UIDVALIDITY changes: every stored
// UID is meaningless, so the mailbox is refetched from scratch on the next sync.
func (db *DB) ResetMailboxCache(mailboxID int64) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec(`
		DELETE FROM messages_fts
		WHERE rowid IN (SELECT id FROM messages WHERE mailbox_id = ?)`, mailboxID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM messages WHERE mailbox_id = ?`, mailboxID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM deleted_messages WHERE mailbox_id = ?`, mailboxID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE mailboxes SET last_synced = 0 WHERE id = ?`, mailboxID); err != nil {
		return err
	}
	return tx.Commit()
}
