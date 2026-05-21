package smtp

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/allisonhere/tide/internal/config"
)

type OutgoingMessage struct {
	From       string
	To         []string
	CC         []string
	Subject    string
	Body       string
	InReplyTo  string
	References string
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
	dialer := &net.Dialer{Timeout: 30 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("dial smtp: %w", err)
	}

	host, _, _ := net.SplitHostPort(addr)
	client, err := smtp.NewClient(conn, host)
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

	dialer := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: 30 * time.Second},
		Config:    tlsCfg,
	}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("dial smtp tls: %w", err)
	}

	client, err := smtp.NewClient(conn, host)
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
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + strings.Join(msg.To, ", ") + "\r\n")
	if len(msg.CC) > 0 {
		b.WriteString("Cc: " + strings.Join(msg.CC, ", ") + "\r\n")
	}
	b.WriteString("Subject: " + msg.Subject + "\r\n")
	if msg.InReplyTo != "" {
		b.WriteString("In-Reply-To: " + msg.InReplyTo + "\r\n")
	}
	if msg.References != "" {
		b.WriteString("References: " + msg.References + "\r\n")
	}
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(msg.Body)
	return []byte(b.String())
}
