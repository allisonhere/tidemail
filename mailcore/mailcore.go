// Package mailcore exposes a small, UI-independent mail API for TideMail's
// Android app. The desktop client retains the IMAP, SMTP, and config internals.
package mailcore

import (
	"context"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/imap"
	"github.com/allisonhere/tidemail/internal/smtp"
)

// Account contains only the connection settings needed by the mail API.
// Callers own credential storage and pass credentials for each operation.
type Account struct {
	IMAPHost string
	IMAPPort int
	IMAPTLS  bool
	SMTPHost string
	SMTPPort int
	SMTPTLS  bool
	User     string
	Password string
	From     string
}

func (a Account) config() config.AccountConfig {
	return config.AccountConfig{
		IMAPHost: a.IMAPHost,
		IMAPPort: a.IMAPPort,
		IMAPTLS:  a.IMAPTLS,
		SMTPHost: a.SMTPHost,
		SMTPPort: a.SMTPPort,
		SMTPTLS:  a.SMTPTLS,
		User:     a.User,
		Password: a.Password,
		From:     a.From,
	}
}

// Message contains the fields currently used by Android's inbox and reader.
type Message struct {
	UID       uint32
	From      string
	Subject   string
	BodyText  string
	Date      time.Time
	Read      bool
	Starred   bool
	MessageID string
}

func projectMessage(m db.Message) Message {
	return Message{
		UID:       m.UID,
		From:      m.From,
		Subject:   m.Subject,
		BodyText:  m.BodyText,
		Date:      m.Date,
		Read:      m.Read,
		Starred:   m.Starred,
		MessageID: m.MessageID,
	}
}

// Mailbox includes the IMAP flags needed to identify an archive mailbox.
type Mailbox struct {
	Name  string
	Flags []string
}

// Client wraps one IMAP connection. Call Close after Connect succeeds.
type Client struct {
	inner *imap.Client
}

func NewClient(account Account) *Client {
	return &Client{inner: imap.New(account.config())}
}

func (c *Client) Connect(ctx context.Context) error { return c.inner.Connect(ctx) }
func (c *Client) Close() error                      { return c.inner.Close() }

func (c *Client) ListMailboxes(ctx context.Context) ([]Mailbox, error) {
	mailboxes, err := c.inner.ListMailboxes(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Mailbox, 0, len(mailboxes))
	for _, mailbox := range mailboxes {
		out = append(out, Mailbox{Name: mailbox.Name, Flags: mailbox.Flags})
	}
	return out, nil
}

func (c *Client) FetchInbox(ctx context.Context, limit int) ([]Message, error) {
	messages, err := c.inner.FetchMessages(ctx, "INBOX", limit)
	if err != nil {
		return nil, err
	}
	out := make([]Message, 0, len(messages))
	for _, message := range messages {
		out = append(out, projectMessage(message))
	}
	return out, nil
}

func (c *Client) FetchMessage(ctx context.Context, uid uint32) (Message, error) {
	message, err := c.inner.FetchByUID(ctx, "INBOX", uid)
	if err != nil {
		return Message{}, err
	}
	return projectMessage(message), nil
}

func (c *Client) SetRead(ctx context.Context, uid uint32, read bool) error {
	return c.inner.MarkSeenUIDs(ctx, "INBOX", []uint32{uid}, read)
}

func (c *Client) SetFlagged(ctx context.Context, uid uint32, flagged bool) error {
	return c.inner.MarkFlagged(ctx, "INBOX", uid, flagged)
}

func (c *Client) Move(ctx context.Context, uid uint32, mailbox string) error {
	return c.inner.MoveMessage(ctx, "INBOX", uid, mailbox)
}

func (c *Client) Delete(ctx context.Context, uid uint32) error {
	return c.inner.DeleteMessage(ctx, "INBOX", uid)
}

// OutgoingMessage carries the fields used by Android compose and reply.
type OutgoingMessage struct {
	From       string
	To         []string
	Subject    string
	Body       string
	InReplyTo  string
	References string
}

func Send(ctx context.Context, account Account, message OutgoingMessage) error {
	outgoing := smtp.OutgoingMessage{
		From:       message.From,
		To:         message.To,
		Subject:    message.Subject,
		Body:       message.Body,
		InReplyTo:  message.InReplyTo,
		References: message.References,
	}
	outgoing.EnsureIdentity(message.From)
	return smtp.Send(ctx, account.config(), outgoing)
}
