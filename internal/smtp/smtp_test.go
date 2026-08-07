package smtp

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
)

func TestSendMail(t *testing.T) {
	server, client := net.Pipe()

	go func() {
		// Go 1.26 smtp.NewClient reads 220 greeting first
		server.Write([]byte("220 smtp.test ESMTP\r\n"))
		buf := make([]byte, 1024)

		// Read EHLO (sent by hello() inside client.Mail())
		n, _ := server.Read(buf)
		cmd := string(buf[:n])
		if !strings.HasPrefix(cmd, "EHLO") {
			t.Errorf("expected EHLO, got %s", cmd)
		}
		server.Write([]byte("250-Hello\r\n250 AUTH PLAIN\r\n"))

		// Read MAIL FROM
		n, _ = server.Read(buf)
		cmd = string(buf[:n])
		if !strings.HasPrefix(cmd, "MAIL FROM:<alice@x>") {
			t.Errorf("expected MAIL FROM, got %s", cmd)
		}
		server.Write([]byte("250 OK\r\n"))

		// Read RCPT TO
		n, _ = server.Read(buf)
		cmd = string(buf[:n])
		if !strings.HasPrefix(cmd, "RCPT TO:<bob@x>") {
			t.Errorf("expected RCPT TO, got %s", cmd)
		}
		server.Write([]byte("250 OK\r\n"))

		// Read DATA
		n, _ = server.Read(buf)
		if !strings.Contains(string(buf[:n]), "DATA") {
			t.Errorf("expected DATA, got %s", cmd)
		}
		server.Write([]byte("354 Start mail input\r\n"))

		// Read content
		n, _ = server.Read(buf)
		data := string(buf[:n])
		if !strings.Contains(data, "Subject: Hello") {
			t.Errorf("expected Subject: Hello, got %s", data)
		}
		server.Write([]byte("250 OK\r\n"))

		server.Close()
	}()

	sc, err := smtp.NewClient(client, "test.local")
	if err != nil {
		t.Fatal(err)
	}

	err = sendMail(sc, "alice@x", []string{"bob@x"}, []byte("Subject: Hello\r\n\r\nBody"))
	client.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestSendSTARTTLS_NoTLS(t *testing.T) {
	server, clientConn := net.Pipe()

	done := make(chan struct{})
	go func() {
		defer close(done)
		// Go 1.26 smtp.NewClient reads 220 first
		server.Write([]byte("220 localhost ESMTP\r\n"))
		buf := make([]byte, 1024)

		// Read EHLO (sent by hello() inside smtp.NewClient)
		n, _ := server.Read(buf)
		if !strings.Contains(string(buf[:n]), "EHLO") {
			t.Errorf("expected EHLO, got %q", string(buf[:n]))
		}
		// Respond with AUTH but not STARTTLS — localhost exempts PlainAuth from TLS check
		server.Write([]byte("250-Hello\r\n250 AUTH PLAIN\r\n"))

		// Read AUTH PLAIN (sent by client.Auth())
		n, _ = server.Read(buf)
		if !strings.Contains(string(buf[:n]), "AUTH") {
			t.Errorf("expected AUTH, got %q", string(buf[:n]))
		}
		server.Write([]byte("235 Authentication successful\r\n"))

		// Read MAIL FROM (sent by sendMail)
		server.Read(buf)
		server.Write([]byte("250 OK\r\n"))

		// Read RCPT TO
		server.Read(buf)
		server.Write([]byte("250 OK\r\n"))

		// Read DATA
		server.Read(buf)
		server.Write([]byte("354 Start mail input\r\n"))

		// Read content
		server.Read(buf)
		server.Write([]byte("250 OK\r\n"))

		server.Close()
	}()

	// Override dial to return pipe connection
	oldDial := smtpDial
	oldClient := smtpNewClient
	defer func() {
		smtpDial = oldDial
		smtpNewClient = oldClient
	}()

	smtpDial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return clientConn, nil
	}
	smtpNewClient = func(conn net.Conn, host string) (*smtp.Client, error) {
		return smtp.NewClient(conn, host)
	}

	err := sendSTARTTLS(context.Background(), "localhost:587", config.AccountConfig{
		User:     "user",
		Password: "pass",
	}, "alice@x", []string{"bob@x"}, []byte("Subject: Hello\r\n\r\nBody"))
	if err != nil {
		t.Fatal(err)
	}
	<-done
}

func TestSendSTARTTLS_NoRecipients(t *testing.T) {
	err := Send(context.Background(), config.AccountConfig{SMTPHost: "x", SMTPPort: 587}, OutgoingMessage{})
	if err == nil || !strings.Contains(err.Error(), "no recipients") {
		t.Fatalf("expected 'no recipients', got %v", err)
	}
}

func TestSend_STARTTLS_DialError(t *testing.T) {
	oldDial := smtpDial
	defer func() { smtpDial = oldDial }()
	smtpDial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return nil, fmt.Errorf("connection refused")
	}

	err := Send(context.Background(), config.AccountConfig{
		User:     "u",
		Password: "p",
		From:     "u@x",
		SMTPHost: "localhost",
		SMTPPort: 587,
	}, OutgoingMessage{
		To:      []string{"b@x"},
		Subject: "Hi",
		Body:    "Hello",
	})
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("expected dial error, got %v", err)
	}
}

func TestSend_TLS_DialError(t *testing.T) {
	oldTLSDial := tlsDial
	defer func() { tlsDial = oldTLSDial }()
	tlsDial = func(ctx context.Context, network, addr string, cfg *tls.Config) (net.Conn, error) {
		return nil, fmt.Errorf("tls dial failed")
	}

	err := Send(context.Background(), config.AccountConfig{
		User:     "u",
		Password: "p",
		From:     "u@x",
		SMTPHost: "localhost",
		SMTPPort: 465,
	}, OutgoingMessage{
		To:      []string{"b@x"},
		Subject: "Hi",
		Body:    "Hello",
	})
	if err == nil || !strings.Contains(err.Error(), "tls dial failed") {
		t.Fatalf("expected TLS dial error, got %v", err)
	}
}

func TestSend_FromFallback(t *testing.T) {
	oldDial := smtpDial
	defer func() { smtpDial = oldDial }()

	smtpDial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return nil, fmt.Errorf("simulated error with user fallback")
	}

	err := Send(context.Background(), config.AccountConfig{
		User:     "fallback@x",
		Password: "p",
		SMTPHost: "localhost",
		SMTPPort: 587,
	}, OutgoingMessage{
		To:      []string{"b@x"},
		Subject: "Hi",
		Body:    "Hello",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestSend_HeaderKeepsDisplayName verifies that a "Name <addr>" From is stripped to a
// bare address for the MAIL FROM envelope (Gmail rejects display names there) while the
// visible From: header still carries the display name.
func TestSend_HeaderKeepsDisplayName(t *testing.T) {
	server, clientConn := net.Pipe()

	var mailFrom, data string
	done := make(chan struct{})
	go func() {
		defer close(done)
		server.Write([]byte("220 localhost ESMTP\r\n"))
		buf := make([]byte, 4096)

		server.Read(buf) // EHLO
		server.Write([]byte("250-Hello\r\n250 AUTH PLAIN\r\n"))

		server.Read(buf) // AUTH PLAIN
		server.Write([]byte("235 Authentication successful\r\n"))

		n, _ := server.Read(buf) // MAIL FROM
		mailFrom = string(buf[:n])
		server.Write([]byte("250 OK\r\n"))

		server.Read(buf) // RCPT TO
		server.Write([]byte("250 OK\r\n"))

		server.Read(buf) // DATA
		server.Write([]byte("354 Start mail input\r\n"))

		n, _ = server.Read(buf) // message content
		data = string(buf[:n])
		server.Write([]byte("250 OK\r\n"))

		server.Close()
	}()

	oldDial := smtpDial
	oldClient := smtpNewClient
	defer func() {
		smtpDial = oldDial
		smtpNewClient = oldClient
	}()
	smtpDial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return clientConn, nil
	}
	smtpNewClient = func(conn net.Conn, host string) (*smtp.Client, error) {
		return smtp.NewClient(conn, host)
	}

	err := Send(context.Background(), config.AccountConfig{
		User:     "user",
		Password: "pass",
		From:     "John Doe <john@x.com>",
		SMTPHost: "localhost",
		SMTPPort: 587,
	}, OutgoingMessage{
		To:      []string{"bob@x"},
		Subject: "Hi",
		Body:    "Hello",
	})
	clientConn.Close()
	if err != nil {
		t.Fatal(err)
	}
	<-done

	if !strings.HasPrefix(mailFrom, "MAIL FROM:<john@x.com>") {
		t.Errorf("envelope should be the bare address, got %q", mailFrom)
	}
	if !strings.Contains(data, "From: John Doe <john@x.com>") {
		t.Errorf("From: header should keep the display name, got %q", data)
	}
}

func TestCleanEmail(t *testing.T) {
	cases := []struct{ in, want string }{
		{"John Doe <john@x.com>", "john@x.com"}, // strip display name for the envelope
		{"john@x.com", "john@x.com"},
		{"alice", "alice"},        // non-email login is accepted (server validates)
		{"<foo@bar", ""},          // unmatched '<' is rejected, not returned malformed
		{"  Name <a@b>  ", "a@b"}, // surrounding whitespace trimmed
		{"", ""},
		{"two words", ""},    // whitespace and no brackets → invalid
		{"a@b <c@d>", "c@d"}, // angle-bracket address wins
	}
	for _, c := range cases {
		if got := cleanEmail(c.in); got != c.want {
			t.Errorf("cleanEmail(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSendMail_MAILFROMError(t *testing.T) {
	server, client := net.Pipe()

	go func() {
		server.Write([]byte("220 smtp.test ESMTP\r\n"))
		buf := make([]byte, 1024)
		server.Read(buf) // EHLO
		server.Write([]byte("250-Hello\r\n250 AUTH PLAIN\r\n"))
		server.Read(buf) // MAIL FROM
		server.Write([]byte("550 Sender rejected\r\n"))
	}()

	sc, err := smtp.NewClient(client, "test.local")
	if err != nil {
		t.Fatal(err)
	}

	err = sendMail(sc, "bad@x", []string{"b@x"}, []byte("data"))
	client.Close()
	if err == nil || !strings.Contains(err.Error(), "MAIL FROM") {
		t.Fatalf("expected MAIL FROM error, got %v", err)
	}
}

func TestSendMail_RCPTError(t *testing.T) {
	server, client := net.Pipe()

	go func() {
		server.Write([]byte("220 smtp.test ESMTP\r\n"))
		buf := make([]byte, 1024)
		server.Read(buf) // EHLO
		server.Write([]byte("250-Hello\r\n250 AUTH PLAIN\r\n"))
		server.Read(buf) // MAIL FROM
		server.Write([]byte("250 OK\r\n"))
		server.Read(buf) // RCPT TO
		server.Write([]byte("550 No such user\r\n"))
	}()

	sc, err := smtp.NewClient(client, "test.local")
	if err != nil {
		t.Fatal(err)
	}

	err = sendMail(sc, "a@x", []string{"bad@x"}, []byte("data"))
	client.Close()
	if err == nil || !strings.Contains(err.Error(), "RCPT TO") {
		t.Fatalf("expected RCPT TO error, got %v", err)
	}
}

func TestSendMail_DATAError(t *testing.T) {
	server, client := net.Pipe()

	go func() {
		server.Write([]byte("220 smtp.test ESMTP\r\n"))
		buf := make([]byte, 1024)
		server.Read(buf) // EHLO
		server.Write([]byte("250-Hello\r\n250 AUTH PLAIN\r\n"))
		server.Read(buf) // MAIL FROM
		server.Write([]byte("250 OK\r\n"))
		server.Read(buf) // RCPT TO
		server.Write([]byte("250 OK\r\n"))
		server.Read(buf) // DATA
		server.Write([]byte("554 Transaction failed\r\n"))
	}()

	sc, err := smtp.NewClient(client, "test.local")
	if err != nil {
		t.Fatal(err)
	}

	err = sendMail(sc, "a@x", []string{"b@x"}, []byte("data"))
	client.Close()
	if err == nil || !strings.Contains(err.Error(), "DATA") {
		t.Fatalf("expected DATA error, got %v", err)
	}
}

func TestBuildRaw_WithAttachments(t *testing.T) {
	raw := buildRaw("a@x", OutgoingMessage{
		To:      []string{"b@x"},
		Subject: "Files",
		Body:    "See attached",
		Attachments: []Attachment{
			{Name: "hello.txt", Data: []byte("Hello, world!")},
			{Name: "data.csv", Data: []byte("a,b,c\n1,2,3")},
		},
	})
	s := string(raw)

	if !strings.Contains(s, "multipart/mixed") {
		t.Fatalf("expected multipart/mixed, got %q", s)
	}
	if !strings.Contains(s, "hello.txt") {
		t.Fatal("expected attachment filename hello.txt")
	}
	if !strings.Contains(s, "data.csv") {
		t.Fatal("expected attachment filename data.csv")
	}
	if !strings.Contains(s, "See attached") {
		t.Fatal("expected body text")
	}
	// Base64 of "Hello, world!"
	if !strings.Contains(s, "SGVsbG8sIHdvcmxkIQ==") {
		t.Fatalf("expected base64 content, got %q", s)
	}
	// Boundary should be at end
	if !strings.HasSuffix(strings.TrimSpace(s), "--") {
		t.Fatal("expected closing boundary")
	}
}

func TestBuildRaw_MultilineBody(t *testing.T) {
	raw := buildRaw("a@x", OutgoingMessage{
		To:      []string{"b@x"},
		Subject: "Hi",
		Body:    "Line1\n Line2\n Line3",
	})
	s := string(raw)
	if !strings.Contains(s, "Line1") || !strings.Contains(s, "Line2") || !strings.Contains(s, "Line3") {
		t.Fatalf("expected multiline body, got %q", s)
	}
}
