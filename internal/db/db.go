package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type DB struct {
	*sql.DB
}

func Open() (*DB, error) {
	dir, err := dataDir()
	if err != nil {
		return nil, fmt.Errorf("data dir: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	path := filepath.Join(dir, "mail.db")
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	conn.SetMaxOpenConns(1)

	db := &DB{conn}
	if err := db.init(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("init db: %w", err)
	}
	return db, nil
}

func (db *DB) init() error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return err
		}
	}
	if err := db.migrate(); err != nil {
		return err
	}
	return nil
}

func (db *DB) migrate() error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS accounts (
			id       INTEGER PRIMARY KEY AUTOINCREMENT,
			name     TEXT    NOT NULL,
			position INTEGER NOT NULL DEFAULT 0,
			color    TEXT    NOT NULL DEFAULT ''
		);

		CREATE TABLE IF NOT EXISTS mailboxes (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			account_id   INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			name         TEXT    NOT NULL,
			display_name TEXT    NOT NULL DEFAULT '',
			delimiter    TEXT    NOT NULL DEFAULT '/',
			flags        TEXT    NOT NULL DEFAULT '[]',
			unread_count INTEGER NOT NULL DEFAULT 0,
			last_synced  INTEGER NOT NULL DEFAULT 0,
			UNIQUE(account_id, name)
		);

		CREATE TABLE IF NOT EXISTS messages (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			mailbox_id     INTEGER NOT NULL REFERENCES mailboxes(id) ON DELETE CASCADE,
			uid            INTEGER NOT NULL DEFAULT 0,
			message_id     TEXT    NOT NULL DEFAULT '',
			subject        TEXT    NOT NULL DEFAULT '',
			from_addr      TEXT    NOT NULL DEFAULT '',
			to_addr        TEXT    NOT NULL DEFAULT '',
			cc_addr        TEXT    NOT NULL DEFAULT '',
			reply_to       TEXT    NOT NULL DEFAULT '',
			date           INTEGER NOT NULL DEFAULT 0,
			body_text      TEXT    NOT NULL DEFAULT '',
			body_html      TEXT    NOT NULL DEFAULT '',
			summary        TEXT    NOT NULL DEFAULT '',
			flags          TEXT    NOT NULL DEFAULT '[]',
			read           INTEGER NOT NULL DEFAULT 0,
			has_attachment INTEGER NOT NULL DEFAULT 0,
			headers        TEXT    NOT NULL DEFAULT '',
			UNIQUE(mailbox_id, uid)
		);

		CREATE INDEX IF NOT EXISTS idx_messages_mailbox_id ON messages(mailbox_id);
		CREATE INDEX IF NOT EXISTS idx_messages_read       ON messages(read);

		CREATE TABLE IF NOT EXISTS deleted_messages (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			mailbox_id INTEGER NOT NULL REFERENCES mailboxes(id) ON DELETE CASCADE,
			uid        INTEGER NOT NULL DEFAULT 0,
			message_id TEXT    NOT NULL DEFAULT '',
			deleted_at INTEGER NOT NULL DEFAULT 0
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_deleted_messages_mailbox_uid
			ON deleted_messages(mailbox_id, uid)
			WHERE uid != 0;
		CREATE UNIQUE INDEX IF NOT EXISTS idx_deleted_messages_mailbox_message_id
			ON deleted_messages(mailbox_id, message_id)
			WHERE message_id != '';

		CREATE TABLE IF NOT EXISTS attachments (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			message_id   INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
			filename     TEXT    NOT NULL,
			content_type TEXT    NOT NULL DEFAULT '',
			data         BLOB   NOT NULL,
			size         INTEGER NOT NULL DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS idx_attachments_message_id ON attachments(message_id);

		CREATE TABLE IF NOT EXISTS settings (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL DEFAULT ''
		);

		CREATE TABLE IF NOT EXISTS contacts (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			addr         TEXT    NOT NULL UNIQUE,
			display_name TEXT    NOT NULL DEFAULT '',
			source       TEXT    NOT NULL DEFAULT 'manual',
			created_at   INTEGER NOT NULL DEFAULT 0,
			phone        TEXT    NOT NULL DEFAULT '',
			organization TEXT    NOT NULL DEFAULT '',
			title        TEXT    NOT NULL DEFAULT '',
			note         TEXT    NOT NULL DEFAULT ''
		);
	`)
	if err != nil {
		return err
	}
	// Migrations: add columns that may not exist on older databases.
	// SQLite error on duplicate column is silently ignored.
	db.Exec(`ALTER TABLE messages ADD COLUMN headers TEXT NOT NULL DEFAULT ''`) //nolint:errcheck
	if err := db.migrateContactsToCuratedSchema(); err != nil {
		return err
	}
	if err := db.migrateContactMetadataColumns(); err != nil {
		return err
	}
	if err := db.PruneDeletedMessageTombstones(); err != nil {
		return err
	}
	return nil
}

func (db *DB) migrateContactsToCuratedSchema() error {
	rows, err := db.Query(`PRAGMA table_info(contacts)`)
	if err != nil {
		return err
	}
	defer rows.Close()

	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !columns["status"] && !columns["use_count"] && !columns["last_used"] {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec(`
		CREATE TABLE contacts_curated_new (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			addr         TEXT    NOT NULL UNIQUE,
			display_name TEXT    NOT NULL DEFAULT '',
			source       TEXT    NOT NULL DEFAULT 'manual',
			created_at   INTEGER NOT NULL DEFAULT 0,
			phone        TEXT    NOT NULL DEFAULT '',
			organization TEXT    NOT NULL DEFAULT '',
			title        TEXT    NOT NULL DEFAULT '',
			note         TEXT    NOT NULL DEFAULT ''
		)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT OR IGNORE INTO contacts_curated_new (id, addr, display_name, source, created_at)
		SELECT id, addr, display_name, source, created_at
		FROM contacts
		WHERE source IN ('manual', 'vcard', 'carddav')`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DROP TABLE contacts`); err != nil {
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE contacts_curated_new RENAME TO contacts`); err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) migrateContactMetadataColumns() error {
	for _, stmt := range []string{
		`ALTER TABLE contacts ADD COLUMN phone TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE contacts ADD COLUMN organization TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE contacts ADD COLUMN title TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE contacts ADD COLUMN note TEXT NOT NULL DEFAULT ''`,
	} {
		db.Exec(stmt) //nolint:errcheck
	}
	return nil
}

// GetSetting retrieves a UI setting value by key.
func (db *DB) GetSetting(key string) (string, error) {
	var val string
	err := db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&val)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return val, err
}

// SetSetting stores a UI setting value by key.
func (db *DB) SetSetting(key, value string) error {
	_, err := db.Exec("INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = ?", key, value, value)
	return err
}

func dataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	xdg := os.Getenv("XDG_DATA_HOME")
	if xdg == "" {
		xdg = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(xdg, "tidemail"), nil
}
