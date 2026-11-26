package ui

import (
	"context"
	"database/sql"
	"net"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	imapClient "github.com/allisonhere/tidemail/internal/imap"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
)

func TestSyncRecordsServerCountOnlyWhenTheCacheIsUpdated(t *testing.T) {
	backend := imapmemserver.New()
	user := imapmemserver.NewUser("test", "password")
	backend.AddUser(user)
	for _, name := range []string{"INBOX", "Reading"} {
		if err := user.Create(name, nil); err != nil {
			t.Fatal(err)
		}
	}
	server := imapserver.New(&imapserver.Options{InsecureAuth: true, NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
		return backend.NewSession(), nil, nil
	}})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close(); _ = listener.Close() })

	m, database, messages := newAnnotationDBModel(t, t.TempDir(), 1)
	t.Cleanup(m.sessions.Close)
	mailboxID := messages[0].MailboxID
	cfg := config.AccountConfig{Name: "Personal", IMAPHost: "127.0.0.1", IMAPPort: listener.Addr().(*net.TCPAddr).Port, User: "test", Password: "password"}
	m.cfg.Accounts = []config.AccountConfig{cfg}

	deliver := func(subject string, at time.Time) {
		t.Helper()
		c := imapClient.New(cfg)
		if err := c.Connect(context.Background()); err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		raw := []byte("From: a@example.com\r\nTo: b@example.com\r\nSubject: " + subject + "\r\nDate: " + at.Format(time.RFC1123Z) + "\r\n\r\nbody\r\n")
		if err := c.AppendWithFlags(context.Background(), "INBOX", raw, at); err != nil {
			t.Fatal(err)
		}
	}
	stored := func() (sql.NullInt64, sql.NullInt64) {
		t.Helper()
		var n, at sql.NullInt64
		if err := database.QueryRow(`SELECT server_count, server_count_at FROM mailboxes WHERE id = ?`, mailboxID).Scan(&n, &at); err != nil {
			t.Fatal(err)
		}
		return n, at
	}
	sync := func() MailboxSyncedMsg {
		t.Helper()
		m.syncing[mailboxID] = false
		return m.syncMailboxCmd(mailboxID, false)().(MailboxSyncedMsg)
	}

	deliver("one", time.Unix(1000, 0))
	deliver("two", time.Unix(1000, 0))
	if n, _ := stored(); n.Valid {
		t.Fatal("server count recorded before any sync")
	}
	if result := sync(); result.Err != nil {
		t.Fatal(result.Err)
	}
	n, at := stored()
	if !n.Valid || n.Int64 != 2 || !at.Valid {
		t.Fatalf("after sync: count=%v at=%v", n, at)
	}

	// In each case below new mail is waiting and one step of the sync
	// fails, so a count written anyway would differ from the stored one.
	refuse := func(when string, step func()) {
		t.Helper()
		if _, err := database.Exec(`CREATE TRIGGER refuse ` + when + ` BEGIN SELECT RAISE(ABORT, 'refused'); END`); err != nil {
			t.Fatal(err)
		}
		step()
		if _, err := database.Exec(`DROP TRIGGER refuse`); err != nil {
			t.Fatal(err)
		}
		if n2, at2 := stored(); n2 != n || at2 != at {
			t.Fatalf("%s changed the count: %v %v, want %v %v", when, n2, at2, n, at)
		}
	}

	// The reconcile cannot drop a message the server no longer has. The
	// sync itself still succeeds.
	if err := database.UpsertMessage(db.Message{MailboxID: mailboxID, UID: 99, Subject: "gone from the server"}); err != nil {
		t.Fatal(err)
	}
	deliver("three", time.Now())
	refuse("BEFORE DELETE ON messages", func() {
		if result := sync(); result.Err != nil {
			t.Fatal(result.Err)
		}
	})
	// Storing new mail fails.
	deliver("four", time.Now())
	refuse("BEFORE INSERT ON messages", func() {
		if result := sync(); result.Err == nil {
			t.Fatal("sync succeeded although the message could not be stored")
		}
	})
	// A bookkeeping write fails, so the sync is reported as failed.
	deliver("five", time.Now())
	refuse("BEFORE UPDATE OF unread_count ON mailboxes", func() {
		if result := sync(); result.Err == nil {
			t.Fatal("sync succeeded although the unread count could not be saved")
		}
	})

	if result := sync(); result.Err != nil {
		t.Fatal(result.Err)
	}
	if n, at = stored(); n.Int64 != 5 {
		t.Fatalf("after recovery: count=%v, want 5", n)
	}

	// The server renumbers the folder, but emptying the cache fails, so it
	// still holds messages under the old numbering.
	validity, err := database.MailboxUIDValidity(mailboxID)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.SetMailboxUIDValidity(mailboxID, validity+1); err != nil {
		t.Fatal(err)
	}
	deliver("six", time.Now())
	refuse("BEFORE DELETE ON messages", func() {
		if result := sync(); result.Err != nil {
			t.Fatal(result.Err)
		}
	})
	// The change is not forgotten, so the next sync retries the reset.
	if got, err := database.MailboxUIDValidity(mailboxID); err != nil || got != validity+1 {
		t.Fatalf("after a failed reset: UIDVALIDITY=%d err=%v, want %d kept", got, err, validity+1)
	}

	// Once the cache can be emptied, it is refetched and the count follows.
	if result := sync(); result.Err != nil {
		t.Fatal(result.Err)
	}
	n5, at5 := stored()
	if n5.Int64 != 6 || at5.Int64 < at.Int64 {
		t.Fatalf("after renumbering: count=%v at=%v, want 6 at or after %v", n5, at5, at)
	}
	if cached, err := database.CountMessages(mailboxID); err != nil || cached != 6 {
		t.Fatalf("after renumbering: cached=%d err=%v, want 6", cached, err)
	}
}
