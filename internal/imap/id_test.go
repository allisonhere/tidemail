package imap

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
)

// startIDTestServer runs a minimal IMAP server that advertises caps and records
// the ID command, if one arrives. It answers LOGIN, CAPABILITY, ID and LOGOUT.
func startIDTestServer(t *testing.T, caps string) (port int, idSeen <-chan string, cleanup func()) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	seen := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		if _, err := fmt.Fprintf(conn, "* OK [CAPABILITY %s] test server ready\r\n", caps); err != nil {
			return
		}
		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			line := scanner.Text()
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			tag, command := fields[0], strings.ToUpper(fields[1])
			switch command {
			case "CAPABILITY":
				fmt.Fprintf(conn, "* CAPABILITY %s\r\n%s OK CAPABILITY done\r\n", caps, tag) //nolint:errcheck
			case "ID":
				select {
				case seen <- line:
				default:
				}
				fmt.Fprintf(conn, "* ID (\"name\" \"test server\")\r\n%s OK ID done\r\n", tag) //nolint:errcheck
			default:
				fmt.Fprintf(conn, "%s OK done\r\n", tag) //nolint:errcheck
			}
		}
	}()

	return ln.Addr().(*net.TCPAddr).Port, seen, func() { ln.Close() }
}

// TestConnectAnnouncesIDOnlyWhenAdvertised covers the NetEase (163/126/yeah.net)
// quirk: Coremail accepts LOGIN and then rejects every mailbox command with
// "NO Unsafe Login" until the client sends the RFC 2971 ID command. Servers
// that do not advertise ID must not be sent it, because they answer BAD.
func TestConnectAnnouncesIDOnlyWhenAdvertised(t *testing.T) {
	for _, tc := range []struct {
		name   string
		caps   string
		wantID bool
	}{
		{name: "server advertises ID", caps: "IMAP4rev1 ID", wantID: true},
		{name: "server has no ID", caps: "IMAP4rev1", wantID: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			port, idSeen, cleanup := startIDTestServer(t, tc.caps)
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

			select {
			case line := <-idSeen:
				if !tc.wantID {
					t.Fatalf("sent %q although the server does not advertise ID", line)
				}
			case <-time.After(2 * time.Second):
				if tc.wantID {
					t.Fatal("no ID command reached a server that advertises ID")
				}
			}
		})
	}
}
