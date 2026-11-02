package db

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func assertConnectionPragmas(t *testing.T, d *DB) {
	t.Helper()
	for pragma, want := range map[string]string{"foreign_keys": "1", "busy_timeout": "5000", "journal_mode": "wal"} {
		var got string
		if err := d.QueryRow("PRAGMA " + pragma).Scan(&got); err != nil || got != want {
			t.Fatalf("%s=%q want %q err=%v", pragma, got, want, err)
		}
	}
}

func discardSQLiteConnection(t *testing.T, d *DB) {
	t.Helper()
	conn, err := d.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	err = conn.Raw(func(any) error { return driver.ErrBadConn })
	_ = conn.Close()
	if !errors.Is(err, driver.ErrBadConn) {
		t.Fatalf("discard connection: %v", err)
	}
	if d.Stats().OpenConnections != 0 {
		t.Fatal("test did not discard physical connection")
	}
}

func TestOpenPathSettingsSurviveConnectionReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mail ? # %.db")
	d, err := OpenPath(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("database created under wrong filename: %v", err)
	}
	assertConnectionPragmas(t, d)
	discardSQLiteConnection(t, d)
	assertConnectionPragmas(t, d)
	account, err := d.AddAccount("replace", "Replacement", "")
	if err != nil {
		t.Fatal(err)
	}
	mailbox, err := d.UpsertMailbox(Mailbox{AccountID: account, Name: "INBOX"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.UpsertMessage(Message{MailboxID: mailbox, UID: 1, Subject: "replacement"}); err != nil {
		t.Fatal(err)
	}
	discardSQLiteConnection(t, d)
	if err := d.DeleteAccount(account); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := d.QueryRow(`SELECT count(*) FROM messages`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("replacement connection lost cascade: count=%d err=%v", count, err)
	}
	if err := d.QueryRow(`SELECT count(*) FROM messages_fts`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("orphan FTS after replacement: count=%d err=%v", count, err)
	}
}

func TestDeleteAccountFTSCleanupIsAtomicAndScoped(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			d, err := OpenPath(filepath.Join(t.TempDir(), "mail.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			var accounts, ids []int64
			for i := 0; i < 2; i++ {
				account, err := d.AddAccount(fmt.Sprint(i), fmt.Sprint(i), "")
				if err != nil {
					t.Fatal(err)
				}
				accounts = append(accounts, account)
				for _, name := range []string{"INBOX", "Archive"} {
					mailbox, err := d.UpsertMailbox(Mailbox{AccountID: account, Name: name})
					if err != nil {
						t.Fatal(err)
					}
					if err := d.UpsertMessage(Message{MailboxID: mailbox, UID: 1, Subject: fmt.Sprintf("account %d", i)}); err != nil {
						t.Fatal(err)
					}
					id, err := d.MessageIDByUID(mailbox, 1)
					if err != nil {
						t.Fatal(err)
					}
					ids = append(ids, id)
					if _, err := d.SaveAttachment(id, Attachment{Filename: "file", Data: []byte("content")}); err != nil {
						t.Fatal(err)
					}
				}
			}
			if fail {
				if _, err := d.Exec(`CREATE TRIGGER fail_account_delete BEFORE DELETE ON accounts BEGIN SELECT RAISE(ABORT,'forced account failure'); END`); err != nil {
					t.Fatal(err)
				}
			}
			err = d.DeleteAccount(accounts[0])
			if (err != nil) != fail {
				t.Fatalf("delete error: %v fail=%v", err, fail)
			}
			for i, id := range ids {
				want := 1
				if i < 2 && !fail {
					want = 0
				}
				for _, query := range []string{`SELECT count(*) FROM messages WHERE id=?`, `SELECT count(*) FROM messages_fts WHERE rowid=?`, `SELECT count(*) FROM attachments WHERE message_id=?`} {
					var count int
					if err := d.QueryRow(query, id).Scan(&count); err != nil || count != want {
						t.Fatalf("id=%d query=%s count=%d want=%d err=%v", id, query, count, want, err)
					}
				}
			}
			if fail {
				if _, err := d.Exec(`DROP TRIGGER fail_account_delete`); err != nil {
					t.Fatal(err)
				}
				if err := d.DeleteAccount(accounts[0]); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestOpenPathMemoryCompatibility(t *testing.T) {
	for _, path := range []string{":memory:", "file:settings-test?mode=memory&cache=shared"} {
		t.Run(path, func(t *testing.T) {
			d, err := OpenPath(path)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			var enabled, timeout int
			if err := d.QueryRow(`PRAGMA foreign_keys`).Scan(&enabled); err != nil || enabled != 1 {
				t.Fatalf("foreign keys: %d %v", enabled, err)
			}
			if err := d.QueryRow(`PRAGMA busy_timeout`).Scan(&timeout); err != nil || timeout != 5000 {
				t.Fatalf("busy timeout: %d %v", timeout, err)
			}
		})
	}
}
