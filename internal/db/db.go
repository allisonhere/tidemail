package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/allisonhere/tidemail/internal/bodytext"
)

type DB struct {
	*sql.DB
	stopFTS context.CancelFunc
	ftsDone chan struct{}
	// ftsPending is set by migrateMessageFTS when a stale index is too big to
	// rebuild while opening, so startFTSRebuild need not check again.
	ftsPending bool
}

func Open() (*DB, error) {
	dir, err := dataDir()
	if err != nil {
		return nil, fmt.Errorf("data dir: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	return OpenPath(filepath.Join(dir, "mail.db"))
}

// OpenPath opens (creating if needed) a TideMail database at path. Open uses
// it for the user's cache; tools use it for throwaway fixture databases.
func OpenPath(path string) (*DB, error) {
	dsn, err := sqliteDSN(path)
	if err != nil {
		return nil, fmt.Errorf("database path: %w", err)
	}
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	conn.SetMaxOpenConns(1)

	db := &DB{DB: conn}
	if err := db.init(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("init db: %w", err)
	}
	db.startFTSRebuild()
	return db, nil
}

// sqliteDSN configures every physical connection, including replacements after
// driver.ErrBadConn. File paths are URI-escaped so ?, # and % remain filename bytes.
func sqliteDSN(path string) (string, error) {
	var dsn string
	var params url.Values
	switch {
	case strings.HasPrefix(path, "file:"):
		u, err := url.Parse(path)
		if err != nil {
			return "", err
		}
		params = u.Query()
		u.RawQuery = ""
		dsn = u.String()
	case path == ":memory:" || path == "":
		dsn = path
		params = make(url.Values)
	default:
		absolute, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		dsn = fileURI(filepath.ToSlash(absolute))
		params = make(url.Values)
	}
	for _, pragma := range []string{"foreign_keys(1)", "busy_timeout(5000)", "journal_mode(WAL)"} {
		params.Add("_pragma", pragma)
	}
	return dsn + "?" + params.Encode(), nil
}

// fileURI turns an absolute, slash-separated path into an SQLite file: URI.
// SQLite only accepts an empty or "localhost" authority, so the URI path must
// start with "/". A Windows path such as C:/Users/me/mail.db therefore becomes
// file:///C:/Users/me/mail.db, and SQLite drops the slash before the drive
// letter again when it opens the file. Without the slash net/url produces
// file://C:/Users/me/mail.db, SQLite reads "C:" as the authority and refuses
// to open it, which the driver reports as "SQL logic error: out of memory".
func fileURI(slashPath string) string {
	if !strings.HasPrefix(slashPath, "/") {
		slashPath = "/" + slashPath
	}
	return (&url.URL{Scheme: "file", Path: slashPath}).String()
}

func (db *DB) init() error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return err
		}
	}
	if err := db.migrate(); err != nil {
		return err
	}
	if err := db.pruneStatusMailTags(); err != nil {
		return err
	}
	backfilled, err := db.GetSetting("mail_tags_backfill_v1")
	if err != nil {
		return err
	}
	if backfilled != "done" {
		return db.DiscoverCachedMailTags()
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

		-- The user's personal spell-check words (names, jargon), stored lowercase.
		CREATE TABLE IF NOT EXISTS spell_words (
			word TEXT PRIMARY KEY
		);

		-- Local-only folder view preferences (never sent to the server), keyed by
		-- folder name so they survive a re-sync that reassigns mailbox ids.
		CREATE TABLE IF NOT EXISTS mailbox_prefs (
			account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			name       TEXT    NOT NULL,
			hidden     INTEGER NOT NULL DEFAULT 0,
			sort_order INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (account_id, name)
		);

		CREATE TABLE IF NOT EXISTS messages (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			mailbox_id     INTEGER NOT NULL REFERENCES mailboxes(id) ON DELETE CASCADE,
			uid            INTEGER NOT NULL DEFAULT 0,
			message_id     TEXT    NOT NULL DEFAULT '',
			in_reply_to    TEXT    NOT NULL DEFAULT '',
			references_text TEXT   NOT NULL DEFAULT '',
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
			starred        INTEGER NOT NULL DEFAULT 0,
			has_attachment INTEGER NOT NULL DEFAULT 0,
			headers        TEXT    NOT NULL DEFAULT '',
			UNIQUE(mailbox_id, uid)
		);

		CREATE INDEX IF NOT EXISTS idx_messages_mailbox_id ON messages(mailbox_id);
		CREATE INDEX IF NOT EXISTS idx_messages_read       ON messages(read);

		CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
			subject,
			from_addr,
			to_addr,
			cc_addr,
			body_text
		);

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
			id                  INTEGER PRIMARY KEY AUTOINCREMENT,
			message_id          INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
			filename            TEXT    NOT NULL,
			content_type        TEXT    NOT NULL DEFAULT '',
			content_id          TEXT    NOT NULL DEFAULT '',
			content_disposition TEXT    NOT NULL DEFAULT '',
			content_location    TEXT    NOT NULL DEFAULT '',
			inline              INTEGER NOT NULL DEFAULT 0,
			data                BLOB   NOT NULL,
			size                INTEGER NOT NULL DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS idx_attachments_message_id ON attachments(message_id);

		CREATE TABLE IF NOT EXISTS drafts (
			id                INTEGER PRIMARY KEY AUTOINCREMENT,
			account_name      TEXT    NOT NULL DEFAULT '',
			account_user      TEXT    NOT NULL DEFAULT '',
			account_index     INTEGER NOT NULL DEFAULT 0,
			mailbox_id        INTEGER NOT NULL DEFAULT 0,
			remote_uid        INTEGER NOT NULL DEFAULT 0,
			remote_message_id TEXT    NOT NULL DEFAULT '',
			to_addr           TEXT    NOT NULL DEFAULT '',
			cc_addr           TEXT    NOT NULL DEFAULT '',
			bcc_addr          TEXT    NOT NULL DEFAULT '',
			subject           TEXT    NOT NULL DEFAULT '',
			body_text         TEXT    NOT NULL DEFAULT '',
			in_reply_to       TEXT    NOT NULL DEFAULT '',
			references_text   TEXT    NOT NULL DEFAULT '',
			created_at        INTEGER NOT NULL DEFAULT 0,
			updated_at        INTEGER NOT NULL DEFAULT 0,
			last_remote_sync  INTEGER NOT NULL DEFAULT 0,
			dirty             INTEGER NOT NULL DEFAULT 1
		);
		CREATE INDEX IF NOT EXISTS idx_drafts_account ON drafts(account_name, account_user);
		CREATE INDEX IF NOT EXISTS idx_drafts_updated ON drafts(updated_at);

		CREATE TABLE IF NOT EXISTS draft_attachments (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			draft_id     INTEGER NOT NULL REFERENCES drafts(id) ON DELETE CASCADE,
			filename     TEXT    NOT NULL,
			path         TEXT    NOT NULL DEFAULT '',
			content_type TEXT    NOT NULL DEFAULT '',
			data         BLOB   NOT NULL,
			size         INTEGER NOT NULL DEFAULT 0,
			position     INTEGER NOT NULL DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS idx_draft_attachments_draft_id ON draft_attachments(draft_id);

        CREATE TABLE IF NOT EXISTS outbox (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            account_name TEXT NOT NULL,
            account_user TEXT NOT NULL,
            draft_id INTEGER NOT NULL DEFAULT 0,
            subject TEXT NOT NULL DEFAULT '',
            recipients TEXT NOT NULL DEFAULT '',
            message_json BLOB NOT NULL,
            draft_json BLOB NOT NULL,
            state TEXT NOT NULL DEFAULT 'queued',
            attempts INTEGER NOT NULL DEFAULT 0,
            max_attempts INTEGER NOT NULL DEFAULT 3,
            next_attempt INTEGER NOT NULL DEFAULT 0,
            last_error TEXT NOT NULL DEFAULT '',
            created_at INTEGER NOT NULL,
            updated_at INTEGER NOT NULL
        );
        CREATE INDEX IF NOT EXISTS idx_outbox_state ON outbox(state, next_attempt);

		CREATE TABLE IF NOT EXISTS settings (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL DEFAULT ''
		);

		CREATE TABLE IF NOT EXISTS search_history (
			query   TEXT PRIMARY KEY COLLATE NOCASE,
			used_at INTEGER NOT NULL
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

		CREATE TABLE IF NOT EXISTS rules (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			account_id INTEGER NOT NULL DEFAULT 0,
			priority   INTEGER NOT NULL DEFAULT 0,
			enabled    INTEGER NOT NULL DEFAULT 1,
			name       TEXT    NOT NULL DEFAULT '',
			json       TEXT    NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL DEFAULT 0
		);

		-- Experimental plugin output. One row per plugin/message/key; rows go
		-- away with their message through the cascade.
		CREATE TABLE IF NOT EXISTS plugin_annotations (
			plugin_id  TEXT    NOT NULL,
			message_id INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
			key        TEXT    NOT NULL,
			value      TEXT    NOT NULL,
			confidence REAL,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY (plugin_id, message_id, key)
		);
		CREATE INDEX IF NOT EXISTS idx_plugin_annotations_message ON plugin_annotations(message_id);

		-- TideMail's own per-message attention state. dismissed = 1 keeps a
		-- message out of Needs You regardless of plugin annotations.
		-- "Stop waiting" choices for Waiting on Them, one per waiting cycle,
		-- keyed by the message that started the wait (see db.WaitingKey).
		CREATE TABLE IF NOT EXISTS waiting_dismissals (
			message_key  TEXT PRIMARY KEY,
			dismissed_at INTEGER NOT NULL
		);

		-- Local snoozes (see db/snooze.go): absolute Unix wake times.
		CREATE TABLE IF NOT EXISTS snoozes (
			target_type  TEXT    NOT NULL,
			target_key   TEXT    NOT NULL,
			snooze_until INTEGER NOT NULL,
			created_at   INTEGER NOT NULL,
			PRIMARY KEY (target_type, target_key)
		);
		CREATE INDEX IF NOT EXISTS idx_snoozes_until ON snoozes(snooze_until);

		CREATE TABLE IF NOT EXISTS message_attention_overrides (
			message_id INTEGER PRIMARY KEY REFERENCES messages(id) ON DELETE CASCADE,
			dismissed  INTEGER NOT NULL DEFAULT 0,
			updated_at INTEGER NOT NULL
		);

		CREATE TABLE IF NOT EXISTS mail_tags (
			account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			keyword TEXT COLLATE NOCASE NOT NULL,
			name TEXT NOT NULL,
			name_search TEXT NOT NULL,
			color TEXT NOT NULL DEFAULT '',
			PRIMARY KEY(account_id, keyword)
		);
		-- Explicit user corrections are separate from plugin judgments. One
		-- value per supported semantic field; absence means use plugins.
		CREATE TABLE IF NOT EXISTS classification_overrides (
			message_id INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
			key        TEXT    NOT NULL,
			value      TEXT    NOT NULL,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY (message_id, key)
		);
		CREATE INDEX IF NOT EXISTS idx_classification_overrides_message
			ON classification_overrides(message_id);
	`)
	if err != nil {
		return err
	}
	// Migrations: add columns that may not exist on older databases.
	// SQLite error on duplicate column is silently ignored.
	db.Exec(`ALTER TABLE messages ADD COLUMN raw_headers TEXT NOT NULL DEFAULT ''`)     //nolint:errcheck
	db.Exec(`ALTER TABLE messages ADD COLUMN headers TEXT NOT NULL DEFAULT ''`)         //nolint:errcheck
	db.Exec(`ALTER TABLE mailboxes ADD COLUMN uid_validity INTEGER NOT NULL DEFAULT 0`) //nolint:errcheck
	db.Exec(`ALTER TABLE messages ADD COLUMN in_reply_to TEXT NOT NULL DEFAULT ''`)     //nolint:errcheck
	db.Exec(`ALTER TABLE messages ADD COLUMN references_text TEXT NOT NULL DEFAULT ''`) //nolint:errcheck
	db.Exec(`ALTER TABLE messages ADD COLUMN starred INTEGER NOT NULL DEFAULT 0`)       //nolint:errcheck
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_messages_starred ON messages(starred)`)     //nolint:errcheck
	// Date-ordered pages across mailboxes (plugin queries) walk this index
	// instead of sorting the whole table: ~140x faster at 50k messages.
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_messages_date ON messages(date, id)`) //nolint:errcheck
	// config_id links an account row to its [[account]] block in config.toml.
	// Before it existed the link was the display name, so a rename detached the
	// row and a duplicate name attached two rows to one block.
	db.Exec(`ALTER TABLE accounts ADD COLUMN config_id TEXT NOT NULL DEFAULT ''`) //nolint:errcheck
	_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_accounts_config_id
		ON accounts(config_id) WHERE config_id != ''`)
	db.Exec(`ALTER TABLE drafts ADD COLUMN account_config_id TEXT NOT NULL DEFAULT ''`) //nolint:errcheck
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_drafts_account_config
		ON drafts(account_config_id)`)
	db.Exec(`ALTER TABLE outbox ADD COLUMN account_config_id TEXT NOT NULL DEFAULT ''`) //nolint:errcheck
	// Inline image identity: Content-ID lets HTML src="cid:..." resolve to the
	// exact MIME part, and the disposition/location columns let the reader tell
	// an embedded image from a real downloadable attachment.
	db.Exec(`ALTER TABLE attachments ADD COLUMN content_id TEXT NOT NULL DEFAULT ''`)           //nolint:errcheck
	db.Exec(`ALTER TABLE attachments ADD COLUMN content_disposition TEXT NOT NULL DEFAULT ''`)  //nolint:errcheck
	db.Exec(`ALTER TABLE attachments ADD COLUMN content_location TEXT NOT NULL DEFAULT ''`)     //nolint:errcheck
	db.Exec(`ALTER TABLE attachments ADD COLUMN inline INTEGER NOT NULL DEFAULT 0`)             //nolint:errcheck
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_attachments_content_id ON attachments(content_id)`) //nolint:errcheck
	// Enforce one local mirror per remote draft. Drop any duplicates a previous
	// build's check-then-insert race may have created before adding the index.
	_, _ = db.Exec(`DELETE FROM drafts WHERE remote_uid != 0 AND id NOT IN (
		SELECT MIN(id) FROM drafts WHERE remote_uid != 0 GROUP BY mailbox_id, remote_uid)`)
	_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_drafts_remote_uid
		ON drafts(mailbox_id, remote_uid) WHERE remote_uid != 0`)
	if err := db.migrateContactsToCuratedSchema(); err != nil {
		return err
	}
	if err := db.migrateContactMetadataColumns(); err != nil {
		return err
	}
	if err := db.migrateDraftsBCC(); err != nil {
		return err
	}
	db.Exec(`ALTER TABLE drafts ADD COLUMN sender_from TEXT NOT NULL DEFAULT ''`) //nolint:errcheck
	if err := db.migrateMessageFTS(); err != nil {
		return err
	}
	if err := db.PruneDeletedMessageTombstones(); err != nil {
		return err
	}
	if err := db.initWaitingSince(); err != nil {
		return err
	}
	if err := db.PruneWaitingDismissals(); err != nil {
		return err
	}
	if err := db.PruneStaleSnoozes(); err != nil {
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

func (db *DB) migrateDraftsBCC() error {
	db.Exec(`ALTER TABLE drafts ADD COLUMN bcc_addr TEXT NOT NULL DEFAULT ''`) //nolint:errcheck
	return nil
}

// ftsSchemaVersion is bumped whenever the messages_fts schema or the columns it
// indexes change, to force a one-time rebuild.
const ftsSchemaVersion = "2"

const (
	// ftsSyncLimit is the largest cache rebuilt inline while opening. Bigger
	// caches are rebuilt in the background so opening stays fast.
	ftsSyncLimit = 2000
	ftsBatchSize = 500
	ftsCursorKey = "fts_rebuild_cursor"
)

// ftsRebuildNeeded reports whether the index is stale: the stored schema token
// is old, or the row counts drifted out of sync (self-heal). It also returns
// the number of messages.
func (db *DB) ftsRebuildNeeded() (bool, int64, error) {
	stored, err := db.GetSetting("fts_schema_version")
	if err != nil {
		return false, 0, err
	}
	var msgCount, ftsCount int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&msgCount); err != nil {
		return false, 0, err
	}
	if stored != ftsSchemaVersion {
		return true, msgCount, nil
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM messages_fts`).Scan(&ftsCount); err != nil {
		return false, 0, err
	}
	return msgCount != ftsCount, msgCount, nil
}

// migrateMessageFTS rebuilds the full-text index only when necessary. The index
// is kept current incrementally by upsertMessageFTS/deleteMessageFTS. Small
// caches are rebuilt here; a large cache is left for startFTSRebuild, because a
// full rebuild over a big mailbox takes seconds.
func (db *DB) migrateMessageFTS() error {
	need, n, err := db.ftsRebuildNeeded()
	if err != nil {
		return err
	}
	if !need {
		// A rebuild that stopped early and is no longer needed must not leave
		// its cursor for a later rebuild to resume from.
		if v, err := db.GetSetting(ftsCursorKey); err != nil || v == "" {
			return err
		}
		_, err := db.Exec(`DELETE FROM settings WHERE key = ?`, ftsCursorKey)
		return err
	}
	if n > ftsSyncLimit {
		db.ftsPending = true
		return nil
	}
	return db.rebuildMessageFTS(context.Background())
}

// startFTSRebuild rebuilds a stale index of a large cache in the background.
// Until it finishes, searches see the old index plus the rows already rebuilt.
// Progress is stored after every batch, so a short-lived command that exits
// early does not lose the work: the next open resumes where it stopped.
func (db *DB) startFTSRebuild() {
	if !db.ftsPending {
		return
	}
	db.ftsPending = false
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	db.stopFTS, db.ftsDone = cancel, done
	go func() {
		defer close(done)
		_ = db.rebuildMessageFTS(ctx) // retried on the next open
	}()
}

// Close stops a running background index rebuild, then closes the database.
func (db *DB) Close() error {
	if db.stopFTS != nil {
		db.stopFTS()
		<-db.ftsDone
	}
	return db.DB.Close()
}

// rebuildMessageFTS re-indexes every message in batches, each in its own
// transaction so searches and writes are not blocked for the whole rebuild.
// Rows are replaced in place rather than the index being emptied first.
//
// The stored cursor is "<schema version>:<last id>", so a rebuild for a newer
// schema never resumes from a cursor left by an older one.
func (db *DB) rebuildMessageFTS(ctx context.Context) error {
	var cursor int64
	if v, err := db.GetSetting(ftsCursorKey); err == nil {
		if version, id, ok := strings.Cut(v, ":"); ok && version == ftsSchemaVersion {
			cursor, _ = strconv.ParseInt(id, 10, 64)
		}
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		last, n, err := db.rebuildFTSBatch(ctx, cursor)
		if err != nil {
			return err
		}
		if n == 0 {
			break
		}
		cursor = last
		if err := db.SetSetting(ftsCursorKey, ftsSchemaVersion+":"+strconv.FormatInt(cursor, 10)); err != nil {
			return err
		}
	}
	if _, err := db.Exec(`DELETE FROM messages_fts WHERE rowid NOT IN (SELECT id FROM messages)`); err != nil {
		return err
	}
	if _, err := db.Exec(`DELETE FROM settings WHERE key = ?`, ftsCursorKey); err != nil {
		return err
	}
	return db.SetSetting("fts_schema_version", ftsSchemaVersion)
}

// rebuildFTSBatch re-indexes the next batch of messages after the given id.
// Bodies are read and their text extracted before the write transaction, so the
// single database connection is held only for the index writes. A message whose
// body changed in between is re-read inside the transaction.
func (db *DB) rebuildFTSBatch(ctx context.Context, after int64) (last int64, n int, err error) {
	type item struct {
		id                  int64
		plain, markup, text string
	}
	rows, err := db.QueryContext(ctx, `SELECT id, body_text, body_html FROM messages WHERE id > ? ORDER BY id LIMIT ?`, after, ftsBatchSize)
	if err != nil {
		return 0, 0, err
	}
	var items []item
	for rows.Next() {
		var it item
		if err = rows.Scan(&it.id, &it.plain, &it.markup); err != nil {
			rows.Close()
			return 0, 0, err
		}
		items = append(items, it)
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(items) == 0 {
		return 0, 0, err
	}
	for i := range items {
		if err = ctx.Err(); err != nil {
			return 0, 0, err
		}
		items[i].text, _ = bodytext.Resolve(items[i].plain, items[i].markup)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback() //nolint:errcheck
	for _, it := range items {
		var current bool
		err = tx.QueryRowContext(ctx, `SELECT body_text = ? AND body_html = ? FROM messages WHERE id = ?`, it.plain, it.markup, it.id).Scan(&current)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			err = nil // deleted since the read; its index row is cleaned up at the end
		case err != nil:
		case current:
			err = writeMessageFTS(tx, it.id, it.text)
		default:
			err = upsertMessageFTS(tx, it.id)
		}
		if err != nil {
			return 0, 0, err
		}
	}
	return items[len(items)-1].id, len(items), tx.Commit()
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
