package imap

import (
	"context"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
)

func backfillTestClient(t *testing.T, port int) *Client {
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

// TestFetchOlderThanPagesBackwards covers the core of the message-ceiling fix:
// the initial sync only caches the newest window, so paging back has to reach
// messages below the oldest UID already held.
func TestFetchOlderThanPagesBackwards(t *testing.T) {
	port, cleanup := startTestServer(t)
	defer cleanup()

	uids := make([]uint32, 0, 5)
	for range 5 {
		uids = append(uids, appendTestMessage(t, port, "INBOX", false))
	}
	client := backfillTestClient(t, port)

	// Pretend only the newest two are cached, so the cursor sits at uids[3].
	msgs, err := client.FetchOlderThan(context.Background(), "INBOX", uids[3], 10)
	if err != nil {
		t.Fatalf("FetchOlderThan: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected the 3 messages older than the cursor, got %d", len(msgs))
	}
	for _, m := range msgs {
		if m.UID >= uids[3] {
			t.Fatalf("FetchOlderThan returned UID %d at or above the cursor %d", m.UID, uids[3])
		}
	}
}

// The page size must cap the result, and it must take the newest of the older
// messages — those are the ones the user is about to scroll into.
func TestFetchOlderThanHonoursLimitFromTheTail(t *testing.T) {
	port, cleanup := startTestServer(t)
	defer cleanup()

	uids := make([]uint32, 0, 5)
	for range 5 {
		uids = append(uids, appendTestMessage(t, port, "INBOX", false))
	}
	client := backfillTestClient(t, port)

	msgs, err := client.FetchOlderThan(context.Background(), "INBOX", uids[4], 2)
	if err != nil {
		t.Fatalf("FetchOlderThan: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected the limit to cap the page at 2, got %d", len(msgs))
	}
	got := map[uint32]bool{}
	for _, m := range msgs {
		got[m.UID] = true
	}
	if !got[uids[2]] || !got[uids[3]] {
		t.Fatalf("expected the two newest below the cursor (%d, %d), got %v", uids[2], uids[3], got)
	}
}

// Reaching the start of the mailbox must be a clean empty result, not an error —
// that is what tells the UI to stop offering to load more.
func TestFetchOlderThanAtStartOfMailbox(t *testing.T) {
	port, cleanup := startTestServer(t)
	defer cleanup()

	first := appendTestMessage(t, port, "INBOX", false)
	client := backfillTestClient(t, port)

	msgs, err := client.FetchOlderThan(context.Background(), "INBOX", first, 10)
	if err != nil {
		t.Fatalf("FetchOlderThan at start: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected nothing older than the first message, got %d", len(msgs))
	}

	// A cursor of 1 (or 0) has nothing below it and must not hit the network.
	if msgs, err = client.FetchOlderThan(context.Background(), "INBOX", 1, 10); err != nil || msgs != nil {
		t.Fatalf("expected UID cursor 1 to short-circuit, got %d msgs, err %v", len(msgs), err)
	}
	if msgs, err = client.FetchOlderThan(context.Background(), "INBOX", 100, 0); err != nil || msgs != nil {
		t.Fatalf("expected a zero limit to short-circuit, got %d msgs, err %v", len(msgs), err)
	}
}
