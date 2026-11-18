package imap

import (
	"context"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
)

func TestZeroUIDBatchesAreRejectedWithoutPartialChanges(t *testing.T) {
	port, cleanup := startTestServer(t)
	defer cleanup()
	uid := appendTestMessage(t, port, "INBOX", false)
	client := New(config.AccountConfig{IMAPHost: "127.0.0.1", IMAPPort: port, User: "testuser", Password: "testpass"})
	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	for _, uids := range [][]uint32{{0}, {uid, 0}, {0, uid}} {
		for _, action := range []func() error{
			func() error { return client.MarkSeenUIDs(ctx, "INBOX", uids, true) },
			func() error { return client.MoveMessages(ctx, "INBOX", uids, "Archive") },
			func() error { return client.DeleteMessages(ctx, "INBOX", uids) },
		} {
			if err := action(); err == nil || !strings.Contains(err.Error(), "UID") {
				t.Fatalf("invalid batch %v: %v", uids, err)
			}
		}
	}
	if _, err := client.FetchRaw(ctx, "INBOX", 0); err == nil {
		t.Fatal("FetchRaw accepted UID zero")
	}
	state, _, err := client.ServerState(ctx, "INBOX")
	if err != nil || len(state) != 1 || state[0].UID != uid || state[0].Seen || state[0].Flagged {
		t.Fatalf("invalid batch changed mailbox: %+v, %v", state, err)
	}
	if set := uidSetOf([]uint32{0, uid, 0}); set.String() != uidSetOf([]uint32{uid}).String() {
		t.Fatalf("UID builder included wildcard: %s", set.String())
	}
	if set := uidSetOf([]uint32{0}); len(set) != 0 {
		t.Fatalf("zero-only set: %v", set)
	}
}
