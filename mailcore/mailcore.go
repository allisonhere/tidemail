// Package mailcore exposes a small, UI-independent mail API for TideMail's
// Android app. The desktop client retains the IMAP, SMTP, and config internals.
package mailcore

import (
	"context"
	"errors"
	"time"

	imapgo "github.com/emersion/go-imap/v2"

	"github.com/allisonhere/tidemail/internal/auth"
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

	// OAuth2 sign-in. Set AuthMethod to "oauth2" and Provider to "Gmail" (Outlook is the other
	// supported provider) with a client and refresh token instead of a Password.
	Provider     string
	AuthMethod   string
	ClientID     string
	ClientSecret string
	RefreshToken string
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

		Provider:     a.Provider,
		AuthMethod:   a.AuthMethod,
		ClientID:     a.ClientID,
		ClientSecret: a.ClientSecret,
		RefreshToken: a.RefreshToken,
	}
}

// Attachment is a MIME part of a message: a downloadable file, or an inline
// image referenced from the HTML body as cid:<ContentID>.
type Attachment struct {
	Filename    string
	ContentType string
	ContentID   string
	Inline      bool
	Size        int64
	Data        []byte
}

// Message contains the fields currently used by Android's inbox and reader.
type Message struct {
	UID         uint32
	From        string
	Subject     string
	BodyText    string
	BodyHTML    string
	To          string
	CC          string
	Attachments []Attachment
	Date        time.Time
	Read        bool
	Starred     bool
	MessageID   string
}

func projectMessage(m db.Message) Message {
	atts := make([]Attachment, len(m.AttachmentData))
	for i, a := range m.AttachmentData {
		atts[i] = Attachment{
			Filename: a.Filename, ContentType: a.ContentType, ContentID: a.ContentID,
			Inline: a.Inline, Size: a.Size, Data: a.Data,
		}
	}
	return Message{
		Attachments: atts,
		UID:         m.UID,
		From:        m.From,
		Subject:     m.Subject,
		BodyText:    m.BodyText,
		BodyHTML:    m.BodyHTML,
		To:          m.To,
		CC:          m.CC,
		Date:        m.Date,
		Read:        m.Read,
		Starred:     m.Starred,
		MessageID:   m.MessageID,
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
	return c.FetchMailbox(ctx, "INBOX", limit)
}

// FetchMailbox returns the newest messages of any mailbox. Keep limit modest for folders such
// as Sent, where large attachments make big pages slow (see MessagesPerFolderFirstSync).
func (c *Client) FetchMailbox(ctx context.Context, mailbox string, limit int) ([]Message, error) {
	messages, err := c.inner.FetchMessages(ctx, mailbox, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Message, 0, len(messages))
	for _, message := range messages {
		out = append(out, projectMessage(message))
	}
	return out, nil
}

// FetchOlderInbox returns the next page of INBOX messages with UIDs below
// beforeUID. UIDs can be sparse, so callers should use the oldest UID from
// their current page as the cursor rather than subtracting the page size.
func (c *Client) FetchOlderInbox(ctx context.Context, beforeUID uint32, limit int) ([]Message, error) {
	return c.FetchOlderMailbox(ctx, "INBOX", beforeUID, limit)
}

// FetchOlderMailbox is FetchOlderInbox for any mailbox.
func (c *Client) FetchOlderMailbox(ctx context.Context, mailbox string, beforeUID uint32, limit int) ([]Message, error) {
	messages, err := c.inner.FetchOlderThan(ctx, mailbox, beforeUID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Message, 0, len(messages))
	for _, message := range messages {
		out = append(out, projectMessage(message))
	}
	return out, nil
}

// SearchMailbox asks the server to find messages containing text (headers or body) in a mailbox
// and returns up to limit of them, newest first.
func (c *Client) SearchMailbox(ctx context.Context, mailbox string, text string, limit int) ([]Message, error) {
	messages, err := c.inner.SearchText(ctx, mailbox, text, limit)
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
	return c.FetchMessageFrom(ctx, "INBOX", uid)
}

// FetchMessageFrom fetches one message by UID from any mailbox. UIDs are per mailbox.
func (c *Client) FetchMessageFrom(ctx context.Context, mailbox string, uid uint32) (Message, error) {
	message, err := c.inner.FetchByUID(ctx, mailbox, uid)
	if err != nil {
		return Message{}, err
	}
	return projectMessage(message), nil
}

func (c *Client) SetRead(ctx context.Context, uid uint32, read bool) error {
	return c.SetReadIn(ctx, "INBOX", uid, read)
}

func (c *Client) SetReadIn(ctx context.Context, mailbox string, uid uint32, read bool) error {
	return c.inner.MarkSeenUIDs(ctx, mailbox, []uint32{uid}, read)
}

func (c *Client) SetFlagged(ctx context.Context, uid uint32, flagged bool) error {
	return c.SetFlaggedIn(ctx, "INBOX", uid, flagged)
}

func (c *Client) SetFlaggedIn(ctx context.Context, mailbox string, uid uint32, flagged bool) error {
	return c.inner.MarkFlagged(ctx, mailbox, uid, flagged)
}

func (c *Client) Move(ctx context.Context, uid uint32, mailbox string) error {
	return c.MoveFrom(ctx, "INBOX", uid, mailbox)
}

// MoveFrom moves a message from one mailbox to another.
func (c *Client) MoveFrom(ctx context.Context, from string, uid uint32, to string) error {
	return c.inner.MoveMessage(ctx, from, uid, to)
}

func (c *Client) Delete(ctx context.Context, uid uint32) error {
	return c.DeleteFrom(ctx, "INBOX", uid)
}

// DeleteFrom permanently deletes (\Deleted + expunge) a message from any mailbox.
func (c *Client) DeleteFrom(ctx context.Context, mailbox string, uid uint32) error {
	return c.inner.DeleteMessage(ctx, mailbox, uid)
}

// OutgoingMessage carries the fields used by Android compose and reply.
type OutgoingMessage struct {
	From        string
	To          []string
	CC          []string
	BCC         []string
	Subject     string
	Body        string
	InReplyTo   string
	References  string
	Attachments []OutgoingAttachment
}

// OutgoingAttachment is a file to attach to an outgoing message.
type OutgoingAttachment struct {
	Name string
	Data []byte
}

func toSMTP(message OutgoingMessage) smtp.OutgoingMessage {
	outgoing := smtp.OutgoingMessage{
		From:       message.From,
		To:         message.To,
		CC:         message.CC,
		BCC:        message.BCC,
		Subject:    message.Subject,
		Body:       message.Body,
		InReplyTo:  message.InReplyTo,
		References: message.References,
	}
	for _, a := range message.Attachments {
		outgoing.Attachments = append(outgoing.Attachments, smtp.Attachment{Name: a.Name, Data: a.Data})
	}
	return outgoing
}

func Send(ctx context.Context, account Account, message OutgoingMessage) error {
	outgoing := toSMTP(message)
	outgoing.EnsureIdentity(message.From)
	return smtp.Send(ctx, account.config(), outgoing)
}

// SendWithCopy sends the message and returns the exact bytes that were transmitted, so the caller
// can append an identical copy to the Sent folder (SMTP submission leaves no copy behind on most
// servers; Gmail is the exception and saves its own).
func SendWithCopy(ctx context.Context, account Account, message OutgoingMessage) ([]byte, error) {
	outgoing := toSMTP(message)
	outgoing.EnsureIdentity(message.From)
	raw := smtp.BuildRaw(account.config(), outgoing)
	if err := smtp.Send(ctx, account.config(), outgoing); err != nil {
		return nil, err
	}
	return raw, nil
}

// BuildDraft returns the RFC822 bytes for message without sending it, plus the Message-ID it
// carries, for saving to the Drafts folder.
func BuildDraft(account Account, message OutgoingMessage) (raw []byte, messageID string) {
	outgoing := toSMTP(message)
	outgoing.EnsureIdentity(message.From)
	return smtp.BuildRaw(account.config(), outgoing), outgoing.MessageID
}

// AppendSent stores raw in the given mailbox marked read, as a sent copy.
func (c *Client) AppendSent(ctx context.Context, mailbox string, raw []byte, when time.Time) error {
	return c.inner.AppendSent(ctx, mailbox, raw, when)
}

// AppendDraft stores raw in the given mailbox flagged \Draft (and read).
func (c *Client) AppendDraft(ctx context.Context, mailbox string, raw []byte, when time.Time) error {
	return c.inner.AppendWithFlags(ctx, mailbox, raw, when, imapgo.FlagSeen, imapgo.FlagDraft)
}

// GoogleSignIn is one browser sign-in attempt for a Gmail account. The caller opens AuthURL in a
// browser; Wait returns once Google redirects back to a short-lived localhost listener.
type GoogleSignIn struct {
	flow *auth.GoogleBrowserFlow
}

// StartGoogleSignIn begins an authorization-code + PKCE sign-in with a Desktop-type Google OAuth
// client. It listens on 127.0.0.1 until the sign-in finishes, is cancelled, or times out.
func StartGoogleSignIn(ctx context.Context, clientID, clientSecret string) (*GoogleSignIn, error) {
	flow, err := auth.StartGoogleBrowserFlow(ctx, clientID, clientSecret)
	if err != nil {
		return nil, err
	}
	return &GoogleSignIn{flow: flow}, nil
}

// AuthURL is the Google sign-in page to open in a browser.
func (g *GoogleSignIn) AuthURL() string { return g.flow.AuthURL }

// Wait blocks until sign-in completes and returns the refresh token to store for the account.
func (g *GoogleSignIn) Wait() (string, error) {
	tok, err := g.flow.Wait()
	if err != nil {
		return "", err
	}
	if tok == nil || tok.RefreshToken == "" {
		return "", errors.New("no refresh token came back from Google; remove TideMail from your Google account's third-party access and sign in again")
	}
	return tok.RefreshToken, nil
}

// Cancel abandons the sign-in and closes its listener.
func (g *GoogleSignIn) Cancel() { g.flow.Close() }
