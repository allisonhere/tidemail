package imap

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
)

// TestConnectWaitsForGreetingBeforeLogin guards the greeting handshake: a
// server that drops commands arriving before its greeting is processed (QQ
// Mail's XMIMAP4Server does) never answers a pipelined LOGIN, so Connect would
// sit there until the context deadline and report a bare "unexpected EOF".
// Connect must therefore write nothing until the greeting has been read.
func TestConnectWaitsForGreetingBeforeLogin(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	early := make(chan int, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Hold the greeting back and watch for command bytes: a read during
		// this window must time out when the client waits as it should.
		buf := make([]byte, 512)
		if err := conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
			return
		}
		n, _ := conn.Read(buf) //nolint:errcheck // a timeout is the expected outcome
		early <- n
		if err := conn.SetReadDeadline(time.Time{}); err != nil {
			return
		}

		if _, err := conn.Write([]byte("* OK [CAPABILITY IMAP4rev1] test server ready\r\n")); err != nil {
			return
		}

		// Reply to the LOGIN, whether it arrived early (buffered above) or only
		// now, so the test fails on the assertion instead of deadlocking.
		tag := firstField(string(buf[:n]))
		if tag == "" {
			if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
				return
			}
			line, err := bufio.NewReader(conn).ReadString('\n')
			if err != nil {
				return
			}
			tag = firstField(line)
		}
		if tag == "" {
			return
		}
		_, _ = conn.Write([]byte(tag + " NO [AUTHENTICATIONFAILED] rejected by test server\r\n")) //nolint:errcheck
	}()

	client := New(config.AccountConfig{
		IMAPHost: "127.0.0.1",
		IMAPPort: ln.Addr().(*net.TCPAddr).Port,
		User:     "testuser",
		Password: "testpass",
	})

	err = client.Connect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "login:") {
		t.Fatalf("Connect error = %v, want a login failure", err)
	}
	if n := <-early; n != 0 {
		t.Fatalf("client wrote %d bytes before the server greeting; LOGIN must wait for it", n)
	}
}

// firstField returns the first whitespace-separated field of s, or "".
func firstField(s string) string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
