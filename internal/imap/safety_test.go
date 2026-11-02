package imap

import (
	"context"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
)

func TestIncrementalPagesBoundBodiesAndCoverBacklog(t *testing.T) {
	port, cleanup := startTestServer(t)
	defer cleanup()
	for range 7 {
		appendTestMessage(t, port, "INBOX", false)
	}
	c := New(config.AccountConfig{IMAPHost: "127.0.0.1", IMAPPort: port, User: "testuser", Password: "testpass"})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	known := map[uint32]bool{}
	for page := 0; page < 4; page++ {
		msgs, more, err := c.FetchSincePage(ctx, "INBOX", time.Now().Add(-24*time.Hour), 2, known)
		if err != nil {
			t.Fatal(err)
		}
		if len(msgs) > 2 || len(msgs) == 0 {
			t.Fatalf("page %d fetched %d", page, len(msgs))
		}
		for _, m := range msgs {
			if known[m.UID] {
				t.Fatal("repeated UID", m.UID)
			}
			known[m.UID] = true
		}
		if more != (page < 3) {
			t.Fatalf("page %d more=%v", page, more)
		}
	}
	if len(known) != 7 {
		t.Fatalf("backlog skipped: %v", known)
	}
	msgs, more, err := c.FetchSincePage(ctx, "INBOX", time.Now().Add(-24*time.Hour), 2, known)
	if err != nil || more || len(msgs) != 0 {
		t.Fatalf("completed backlog: %v %v %v", msgs, more, err)
	}
}

func TestMarkAllSeenIncludesUncachedMessages(t *testing.T) {
	port, cleanup := startTestServer(t)
	defer cleanup()
	for range 4 {
		appendTestMessage(t, port, "INBOX", false)
	}
	c := New(config.AccountConfig{IMAPHost: "127.0.0.1", IMAPPort: port, User: "testuser", Password: "testpass"})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if msgs, err := c.FetchMessages(ctx, "INBOX", 1); err != nil || len(msgs) != 1 {
		t.Fatalf("initial page %v %v", msgs, err)
	}
	if err := c.MarkAllSeen(ctx, "INBOX"); err != nil {
		t.Fatal(err)
	}
	msgs, _, err := c.ServerState(ctx, "INBOX")
	if err != nil || len(msgs) != 4 {
		t.Fatalf("state %v %v", msgs, err)
	}
	for _, m := range msgs {
		if !m.Seen {
			t.Fatal("server message still unread", m.UID)
		}
	}
}

func TestIncrementalPagesIncludeOldDatedMoves(t *testing.T) {
	port, cleanup := startTestServer(t)
	defer cleanup()
	for range 7 {
		appendTestMessage(t, port, "INBOX", false)
	}
	c := New(config.AccountConfig{IMAPHost: "127.0.0.1", IMAPPort: port, User: "testuser", Password: "testpass"})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	known := map[uint32]bool{1: true}
	// All message dates precede this watermark, as with a moved older message.
	for page := 0; page < 3; page++ {
		msgs, more, err := c.FetchSincePage(ctx, "INBOX", time.Now().Add(48*time.Hour), 2, known)
		if err != nil || len(msgs) != 2 || more != (page < 2) {
			t.Fatalf("page %d: %v more=%v err=%v", page, msgs, more, err)
		}
		for _, msg := range msgs {
			if known[msg.UID] {
				t.Fatal("duplicate UID")
			}
			known[msg.UID] = true
		}
	}
	if len(known) != 7 {
		t.Fatal("old-dated moves skipped", known)
	}
}
