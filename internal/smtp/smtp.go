package smtp

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"mime/multipart"
	"net"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"

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
}

func Send(ctx context.Context, cfg config.AccountConfig, msg OutgoingMessage) error {
	from := cfg.From
	if from == "" {
		from = cfg.User
	}
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

// smtpAuth returns the smtp.Auth for the given config. TideMail authenticates
// with an app password over PLAIN (Gmail requires an app password + 2FA).
func smtpAuth(cfg config.AccountConfig, host string) (smtp.Auth, error) {
	return smtp.PlainAuth("", cfg.User, cfg.Password, host), nil
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

	auth, err := smtpAuth(cfg, host)
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

	auth, err := smtpAuth(cfg, host)
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
	return w.Close()
}

func buildRaw(from string, msg OutgoingMessage) []byte {
	var hdr strings.Builder
	hdr.WriteString("From: " + from + "\r\n")
	hdr.WriteString("To: " + strings.Join(msg.To, ", ") + "\r\n")
	if len(msg.CC) > 0 {
		hdr.WriteString("Cc: " + strings.Join(msg.CC, ", ") + "\r\n")
	}
	hdr.WriteString("Subject: " + msg.Subject + "\r\n")
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
			"Content-Disposition":       {fmt.Sprintf("attachment; filename=\"%s\"", att.Name)},
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
