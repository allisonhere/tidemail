package smtp

import (
	"net"
	"net/smtp"
	"testing"
)

func TestBuildRaw(t *testing.T) {
	tests := []struct {
		name string
		from string
		msg  OutgoingMessage
		want string
	}{
		{
			name: "basic message",
			from: "alice@example.com",
			msg: OutgoingMessage{
				From:    "alice@example.com",
				To:      []string{"bob@test.org"},
				Subject: "Hello",
				Body:    "Hi Bob!",
			},
			want: "From: alice@example.com\r\n" +
				"To: bob@test.org\r\n" +
				"Subject: Hello\r\n" +
				"MIME-Version: 1.0\r\n" +
				"Content-Type: text/plain; charset=utf-8\r\n" +
				"\r\n" +
				"Hi Bob!",
		},
		{
			name: "with CC and threading",
			from: "alice@example.com",
			msg: OutgoingMessage{
				From:       "alice@example.com",
				To:         []string{"bob@test.org"},
				CC:         []string{"carol@other.com"},
				Subject:    "Re: Hello",
				Body:       "Sure!",
				InReplyTo:  "<abc@example.com>",
				References: "<abc@example.com> <def@example.com>",
			},
			want: "From: alice@example.com\r\n" +
				"To: bob@test.org\r\n" +
				"Cc: carol@other.com\r\n" +
				"Subject: Re: Hello\r\n" +
				"In-Reply-To: <abc@example.com>\r\n" +
				"References: <abc@example.com> <def@example.com>\r\n" +
				"MIME-Version: 1.0\r\n" +
				"Content-Type: text/plain; charset=utf-8\r\n" +
				"\r\n" +
				"Sure!",
		},
		{
			name: "multiple recipients",
			from: "alice@example.com",
			msg: OutgoingMessage{
				From:    "alice@example.com",
				To:      []string{"bob@test.org", "carol@other.com"},
				Subject: "Team meeting",
				Body:    "Tomorrow at 10am",
			},
			want: "From: alice@example.com\r\n" +
				"To: bob@test.org, carol@other.com\r\n" +
				"Subject: Team meeting\r\n" +
				"MIME-Version: 1.0\r\n" +
				"Content-Type: text/plain; charset=utf-8\r\n" +
				"\r\n" +
				"Tomorrow at 10am",
		},
		{
			name: "from parameter used as header",
			from: "bounces@example.com",
			msg: OutgoingMessage{
				From:    "alice@example.com",
				To:      []string{"bob@test.org"},
				Subject: "Test",
				Body:    "Body",
			},
			want: "From: bounces@example.com\r\n" +
				"To: bob@test.org\r\n" +
				"Subject: Test\r\n" +
				"MIME-Version: 1.0\r\n" +
				"Content-Type: text/plain; charset=utf-8\r\n" +
				"\r\n" +
				"Body",
		},
		{
			name: "empty body",
			from: "alice@example.com",
			msg: OutgoingMessage{
				From:    "alice@example.com",
				To:      []string{"bob@test.org"},
				Subject: "Empty",
				Body:    "",
			},
			want: "From: alice@example.com\r\n" +
				"To: bob@test.org\r\n" +
				"Subject: Empty\r\n" +
				"MIME-Version: 1.0\r\n" +
				"Content-Type: text/plain; charset=utf-8\r\n" +
				"\r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(buildRaw(tt.from, tt.msg))
			if got != tt.want {
				t.Errorf("buildRaw() =\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}

type mockSMTPClient struct {
	mailFrom string
	rcptTos  []string
	data     []byte
}

func TestSendMail(t *testing.T) {
	// Use a local smtp.Client connected to a fake that records commands.
	// We build a minimal SMTP server simulation via net.Pipe.
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	go func() {
		// Read SMTP conversation
		buf := make([]byte, 4096)
		// Server greeting
		server.Write([]byte("220 mock ESMTP\r\n"))
		n, _ := server.Read(buf)
		_ = n
		server.Write([]byte("250 OK\r\n")) // EHLO
		n, _ = server.Read(buf)
		_ = n
		server.Write([]byte("250 OK\r\n")) // MAIL FROM
		n, _ = server.Read(buf)
		_ = n
		server.Write([]byte("250 OK\r\n")) // RCPT TO
		n, _ = server.Read(buf)
		_ = n
		server.Write([]byte("354 Start mail input\r\n")) // DATA
		n, _ = server.Read(buf)
		_ = n
		server.Write([]byte("250 OK\r\n")) // end DATA
		server.Close()
	}()

	host := "mock"
	smtpClient, err := smtp.NewClient(client, host)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer smtpClient.Close()

	err = sendMail(smtpClient, "alice@example.com", []string{"bob@test.org"}, []byte("Subject: Test\r\n\r\nBody"))
	if err != nil {
		t.Fatalf("sendMail() error = %v", err)
	}
}
