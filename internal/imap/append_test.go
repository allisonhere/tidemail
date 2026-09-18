package imap

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
)

func newAppendTestClient(t *testing.T, port int) *Client {
	t.Helper()
	client := New(config.AccountConfig{
		IMAPHost: "127.0.0.1",
		IMAPPort: port,
		User:     "testuser",
		Password: "testpass",
	})
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

// SMTP submission leaves no copy behind, so the Sent folder only has what we
// put there.
func TestAppendSentStoresAReadableCopy(t *testing.T) {
	port, cleanup := startTestServer(t)
	defer cleanup()
	client := newAppendTestClient(t, port)

	raw := "From: me@example.com\r\n" +
		"To: you@example.com\r\n" +
		"Subject: Appended copy\r\n" +
		"\r\n" +
		"body text\r\n"
	if err := client.AppendSent(context.Background(), "Sent", []byte(raw), time.Now()); err != nil {
		t.Fatalf("AppendSent: %v", err)
	}

	msgs, err := client.FetchMessages(context.Background(), "Sent", 10)
	if err != nil {
		t.Fatalf("FetchMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("got %d messages in Sent, want 1", len(msgs))
	}
	if !strings.Contains(msgs[0].Subject, "Appended copy") {
		t.Fatalf("subject = %q, want the appended message", msgs[0].Subject)
	}
	// Mail you just sent is not unread.
	if !msgs[0].Read {
		t.Fatal("the appended copy should be flagged \\Seen")
	}
}

// The riskiest failure mode: a rejected APPEND must not wedge the connection,
// because it is pooled and every later operation on the account reuses it.
func TestAppendSentFailureLeavesConnectionUsable(t *testing.T) {
	port, cleanup := startTestServer(t)
	defer cleanup()
	client := newAppendTestClient(t, port)

	raw := []byte("Subject: nowhere\r\n\r\nbody\r\n")
	if err := client.AppendSent(context.Background(), "NoSuchFolder", raw, time.Now()); err == nil {
		t.Fatal("expected an error appending to a mailbox that does not exist")
	}

	// The session must still work afterwards.
	if _, err := client.ListMailboxes(context.Background()); err != nil {
		t.Fatalf("connection unusable after a failed append: %v", err)
	}
	if err := client.AppendSent(context.Background(), "Sent", raw, time.Now()); err != nil {
		t.Fatalf("append after a failed append: %v", err)
	}
}

func TestAppendSentRejectsEmptyMessage(t *testing.T) {
	port, cleanup := startTestServer(t)
	defer cleanup()
	client := newAppendTestClient(t, port)

	if err := client.AppendSent(context.Background(), "Sent", nil, time.Now()); err == nil {
		t.Fatal("expected an error for an empty message")
	}
}
