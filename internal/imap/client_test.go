package imap

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/allisonhere/tide/internal/config"
	"github.com/emersion/go-imap/v2"
	imapclient "github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
)

// startTestServer creates an in-memory IMAP server on a random port
// and returns the port and a cleanup function.
func startTestServer(t *testing.T) (int, func()) {
	t.Helper()

	memServer := imapmemserver.New()
	user := imapmemserver.NewUser("testuser", "testpass")
	memServer.AddUser(user)

	// Create mailboxes
	if err := user.Create("INBOX", nil); err != nil {
		t.Fatalf("create INBOX: %v", err)
	}
	if err := user.Create("Sent", nil); err != nil {
		t.Fatalf("create Sent: %v", err)
	}
	if err := user.Create("Archive", nil); err != nil {
		t.Fatalf("create Archive: %v", err)
	}

	be := &imapserver.Options{
		NewSession: func(conn *imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return memServer.NewSession(), nil, nil
		},
		InsecureAuth: true,
	}

	s := imapserver.New(be)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	go func() {
		s.Serve(ln) //nolint:errcheck
	}()

	port := ln.Addr().(*net.TCPAddr).Port

	cleanup := func() {
		s.Close()
		ln.Close()
	}

	return port, cleanup
}

// appendTestMessage appends a simple message to a mailbox using a raw imapclient.
func appendTestMessage(t *testing.T, port int, mailbox string, seen bool) uint32 {
	t.Helper()

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("dial for append: %v", err)
	}
	defer conn.Close()

	client := imapclient.New(conn, nil)
	defer client.Close()

	if err := client.Login("testuser", "testpass").Wait(); err != nil {
		t.Fatalf("login for append: %v", err)
	}

	var flags []imap.Flag
	if seen {
		flags = append(flags, imap.FlagSeen)
	}

	body := fmt.Sprintf("Subject: Test %d\r\n\r\nHello world %d", time.Now().UnixNano(), time.Now().UnixNano())
	cmd := client.Append(mailbox, int64(len(body)), &imap.AppendOptions{
		Flags: flags,
	})
	_, _ = cmd.Write([]byte(body))
	_ = cmd.Close()
	appendData, err := cmd.Wait()
	if err != nil {
		t.Fatalf("append: %v", err)
	}

	return uint32(appendData.UID)
}

func TestConnect_OK(t *testing.T) {
	port, cleanup := startTestServer(t)
	defer cleanup()

	client := New(config.AccountConfig{
		IMAPHost: "127.0.0.1",
		IMAPPort: port,
		User:     "testuser",
		Password: "testpass",
	})

	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	if client.conn == nil {
		t.Fatal("client.conn is nil after Connect")
	}
}

func TestConnect_BadAuth(t *testing.T) {
	port, cleanup := startTestServer(t)
	defer cleanup()

	client := New(config.AccountConfig{
		IMAPHost: "127.0.0.1",
		IMAPPort: port,
		User:     "testuser",
		Password: "wrongpass",
	})

	err := client.Connect(context.Background())
	if err == nil {
		t.Fatal("expected error for bad auth, got nil")
	}
}

func TestConnect_BadHost(t *testing.T) {
	client := New(config.AccountConfig{
		IMAPHost: "127.0.0.1",
		IMAPPort: 1,
		User:     "testuser",
		Password: "testpass",
	})

	err := client.Connect(context.Background())
	if err == nil {
		t.Fatal("expected error for bad host, got nil")
	}
}

func TestListMailboxes(t *testing.T) {
	port, cleanup := startTestServer(t)
	defer cleanup()

	client := New(config.AccountConfig{
		IMAPHost: "127.0.0.1",
		IMAPPort: port,
		User:     "testuser",
		Password: "testpass",
	})

	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	mailboxes, err := client.ListMailboxes(context.Background())
	if err != nil {
		t.Fatalf("ListMailboxes failed: %v", err)
	}

	names := make(map[string]bool)
	for _, mb := range mailboxes {
		names[mb.Name] = true
	}

	if !names["INBOX"] {
		t.Errorf("expected INBOX in list, got %v", names)
	}
	if !names["Sent"] {
		t.Errorf("expected Sent in list, got %v", names)
	}
	if !names["Archive"] {
		t.Errorf("expected Archive in list, got %v", names)
	}
}

func TestFetchMessages_Empty(t *testing.T) {
	port, cleanup := startTestServer(t)
	defer cleanup()

	client := New(config.AccountConfig{
		IMAPHost: "127.0.0.1",
		IMAPPort: port,
		User:     "testuser",
		Password: "testpass",
	})

	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	msgs, err := client.FetchMessages(context.Background(), "INBOX", 10)
	if err != nil {
		t.Fatalf("FetchMessages failed: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
}

func TestFetchMessages_WithMessages(t *testing.T) {
	port, cleanup := startTestServer(t)
	defer cleanup()

	// Pre-populate messages using a raw IMAP client
	appendTestMessage(t, port, "INBOX", false)
	uid := appendTestMessage(t, port, "INBOX", true)
	_ = uid

	client := New(config.AccountConfig{
		IMAPHost: "127.0.0.1",
		IMAPPort: port,
		User:     "testuser",
		Password: "testpass",
	})

	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	msgs, err := client.FetchMessages(context.Background(), "INBOX", 10)
	if err != nil {
		t.Fatalf("FetchMessages failed: %v", err)
	}
	if len(msgs) != 2 {
		t.Errorf("expected 2 messages, got %d", len(msgs))
	}
}

func TestMarkSeen(t *testing.T) {
	port, cleanup := startTestServer(t)
	defer cleanup()

	uid := appendTestMessage(t, port, "INBOX", false)

	client := New(config.AccountConfig{
		IMAPHost: "127.0.0.1",
		IMAPPort: port,
		User:     "testuser",
		Password: "testpass",
	})

	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	if err := client.MarkSeen(context.Background(), "INBOX", uid, true); err != nil {
		t.Fatalf("MarkSeen failed: %v", err)
	}

	// Verify by fetching and checking flags
	msgs, err := client.FetchMessages(context.Background(), "INBOX", 10)
	if err != nil {
		t.Fatalf("FetchMessages after MarkSeen: %v", err)
	}
	found := false
	for _, msg := range msgs {
		if msg.UID == uid {
			found = true
			if !msg.Read {
				t.Error("expected message to be marked seen")
			}
		}
	}
	if !found {
		t.Error("expected to find the message")
	}
}

func TestMoveMessage(t *testing.T) {
	port, cleanup := startTestServer(t)
	defer cleanup()

	uid := appendTestMessage(t, port, "INBOX", false)

	client := New(config.AccountConfig{
		IMAPHost: "127.0.0.1",
		IMAPPort: port,
		User:     "testuser",
		Password: "testpass",
	})

	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	if err := client.MoveMessage(context.Background(), "INBOX", uid, "Archive"); err != nil {
		t.Fatalf("MoveMessage failed: %v", err)
	}

	// Verify: message should be gone from INBOX
	msgs, err := client.FetchMessages(context.Background(), "INBOX", 10)
	if err != nil {
		t.Fatalf("FetchMessages after Move: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages in INBOX after move, got %d", len(msgs))
	}
}

func TestDeleteMessage(t *testing.T) {
	port, cleanup := startTestServer(t)
	defer cleanup()

	uid := appendTestMessage(t, port, "INBOX", false)

	client := New(config.AccountConfig{
		IMAPHost: "127.0.0.1",
		IMAPPort: port,
		User:     "testuser",
		Password: "testpass",
	})

	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	if err := client.DeleteMessage(context.Background(), "INBOX", uid); err != nil {
		t.Fatalf("DeleteMessage failed: %v", err)
	}

	// Verify: message should be gone
	msgs, err := client.FetchMessages(context.Background(), "INBOX", 10)
	if err != nil {
		t.Fatalf("FetchMessages after Delete: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages after delete, got %d", len(msgs))
	}
}

func TestClose_NotConnected(t *testing.T) {
	client := New(config.AccountConfig{})
	err := client.Close()
	if err != nil {
		t.Errorf("expected nil for Close when not connected, got %v", err)
	}
}

func TestDoubleClose(t *testing.T) {
	port, cleanup := startTestServer(t)
	defer cleanup()

	client := New(config.AccountConfig{
		IMAPHost: "127.0.0.1",
		IMAPPort: port,
		User:     "testuser",
		Password: "testpass",
	})

	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	if err := client.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}
}

func TestNotConnectedError(t *testing.T) {
	client := New(config.AccountConfig{})

	_, err := client.ListMailboxes(context.Background())
	if err == nil {
		t.Fatal("expected error for ListMailboxes when not connected")
	}

	_, err = client.FetchMessages(context.Background(), "INBOX", 10)
	if err == nil {
		t.Fatal("expected error for FetchMessages when not connected")
	}

	err = client.MarkSeen(context.Background(), "INBOX", 1, true)
	if err == nil {
		t.Fatal("expected error for MarkSeen when not connected")
	}

	err = client.MoveMessage(context.Background(), "INBOX", 1, "Archive")
	if err == nil {
		t.Fatal("expected error for MoveMessage when not connected")
	}

	err = client.DeleteMessage(context.Background(), "INBOX", 1)
	if err == nil {
		t.Fatal("expected error for DeleteMessage when not connected")
	}
}
