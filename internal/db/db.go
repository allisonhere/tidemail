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
	return db.migrate()
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
			UNIQUE(mailbox_id, uid)
		);

		CREATE INDEX IF NOT EXISTS idx_messages_mailbox_id ON messages(mailbox_id);
		CREATE INDEX IF NOT EXISTS idx_messages_read       ON messages(read);

		CREATE TABLE IF NOT EXISTS attachments (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			message_id   INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
			filename     TEXT    NOT NULL,
			content_type TEXT    NOT NULL DEFAULT '',
			data         BLOB   NOT NULL,
			size         INTEGER NOT NULL DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS idx_attachments_message_id ON attachments(message_id);
	`)
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
