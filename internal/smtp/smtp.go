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

	"github.com/allisonhere/tide/internal/auth"
	"github.com/allisonhere/tide/internal/config"
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

// xoauth2Auth implements the smtp.Auth interface for XOAUTH2.
type xoauth2Auth struct {
	user, token string
}

func (a *xoauth2Auth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	// Return the RAW initial-response bytes. net/smtp's Client.Auth base64-encodes
	// whatever Start returns before sending "AUTH XOAUTH2 <base64>"; encoding here
	// too would double-encode and Gmail rejects it with "501 5.5.2 Cannot Decode".
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

// smtpAuth returns the appropriate smtp.Auth for the given config.
func smtpAuth(cfg config.AccountConfig, host string) (smtp.Auth, error) {
	if cfg.UsesOAuth2() {
		accessToken := cfg.Password
		if cfg.RefreshToken != "" {
			tok, err := auth.RefreshAccessToken(cfg.ClientID, cfg.ClientSecret, cfg.RefreshToken)
			if err != nil {
				return nil, fmt.Errorf("oauth2 refresh: %w", err)
			}
			accessToken = tok.AccessToken
		}
		if accessToken == "" {
			return nil, fmt.Errorf("oauth2: no access token or refresh token available")
		}
		return &xoauth2Auth{user: cfg.User, token: accessToken}, nil
	}
	return smtp.PlainAuth("", cfg.User, cfg.Password, host), nil
}

func sendSTARTTLS(ctx context.Context, addr string, cfg config.AccountConfig, from string, to []string, raw []byte) error {
	conn, err := smtpDial(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("dial smtp: %w", err)
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

	// Plain text — no attachments
	if len(msg.Attachments) == 0 {
		hdr.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
		hdr.WriteString("\r\n")
		hdr.WriteString(msg.Body)
		return []byte(hdr.String())
	}

	// Multipart/mixed with attachments
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	hdr.WriteString("Content-Type: multipart/mixed; boundary=\"" + mw.Boundary() + "\"\r\n")
	hdr.WriteString("\r\n")
	buf.WriteString(hdr.String())

	// Body part
	bodyHdr := textproto.MIMEHeader{"Content-Type": {"text/plain; charset=utf-8"}}
	w, _ := mw.CreatePart(bodyHdr)
	_, _ = w.Write([]byte(msg.Body))

	// Attachment parts
	for _, att := range msg.Attachments {
		attHdr := textproto.MIMEHeader{
			"Content-Type":              {"application/octet-stream"},
			"Content-Disposition":       {fmt.Sprintf("attachment; filename=\"%s\"", att.Name)},
			"Content-Transfer-Encoding": {"base64"},
		}
		w, _ := mw.CreatePart(attHdr)
		encoded := base64.StdEncoding.EncodeToString(att.Data)
		// Wrap base64 at 76 chars per line
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
