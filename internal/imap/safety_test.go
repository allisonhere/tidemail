package imap

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
)

func TestFetchSincePagePreservesReadState(t *testing.T) {
	port, cleanup := startTestServer(t)
	defer cleanup()
	unread := appendTestMessage(t, port, "INBOX", false)
	read := appendTestMessage(t, port, "INBOX", true)
	want := map[uint32]bool{unread: false, read: true}

	c := New(config.AccountConfig{IMAPHost: "127.0.0.1", IMAPPort: port, User: "testuser", Password: "testpass"})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	msgs, more, err := c.FetchSincePage(ctx, "INBOX", time.Now().Add(-24*time.Hour), 10, nil)
	if err != nil || more || len(msgs) != len(want) {
		t.Fatalf("fetch got %d messages, more=%v, err=%v", len(msgs), more, err)
	}
	for _, msg := range msgs {
		if seen, ok := want[msg.UID]; !ok || msg.Read != seen {
			t.Errorf("fetched UID %d has read=%v, want %v", msg.UID, msg.Read, seen)
		}
		if !strings.Contains(msg.BodyText, "Hello world") || !strings.Contains(msg.RawHeaders, "Subject: Test") {
			t.Errorf("fetched UID %d is missing its body or headers", msg.UID)
		}
	}
	state, _, err := c.ServerState(ctx, "INBOX")
	if err != nil || len(state) != len(want) {
		t.Fatalf("server state got %d messages, err=%v", len(state), err)
	}
	for _, msg := range state {
		if seen, ok := want[msg.UID]; !ok || msg.Seen != seen {
			t.Errorf("server UID %d has seen=%v, want %v", msg.UID, msg.Seen, seen)
		}
	}
}

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
