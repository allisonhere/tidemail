package imap

import (
	"context"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/emersion/go-imap/v2"
)

func connectTest(t *testing.T) (*Client, func()) {
	t.Helper()
	port, cleanup := startTestServer(t)
	client := New(config.AccountConfig{IMAPHost: "127.0.0.1", IMAPPort: port, User: "testuser", Password: "testpass"})
	if err := client.Connect(context.Background()); err != nil {
		cleanup()
		t.Fatalf("Connect failed: %v", err)
	}
	return client, func() { client.Close(); cleanup() }
}

func mailboxNames(t *testing.T, c *Client) map[string]bool {
	t.Helper()
	list, err := c.ListMailboxes(context.Background())
	if err != nil {
		t.Fatalf("ListMailboxes: %v", err)
	}
	names := map[string]bool{}
	for _, mb := range list {
		names[mb.Name] = true
	}
	return names
}

func TestCreateRenameDeleteMailbox(t *testing.T) {
	c, done := connectTest(t)
	defer done()
	ctx := context.Background()

	if err := c.CreateMailbox(ctx, "Receipts"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if !mailboxNames(t, c)["Receipts"] {
		t.Fatal("Receipts missing after create")
	}
	if err := c.RenameMailbox(ctx, "Receipts", "Bills"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	names := mailboxNames(t, c)
	if names["Receipts"] || !names["Bills"] {
		t.Fatalf("rename did not take effect: %v", names)
	}
	if err := c.DeleteMailbox(ctx, "Bills"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if mailboxNames(t, c)["Bills"] {
		t.Fatal("Bills still listed after delete")
	}
	if err := c.RenameMailbox(ctx, "Nope", "Other"); err == nil {
		t.Fatal("renaming a missing mailbox should fail")
	}
}

func TestUnseenCounts(t *testing.T) {
	c, done := connectTest(t)
	defer done()
	ctx := context.Background()
	msg := []byte("From: a@example.com\r\nTo: b@example.com\r\nSubject: hi\r\nMessage-ID: <1@x>\r\n\r\nbody\r\n")
	if err := c.AppendWithFlags(ctx, "INBOX", msg, time.Now()); err != nil { // unread
		t.Fatalf("append: %v", err)
	}
	if err := c.AppendWithFlags(ctx, "INBOX", msg, time.Now(), imap.FlagSeen); err != nil { // read
		t.Fatalf("append: %v", err)
	}
	counts, err := c.UnseenCounts(ctx, []string{"INBOX", "Sent", "Does-not-exist"})
	if err != nil {
		t.Fatalf("UnseenCounts: %v", err)
	}
	if counts["INBOX"] != 1 {
		t.Errorf("INBOX unseen = %d, want 1", counts["INBOX"])
	}
	if counts["Sent"] != 0 {
		t.Errorf("Sent unseen = %d, want 0", counts["Sent"])
	}
	if _, ok := counts["Does-not-exist"]; ok {
		t.Error("a mailbox the server rejects should be left out, not reported as zero")
	}
}
