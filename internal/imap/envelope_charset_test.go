package imap

import (
	"context"
	"github.com/allisonhere/tidemail/internal/config"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"testing"
	"time"
)

func TestEnvelopeDecodesLegacyCharsetsWithAndWithoutUpdates(t *testing.T) {
	for _, updates := range []bool{false, true} {
		port, cleanup := startTestServerWithSession(t, func(s imapserver.Session, _ *imapserver.Conn) imapserver.Session {
			return legacyEnvelopeSession{Session: s}
		})
		func() {
			defer cleanup()
			cfg := config.AccountConfig{IMAPHost: "127.0.0.1", IMAPPort: port, User: "testuser", Password: "testpass"}
			c := New(cfg)
			if updates {
				c = NewWithUpdates(cfg, func(MailboxUpdate) {})
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := c.Connect(ctx); err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			raw := []byte("Subject: =?iso-8859-1?Q?Caf=E9?=\r\nFrom: =?windows-1252?Q?Price_=80?= <from@example.com>\r\nTo: =?shift_jis?B?k/qWew==?= <to@example.com>\r\n\r\nbody\r\n")
			if err := c.AppendWithFlags(ctx, "INBOX", raw, time.Now()); err != nil {
				t.Fatal(err)
			}
			msgs, err := c.FetchMessages(ctx, "INBOX", 10)
			if err != nil || len(msgs) != 1 {
				t.Fatalf("fetch: %v %v", msgs, err)
			}
			if msgs[0].Subject != "Café" || msgs[0].From != "Price € <from@example.com>" || msgs[0].To != "日本 <to@example.com>" {
				t.Fatalf("updates=%v undecoded envelope: subject=%q from=%q to=%q", updates, msgs[0].Subject, msgs[0].From, msgs[0].To)
			}
		}()
	}
}

// Keep encoded words on the wire. imapmemserver normally decodes these headers
// itself, which would accidentally test the backend rather than Client.Connect.
type legacyEnvelopeSession struct{ imapserver.Session }

func (s legacyEnvelopeSession) Fetch(w *imapserver.FetchWriter, _ imap.NumSet, options *imap.FetchOptions) error {
	response := w.CreateMessage(1)
	response.WriteUID(1)
	if options.Envelope {
		response.WriteEnvelope(&imap.Envelope{
			Subject: "=?iso-8859-1?Q?Caf=E9?=",
			From:    []imap.Address{{Name: "=?windows-1252?Q?Price_=80?=", Mailbox: "from", Host: "example.com"}},
			To:      []imap.Address{{Name: "=?shift_jis?B?k/qWew==?=", Mailbox: "to", Host: "example.com"}},
		})
	}
	if options.Flags {
		response.WriteFlags(nil)
	}
	if options.InternalDate {
		response.WriteInternalDate(time.Now())
	}
	raw := []byte("Subject: test\r\n\r\nbody\r\n")
	for _, section := range options.BodySection {
		writer := response.WriteBodySection(section, int64(len(raw)))
		if _, err := writer.Write(raw); err != nil {
			return err
		}
		if err := writer.Close(); err != nil {
			return err
		}
	}
	return response.Close()
}
