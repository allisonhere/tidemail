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

type Attachment struct {
	Name string
	Data []byte
}

type OutgoingMessage struct {
	From       string
	To         []string
	CC         []string
	Subject    string
	Body       string
	InReplyTo  string
	References string
	Attachments []Attachment
}

func Send(ctx context.Context, cfg config.AccountConfig, msg OutgoingMessage) error {
	from := cfg.From
	if from == "" {
		from = cfg.User
	}

	var allTo []string
	allTo = append(allTo, msg.To...)
	allTo = append(allTo, msg.CC...)
	if len(allTo) == 0 {
		return fmt.Errorf("no recipients")
	}

	raw := buildRaw(from, msg)

	addr := fmt.Sprintf("%s:%d", cfg.SMTPHost, cfg.SMTPPort)

	if cfg.SMTPPort == 465 {
		return sendTLS(ctx, addr, cfg, from, allTo, raw)
	}
	return sendSTARTTLS(ctx, addr, cfg, from, allTo, raw)
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

	if err := client.Auth(smtp.PlainAuth("", cfg.User, cfg.Password, host)); err != nil {
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

	if err := client.Auth(smtp.PlainAuth("", cfg.User, cfg.Password, host)); err != nil {
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
	w.Write([]byte(msg.Body))

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
			w.Write([]byte(encoded[i:end] + "\r\n"))
		}
	}

	mw.Close()
	return buf.Bytes()
}
