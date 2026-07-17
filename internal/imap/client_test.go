package imap

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tide/internal/config"
	"github.com/emersion/go-imap/v2"
	imapclient "github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
)

// TestApplyDeadlineHonorsContext verifies applyDeadline pushes the ctx deadline
// down onto the transport so a blocked socket read fails instead of hanging
// (go-imap commands take no ctx, so the socket deadline is the only lever). An
// already-expired ctx must make the very next read return immediately.
func TestApplyDeadlineHonorsContext(t *testing.T) {
	local, remote := net.Pipe()
	defer local.Close()
	defer remote.Close()
	c := &Client{netConn: local}

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	clear := c.applyDeadline(ctx)
	defer clear()

	buf := make([]byte, 1)
	done := make(chan error, 1)
	go func() {
		_, err := local.Read(buf)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a deadline error from a read past the ctx deadline")
		}
	case <-time.After(time.Second):
		t.Fatal("read blocked despite an expired context deadline")
	}
}

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

func TestServerStateReportsSeenAndFlagged(t *testing.T) {
	port, cleanup := startTestServer(t)
	defer cleanup()

	seenUID := appendTestMessage(t, port, "INBOX", true)
	flaggedUID := appendTestMessage(t, port, "INBOX", false)

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

	if err := client.MarkFlagged(context.Background(), "INBOX", flaggedUID, true); err != nil {
		t.Fatalf("MarkFlagged failed: %v", err)
	}
	state, _, err := client.ServerState(context.Background(), "INBOX")
	if err != nil {
		t.Fatalf("ServerState failed: %v", err)
	}
	byUID := make(map[uint32]ServerMessage, len(state))
	for _, msg := range state {
		byUID[msg.UID] = msg
	}
	if !byUID[seenUID].Seen {
		t.Fatal("expected server state to report seen flag")
	}
	if byUID[seenUID].Flagged {
		t.Fatal("expected seen-only message not to be flagged")
	}
	if !byUID[flaggedUID].Flagged {
		t.Fatal("expected server state to report flagged state")
	}

	if err := client.MarkFlagged(context.Background(), "INBOX", flaggedUID, false); err != nil {
		t.Fatalf("unflag failed: %v", err)
	}
	state, _, err = client.ServerState(context.Background(), "INBOX")
	if err != nil {
		t.Fatalf("ServerState after unflag failed: %v", err)
	}
	found := false
	for _, msg := range state {
		if msg.UID == flaggedUID {
			found = true
			if msg.Flagged {
				t.Fatal("expected server state to report removed flag")
			}
		}
	}
	if !found {
		t.Fatal("expected unflagged message in server state")
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

// startGmailTrashQuirkServer speaks just enough IMAP to drive LOGIN, SELECT and
// UID MOVE, replying to the MOVE with a tagged OK carrying Gmail's malformed
// `[COPYUID 1 1 0]` resp-code. go-imap rejects the `0` while parsing the OK
// response, so a faithful reproduction needs a raw server rather than
// imapmemserver (which always returns a well-formed COPYUID).
func startGmailTrashQuirkServer(t *testing.T) (int, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		write := func(s string) { _, _ = conn.Write([]byte(s)) }
		write("* OK [CAPABILITY IMAP4rev1 MOVE UIDPLUS] ready\r\n")
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			tag, cmd := fields[0], strings.ToUpper(fields[1])
			switch {
			case cmd == "LOGIN":
				write(tag + " OK LOGIN completed\r\n")
			case cmd == "CAPABILITY":
				// Advertise MOVE so go-imap issues a real UID MOVE rather than
				// falling back to COPY+STORE+EXPUNGE — only the MOVE path
				// surfaces Gmail's malformed COPYUID this test reproduces.
				write("* CAPABILITY IMAP4rev1 MOVE UIDPLUS\r\n")
				write(tag + " OK CAPABILITY completed\r\n")
			case cmd == "SELECT":
				write("* 1 EXISTS\r\n")
				write("* OK [UIDVALIDITY 1] .\r\n")
				write("* OK [UIDNEXT 2] .\r\n")
				write("* FLAGS (\\Seen \\Deleted)\r\n")
				write("* OK [PERMANENTFLAGS (\\Seen \\Deleted)] .\r\n")
				write(tag + " OK [READ-WRITE] SELECT completed\r\n")
			case cmd == "UID" && len(fields) >= 3 && strings.ToUpper(fields[2]) == "MOVE":
				// The move succeeds server-side; the OK carries Gmail's
				// malformed COPYUID with a destination set of "0".
				write("* 1 EXPUNGE\r\n")
				write(tag + " OK [COPYUID 1 1 0] MOVE completed\r\n")
			case cmd == "LOGOUT":
				write("* BYE logging out\r\n")
				write(tag + " OK LOGOUT completed\r\n")
				return
			default:
				write(tag + " OK completed\r\n")
			}
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, func() { ln.Close() }
}

// TestMoveMessages_GmailTrashCopyUIDQuirk verifies that a MOVE which the server
// completes is reported as success even when go-imap fails to parse Gmail's
// malformed COPYUID resp-code — otherwise a deleted message wrongly looks like
// "delete failed" and stays visible.
func TestMoveMessages_GmailTrashCopyUIDQuirk(t *testing.T) {
	port, cleanup := startGmailTrashQuirkServer(t)
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

	if err := client.MoveMessages(context.Background(), "INBOX", []uint32{1}, "[Gmail]/Trash"); err != nil {
		t.Fatalf("MoveMessages should treat the COPYUID parse failure as success, got: %v", err)
	}
}

func TestIsCopyUIDParseError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"copyuid parse", fmt.Errorf("in resp-code-copy: imap: bad number set value %q", "0"), true},
		{"other error", fmt.Errorf("move to Archive: connection reset"), false},
	}
	for _, tc := range cases {
		if got := isCopyUIDParseError(tc.err); got != tc.want {
			t.Errorf("%s: isCopyUIDParseError = %v, want %v", tc.name, got, tc.want)
		}
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
