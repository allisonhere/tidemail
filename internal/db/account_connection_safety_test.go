package db

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	// The name must survive URI escaping. Windows forbids "?" in file names.
	name := "mail ? # %.db"
	if runtime.GOOS == "windows" {
		name = "mail # %.db"
	}
	path := filepath.Join(t.TempDir(), name)
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

func TestDeleteAccountRemovesOwnedDraftsAttachmentsAndOutboxAtomically(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			d, err := OpenPath(filepath.Join(t.TempDir(), "mail.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { d.Close() })
			accountID, err := d.AddAccount("cfg-target", "Shared", "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := d.AddAccount("cfg-peer", "Shared", ""); err != nil {
				t.Fatal(err)
			}
			type ownedRow struct {
				draftID, outboxID int64
				remove            bool
			}
			var rows []ownedRow
			for _, tc := range []struct {
				configID, name, user string
				remove               bool
			}{
				{"cfg-target", "Old name", "old@example.com", true},
				{"", "Shared", "target@example.com", true},
				{"cfg-peer", "Shared", "target@example.com", false},
				{"", "Shared", "peer@example.com", false},
			} {
				draftID, err := d.SaveDraft(Draft{AccountConfigID: tc.configID, AccountName: tc.name, AccountUser: tc.user,
					Subject: "saved", Attachments: []DraftAttachment{{Filename: "data.txt", Data: []byte("data")}}})
				if err != nil {
					t.Fatal(err)
				}
				outboxID, err := d.EnqueueOutbox(OutboxItem{AccountConfigID: tc.configID, AccountName: tc.name,
					AccountUser: tc.user, DraftID: draftID, MessageJSON: []byte(`{}`), DraftJSON: []byte(`{}`)})
				if err != nil {
					t.Fatal(err)
				}
				rows = append(rows, ownedRow{draftID: draftID, outboxID: outboxID, remove: tc.remove})
			}
			if fail {
				if _, err := d.Exec(`CREATE TRIGGER fail_account_delete BEFORE DELETE ON accounts BEGIN SELECT RAISE(ABORT,'forced account failure'); END`); err != nil {
					t.Fatal(err)
				}
			}
			err = d.DeleteAccount(accountID, "target@example.com")
			if (err != nil) != fail {
				t.Fatalf("DeleteAccount error=%v, fail=%v", err, fail)
			}
			for _, row := range rows {
				want := 1
				if row.remove && !fail {
					want = 0
				}
				for _, check := range []struct {
					query string
					id    int64
				}{
					{`SELECT count(*) FROM drafts WHERE id = ?`, row.draftID},
					{`SELECT count(*) FROM draft_attachments WHERE draft_id = ?`, row.draftID},
					{`SELECT count(*) FROM outbox WHERE id = ?`, row.outboxID},
				} {
					var got int
					if err := d.QueryRow(check.query, check.id).Scan(&got); err != nil || got != want {
						t.Fatalf("%s id=%d: got %d, want %d, err=%v", check.query, check.id, got, want, err)
					}
				}
			}
		})
	}
}

// SQLite refuses a file: URI whose authority is not empty or "localhost". On
// Windows a drive-letter path used to land in the authority (file://C:/...),
// so TideMail could not open its database at all there.
func TestFileURIKeepsAuthorityEmpty(t *testing.T) {
	for _, tc := range []struct{ path, want string }{
		{"C:/Users/me/AppData/Roaming/tidemail/mail.db", "file:///C:/Users/me/AppData/Roaming/tidemail/mail.db"},
		{"D:/mail ? # %.db", "file:///D:/mail%20%3F%20%23%20%25.db"},
		{"/home/me/.local/share/tidemail/mail.db", "file:///home/me/.local/share/tidemail/mail.db"},
		{"//server/share/mail.db", "file:////server/share/mail.db"},
	} {
		got := fileURI(tc.path)
		if got != tc.want {
			t.Errorf("fileURI(%q) = %q, want %q", tc.path, got, tc.want)
		}
		u, err := url.Parse(got)
		if err != nil || u.Host != "" {
			t.Errorf("fileURI(%q) = %q has authority %q (err %v)", tc.path, got, u.Host, err)
		}
	}
}

func TestSQLiteDSNForFilePathHasNoAuthority(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mail.db")
	dsn, err := sqliteDSN(path)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "file" || u.Host != "" {
		t.Fatalf("dsn %q: scheme %q authority %q", dsn, u.Scheme, u.Host)
	}
	want := filepath.ToSlash(path)
	if !strings.HasPrefix(want, "/") {
		want = "/" + want // C:/... on Windows
	}
	if u.Path != want {
		t.Fatalf("dsn %q names %q, want %q", dsn, u.Path, want)
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
