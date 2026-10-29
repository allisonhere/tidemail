package ui

import (
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/smtp"
	tea "github.com/charmbracelet/bubbletea"
)

type mailtoFields struct{ to, cc, bcc, subject, body string }

// mailto uses URI escaping, not HTML form encoding: a literal + stays a +.
// Decode each component once, and only recognize fields a new composer owns.
func parseComposeMailto(target string) (mailtoFields, error) {
	var f mailtoFields
	u, err := url.Parse(target)
	if err != nil || !strings.EqualFold(u.Scheme, "mailto") || u.Host != "" || u.Path != "" || u.Fragment != "" {
		return f, fmt.Errorf("invalid mailto URL")
	}
	f.to, err = url.PathUnescape(u.Opaque)
	if err != nil {
		return f, fmt.Errorf("invalid mailto recipient encoding")
	}
	fields := map[string][]string{}
	for _, part := range strings.Split(u.RawQuery, "&") {
		key, value, _ := strings.Cut(part, "=")
		key, err = url.PathUnescape(key)
		if err != nil {
			return f, fmt.Errorf("invalid mailto field encoding")
		}
		value, err = url.PathUnescape(value)
		if err != nil {
			return f, fmt.Errorf("invalid mailto value encoding")
		}
		fields[strings.ToLower(key)] = append(fields[strings.ToLower(key)], value)
	}
	for _, entry := range []struct {
		name string
		dest *string
	}{{"to", &f.to}, {"cc", &f.cc}, {"bcc", &f.bcc}} {
		parts := []string{}
		if *entry.dest != "" {
			parts = append(parts, *entry.dest)
		}
		for _, v := range fields[entry.name] {
			if strings.TrimSpace(v) != "" {
				parts = append(parts, v)
			}
		}
		*entry.dest = strings.Join(parts, ", ")
		if !safeMailtoText(*entry.dest, false) {
			return f, fmt.Errorf("invalid mailto %s", entry.name)
		}
		if *entry.dest != "" {
			if msg := validateAddressList(*entry.dest); msg != "" {
				return f, fmt.Errorf("%s", msg)
			}
			for _, addr := range parseAddressList(*entry.dest) {
				if !strings.Contains(addr, "@") {
					return f, fmt.Errorf("invalid mailto address")
				}
			}
		}
	}
	for _, entry := range []struct {
		name string
		dest *string
	}{{"subject", &f.subject}, {"body", &f.body}} {
		vals := fields[entry.name]
		if len(vals) > 1 {
			return f, fmt.Errorf("repeated mailto %s", entry.name)
		}
		if len(vals) == 1 {
			*entry.dest = vals[0]
		}
		if !safeMailtoText(*entry.dest, entry.name == "body") {
			return f, fmt.Errorf("invalid mailto %s", entry.name)
		}
	}
	f.body = strings.ReplaceAll(strings.ReplaceAll(f.body, "\r\n", "\n"), "\r", "\n")
	return f, nil
}

func safeMailtoText(s string, body bool) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if body && (r == '\r' || r == '\n' || r == '\t') {
			continue
		}
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return false
		}
	}
	return true
}

func usableMailtoAccount(a config.AccountConfig) bool {
	sender := smtp.AccountSender(a)
	addr, err := mail.ParseAddress(sender)
	return err == nil && strings.Contains(addr.Address, "@") && !strings.ContainsAny(sender, "\r\n") && strings.TrimSpace(a.SMTPHost) != ""
}

// Resolve the owner of the displayed link, not the sidebar or To header. The
// merged thread footer deduplicates targets; its first displayed occurrence owns
// the entry.
func (m Model) mailtoSourceMessage(target string) *db.Message {
	if !m.threadedMessagesEnabled() {
		return m.currentContentMessage()
	}
	cur := m.currentRowMessage()
	if cur == nil || cur.ID != m.contentMessageID {
		return nil
	}
	messages := m.currentRowMessages()
	for _, msg := range messages {
		result := m.renderMessageForDisplay(msg, m.contentBodyWidth())
		for _, link := range result.links {
			if link == target {
				copy := msg
				return &copy
			}
		}
	}
	return nil
}

func (m *Model) openReaderLink(target string) tea.Cmd {
	if !isMailtoDestination(target) {
		return m.openBrowserCmd(target)
	}
	fields, err := parseComposeMailto(target)
	if err != nil {
		m.setStatus("cannot compose: "+err.Error(), true)
		return m.clearStatusCmd()
	}
	var account config.AccountConfig
	if source := m.mailtoSourceMessage(target); source != nil {
		account, _ = m.accountCfgForMailbox(source.MailboxID)
	}
	if !usableMailtoAccount(account) {
		account, _ = m.cfg.DefaultAccount()
	}
	if !usableMailtoAccount(account) {
		m.setStatus("cannot compose: no usable sending account; configure a sender and SMTP server", true)
		return m.clearStatusCmd()
	}
	c := NewCompose(account, m.cfg.Accounts, m.addressBook)
	// Keep complete URL fields rather than silently truncating at the normal
	// input limit. Completion, editing, validation and draft saving stay native.
	c.toInput.CharLimit = max(c.toInput.CharLimit, utf8.RuneCountInString(fields.to))
	c.ccInput.CharLimit = max(c.ccInput.CharLimit, utf8.RuneCountInString(fields.cc))
	c.bccInput.CharLimit = max(c.bccInput.CharLimit, utf8.RuneCountInString(fields.bcc))
	c.subjectInput.CharLimit = max(c.subjectInput.CharLimit, utf8.RuneCountInString(fields.subject))
	c.toInput.SetValue(fields.to)
	c.ccInput.SetValue(fields.cc)
	c.bccInput.SetValue(fields.bcc)
	c.subjectInput.SetValue(fields.subject)
	// The marker resolves to the selected account's signature during preparation.
	c.setInitialBody(fields.body, "")
	m.compose = c
	m.overlay = overlayCompose
	return nil
}
