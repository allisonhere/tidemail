package smtp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"mime"
	"mime/multipart"
	"net"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"

	"github.com/allisonhere/tidemail/internal/auth"
	"github.com/allisonhere/tidemail/internal/config"
	"github.com/yuin/goldmark"
)

var (
	smtpNewClient = smtp.NewClient
	smtpDial      = (&net.Dialer{Timeout: 30 * time.Second}).DialContext
	tlsDial       = func(ctx context.Context, network, addr string, cfg *tls.Config) (net.Conn, error) {
		return (&tls.Dialer{
			NetDialer: &net.Dialer{Timeout: 30 * time.Second},
			Config:    cfg,
		}).DialContext(ctx, network, addr)
	}
)

type Attachment struct {
	Name string
	Data []byte
}

type OutgoingMessage struct {
	From        string
	To          []string
	CC          []string
	BCC         []string
	Subject     string
	Body        string
	HTMLBody    string
	InReplyTo   string
	References  string
	Attachments []Attachment
	// Date and MessageID are stamped once, before the message is transmitted,
	// so that a copy appended to the Sent folder is byte-identical to what was
	// delivered. Generating them inside buildRaw would differ per call.
	Date      time.Time
	MessageID string
}

// EnsureIdentity stamps the Date and Message-ID if they are not set yet. Call
// it before Send so BuildRaw reproduces exactly the transmitted bytes.
func (m *OutgoingMessage) EnsureIdentity(from string) {
	if m.Date.IsZero() {
		m.Date = time.Now()
	}
	if m.MessageID == "" {
		m.MessageID = newMessageID(from)
	}
}

func (m OutgoingMessage) sentAt() time.Time {
	if m.Date.IsZero() {
		return time.Now()
	}
	return m.Date
}

func (m OutgoingMessage) messageID(from string) string {
	if m.MessageID != "" {
		return m.MessageID
	}
	return newMessageID(from)
}

// newMessageID builds an RFC 5322 msg-id using the sender's domain, falling
// back to the local host when the address has none.
func newMessageID(from string) string {
	domain := "localhost"
	if addr := cleanEmail(from); addr != "" {
		if at := strings.LastIndex(addr, "@"); at >= 0 && at+1 < len(addr) {
			domain = addr[at+1:]
		}
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("<%d@%s>", time.Now().UnixNano(), domain)
	}
	return fmt.Sprintf("<%s@%s>", hex.EncodeToString(b[:]), domain)
}

// BuildRaw returns the exact RFC822 bytes Send transmits for msg, so a caller
// can append an identical copy to the server's Sent folder. SMTP submission
// leaves no copy behind.
func BuildRaw(cfg config.AccountConfig, msg OutgoingMessage) []byte {
	return buildRaw(senderAddress(cfg, msg), msg)
}

// AccountSender resolves an account's own sending identity, independent of
// anything a message carries. Callers that must not honour a stale per-message
// From — an Outbox retry, whose message was serialized before the account was
// corrected — resolve through this instead.
func AccountSender(cfg config.AccountConfig) string {
	if cfg.From != "" {
		return cfg.From
	}
	return cfg.User
}

// senderAddress resolves the From: header value the same way Send does. A
// message that names its own From wins, so a caller can override per message.
func senderAddress(cfg config.AccountConfig, msg OutgoingMessage) string {
	if msg.From != "" {
		return msg.From
	}
	return AccountSender(cfg)
}

func Send(ctx context.Context, cfg config.AccountConfig, msg OutgoingMessage) error {
	from := senderAddress(cfg, msg)
	// The envelope (MAIL FROM) must be a bare address — Gmail rejects "Name <addr>" —
	// but the From: header should keep the display name, so clean only the envelope copy.
	envelopeFrom := cleanEmail(from)

	var allTo []string
	allTo = append(allTo, msg.To...)
	allTo = append(allTo, msg.CC...)
	allTo = append(allTo, msg.BCC...)
	if len(allTo) == 0 {
		return fmt.Errorf("no recipients")
	}
	if envelopeFrom == "" {
		return fmt.Errorf("no sender address: configure 'from' or check account user")
	}

	raw := buildRaw(from, msg)

	addr := fmt.Sprintf("%s:%d", cfg.SMTPHost, cfg.SMTPPort)

	if cfg.SMTPPort == 465 {
		return sendTLS(ctx, addr, cfg, envelopeFrom, allTo, raw)
	}
	return sendSTARTTLS(ctx, addr, cfg, envelopeFrom, allTo, raw)
}

// xoauth2Auth implements the smtp.Auth interface for XOAUTH2.
type xoauth2Auth struct {
	user, token string
}

func (a *xoauth2Auth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	// Return the RAW initial-response bytes. net/smtp's Client.Auth base64-encodes
	// whatever Start returns before sending "AUTH XOAUTH2 <base64>"; encoding here
	// too would double-encode and the server rejects it with "501 5.5.2 Cannot Decode".
	resp := "user=" + a.user + "\x01auth=Bearer " + a.token + "\x01\x01"
	return "XOAUTH2", []byte(resp), nil
}

func (a *xoauth2Auth) Next(fromServer []byte, more bool) ([]byte, error) {
	if more {
		// On auth failure the server sends a challenge whose body is a JSON
		// error description (already base64-decoded by net/smtp) — surface it.
		return nil, fmt.Errorf("xoauth2 rejected: %s", fromServer)
	}
	return nil, nil
}

// smtpAuth returns the smtp.Auth for the given config: XOAUTH2 for Gmail /
// Outlook accounts with a refresh token, otherwise app password over PLAIN
// (Gmail requires an app password + 2FA).
func smtpAuth(ctx context.Context, cfg config.AccountConfig, host string) (smtp.Auth, error) {
	var tok string
	var err error
	switch {
	case cfg.UsesGoogleOAuth2():
		tok, err = auth.GoogleAccessToken(ctx, cfg.ClientID, cfg.ClientSecret, cfg.SessionKey(), cfg.RefreshToken)
	case cfg.UsesMicrosoftOAuth2():
		tok, err = auth.MSAccessToken(ctx, cfg.ClientID, cfg.SessionKey(), cfg.RefreshToken)
	default:
		return smtp.PlainAuth("", cfg.User, cfg.Password, host), nil
	}
	if err != nil {
		return nil, fmt.Errorf("oauth2: %w", err)
	}
	return &xoauth2Auth{user: cfg.User, token: tok}, nil
}

func sendSTARTTLS(ctx context.Context, addr string, cfg config.AccountConfig, from string, to []string, raw []byte) error {
	conn, err := smtpDial(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("dial smtp: %w", err)
	}
	// ctx only bounds the dial by default (net/smtp doesn't take a context for
	// the session that follows); set the deadline on the raw socket too so a
	// connection left stale by e.g. a laptop suspend/resume can't block Auth/
	// Mail/Rcpt/Data forever instead of erroring out.
	if dl, ok := ctx.Deadline(); ok {
		conn.SetDeadline(dl) //nolint:errcheck
	}

	host, _, _ := net.SplitHostPort(addr)
	client, err := smtpNewClient(conn, host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Close()

	if cfg.SMTPTLS {
		tlsCfg := &tls.Config{ServerName: host}
		if err := client.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	}

	auth, err := smtpAuth(ctx, cfg, host)
	if err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	return sendMail(client, from, to, raw)
}

func sendTLS(ctx context.Context, addr string, cfg config.AccountConfig, from string, to []string, raw []byte) error {
	host, _, _ := net.SplitHostPort(addr)
	tlsCfg := &tls.Config{ServerName: host}

	conn, err := tlsDial(ctx, "tcp", addr, tlsCfg)
	if err != nil {
		return fmt.Errorf("dial smtp tls: %w", err)
	}
	// See the matching comment in sendSTARTTLS: bound the whole session, not
	// just the dial, so a stale post-suspend socket errors out instead of
	// hanging Auth/Mail/Rcpt/Data indefinitely.
	if dl, ok := ctx.Deadline(); ok {
		conn.SetDeadline(dl) //nolint:errcheck
	}

	client, err := smtpNewClient(conn, host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Close()

	auth, err := smtpAuth(ctx, cfg, host)
	if err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	return sendMail(client, from, to, raw)
}

func sendMail(client *smtp.Client, from string, to []string, raw []byte) error {
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}
	for _, addr := range to {
		if err := client.Rcpt(addr); err != nil {
			return fmt.Errorf("RCPT TO %s: %w", addr, err)
		}
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("DATA: %w", err)
	}
	if _, err := w.Write(raw); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	err = w.Close()
	if err != nil {
		var response *textproto.Error
		if !errors.As(err, &response) {
			return fmt.Errorf("%w: %v", ErrDeliveryUncertain, err)
		}
	}
	return err
}

func buildRaw(from string, msg OutgoingMessage) []byte {
	var hdr strings.Builder
	hdr.WriteString("From: " + from + "\r\n")
	hdr.WriteString("To: " + strings.Join(msg.To, ", ") + "\r\n")
	if len(msg.CC) > 0 {
		hdr.WriteString("Cc: " + strings.Join(msg.CC, ", ") + "\r\n")
	}
	hdr.WriteString("Subject: " + msg.Subject + "\r\n")
	// RFC 5322 requires Date and an originator. A submission server stamps
	// these when they are missing, but a copy we APPEND to Sent is our own
	// bytes — without them it would have no date, and nothing for threading or
	// duplicate detection to key on.
	hdr.WriteString("Date: " + msg.sentAt().Format(time.RFC1123Z) + "\r\n")
	hdr.WriteString("Message-ID: " + msg.messageID(from) + "\r\n")
	if msg.InReplyTo != "" {
		hdr.WriteString("In-Reply-To: " + msg.InReplyTo + "\r\n")
	}
	if msg.References != "" {
		hdr.WriteString("References: " + msg.References + "\r\n")
	}
	hdr.WriteString("MIME-Version: 1.0\r\n")

	if len(msg.Attachments) == 0 {
		if msg.HTMLBody == "" {
			hdr.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
			hdr.WriteString("\r\n")
			hdr.WriteString(msg.Body)
			return []byte(hdr.String())
		}
		// multipart/alternative: plain text + HTML
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		hdr.WriteString("Content-Type: multipart/alternative; boundary=\"" + mw.Boundary() + "\"\r\n")
		hdr.WriteString("\r\n")
		buf.WriteString(hdr.String())
		addTextPart(mw, msg.Body)
		addHTMLPart(mw, msg.HTMLBody)
		mw.Close()
		return buf.Bytes()
	}

	// Attachments present
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	hdr.WriteString("Content-Type: multipart/mixed; boundary=\"" + mw.Boundary() + "\"\r\n")
	hdr.WriteString("\r\n")
	buf.WriteString(hdr.String())

	if msg.HTMLBody != "" {
		// Wrap text+html in multipart/alternative inside multipart/mixed
		altWriter := multipart.NewWriter(&buf)
		altHdr := textproto.MIMEHeader{"Content-Type": {"multipart/alternative; boundary=\"" + altWriter.Boundary() + "\""}}
		altPart, _ := mw.CreatePart(altHdr)
		altHeader := fmt.Sprintf("Content-Type: multipart/alternative; boundary=\"%s\"\r\n\r\n", altWriter.Boundary())
		_, _ = altPart.Write([]byte(altHeader))
		addTextPart(altWriter, msg.Body)
		addHTMLPart(altWriter, msg.HTMLBody)
		altWriter.Close()
	} else {
		bodyHdr := textproto.MIMEHeader{"Content-Type": {"text/plain; charset=utf-8"}}
		w, _ := mw.CreatePart(bodyHdr)
		_, _ = w.Write([]byte(msg.Body))
	}

	// Attachment parts
	for _, att := range msg.Attachments {
		attHdr := textproto.MIMEHeader{
			"Content-Type":              {"application/octet-stream"},
			"Content-Disposition":       {mime.FormatMediaType("attachment", map[string]string{"filename": att.Name})},
			"Content-Transfer-Encoding": {"base64"},
		}
		w, _ := mw.CreatePart(attHdr)
		encoded := base64.StdEncoding.EncodeToString(att.Data)
		for i := 0; i < len(encoded); i += 76 {
			end := i + 76
			if end > len(encoded) {
				end = len(encoded)
			}
			_, _ = w.Write([]byte(encoded[i:end] + "\r\n"))
		}
	}

	mw.Close()
	return buf.Bytes()
}

func addTextPart(mw *multipart.Writer, body string) {
	hdr := textproto.MIMEHeader{"Content-Type": {"text/plain; charset=utf-8"}}
	w, _ := mw.CreatePart(hdr)
	_, _ = w.Write([]byte(body))
}

func addHTMLPart(mw *multipart.Writer, html string) {
	hdr := textproto.MIMEHeader{"Content-Type": {"text/html; charset=utf-8"}}
	w, _ := mw.CreatePart(hdr)
	_, _ = w.Write([]byte(html))
}

// MarkdownToHTML converts Markdown text to HTML using goldmark.
func MarkdownToHTML(md string) string {
	var buf bytes.Buffer
	if err := goldmark.Convert([]byte(md), &buf); err != nil {
		return ""
	}
	return buf.String()
}

// SignatureHTML renders a signature block for the HTML part of a message.
//
// It deliberately does not go through MarkdownToHTML. Markdown folds
// consecutive lines into one paragraph, so a three-line signature arrived as a
// single run-on line in every client that prefers the HTML part — even though
// the plain-text part was correct. A signature is not prose to be reflowed; the
// lines are the point.
func SignatureHTML(sig string) string {
	sig = strings.TrimSpace(sig)
	if sig == "" {
		return ""
	}
	lines := strings.Split(strings.ReplaceAll(sig, "\r\n", "\n"), "\n")
	for i, line := range lines {
		lines[i] = html.EscapeString(line)
	}
	// A div rather than a paragraph: <p> carries a margin above as well as
	// below, which opened a gap visibly wider than the single blank line the
	// text part has. The preceding paragraph's own bottom margin is the
	// separation, and this block adds nothing on top of it.
	//
	// "-- " on its own line is the standard delimiter; clients use it to fold or
	// strip the signature, and the trailing space is part of the convention.
	return "<div>-- <br>\n" + strings.Join(lines, "<br>\n") + "</div>\n"
}

// cleanEmail extracts a bare email address from formats like "user@host" or "Name <user@host>".
func cleanEmail(s string) string {
	s = strings.TrimSpace(s)
	// "Name <addr>" format
	if idx := strings.LastIndex(s, "<"); idx >= 0 {
		if end := strings.Index(s[idx:], ">"); end >= 0 {
			return strings.TrimSpace(s[idx+1 : idx+end])
		}
	}
	// Otherwise accept a bare token: no whitespace and no stray angle brackets.
	// No "@" requirement, so a non-email login like "alice" still works (the server
	// validates the address); a leftover "<"/">" from malformed input is rejected.
	if s != "" && !strings.ContainsAny(s, " \t<>") {
		return s
	}
	return ""
}

// ErrDeliveryUncertain means the connection failed while waiting for the final
// acceptance response. Retrying automatically could deliver a duplicate.
var ErrDeliveryUncertain = errors.New("delivery not confirmed; check Sent mail before retrying")

func CanRetry(err error) bool {
	if err == nil || errors.Is(err, ErrDeliveryUncertain) {
		return false
	}
	var response *textproto.Error
	if errors.As(err, &response) {
		return response.Code >= 400 && response.Code < 500
	}
	return true
}
