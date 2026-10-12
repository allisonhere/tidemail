package mailapp

import (
	"fmt"
	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/smtp"
	"net/mail"
	"strings"
)

func ResolveAccount(cfg config.Config, selector string) (config.AccountConfig, error) {
	if selector == "" {
		if a, ok := cfg.DefaultAccount(); ok {
			return a, nil
		}
		return config.AccountConfig{}, fmt.Errorf("no configured account; add an account in TideMail")
	}
	if a, ok := cfg.AccountByID(selector); ok {
		return a, nil
	}
	var matches []config.AccountConfig
	for _, a := range cfg.Accounts {
		if strings.EqualFold(a.Name, selector) {
			matches = append(matches, a)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return config.AccountConfig{}, fmt.Errorf("account name is ambiguous; use its stable ID")
	}
	return config.AccountConfig{}, fmt.Errorf("account not found; run tidemail accounts list")
}

func addresses(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	if strings.ContainsAny(value, "\r\n") {
		return nil, fmt.Errorf("recipient headers cannot contain newlines")
	}
	parsed, err := mail.ParseAddressList(value)
	if err != nil {
		return nil, fmt.Errorf("invalid recipient address")
	}
	result := make([]string, 0, len(parsed))
	for _, a := range parsed {
		if !strings.Contains(a.Address, "@") {
			return nil, fmt.Errorf("recipient must contain a domain")
		}
		result = append(result, a.Address)
	}
	return result, nil
}

// Prepare shares compose rendering and identity validation between front ends.
func Prepare(account config.AccountConfig, draft db.Draft, signature bool) (smtp.OutgoingMessage, error) {
	var msg smtp.OutgoingMessage
	var err error
	if msg.To, err = addresses(draft.To); err != nil {
		return msg, err
	}
	if msg.CC, err = addresses(draft.CC); err != nil {
		return msg, err
	}
	if msg.BCC, err = addresses(draft.BCC); err != nil {
		return msg, err
	}
	if len(msg.To)+len(msg.CC)+len(msg.BCC) == 0 {
		return msg, fmt.Errorf("at least one recipient is required")
	}
	msg.From = smtp.AccountSender(account)
	if strings.ContainsAny(msg.From+draft.Subject+draft.InReplyTo+draft.References, "\r\n") {
		return msg, fmt.Errorf("message headers cannot contain newlines")
	}
	if a, e := mail.ParseAddress(msg.From); e != nil || !strings.Contains(a.Address, "@") {
		return msg, fmt.Errorf("configure a valid sender address for this account")
	}
	msg.Subject = draft.Subject
	msg.Body = draft.BodyText
	msg.HTMLBody = smtp.MarkdownToHTML(draft.BodyText)
	msg.InReplyTo = draft.InReplyTo
	msg.References = draft.References
	if sig := strings.TrimSpace(account.Signature); signature && sig != "" {
		msg.Body = strings.TrimRight(msg.Body, "\n") + "\n\n-- \n" + sig + "\n"
		msg.HTMLBody += smtp.SignatureHTML(sig)
	}
	for _, a := range draft.Attachments {
		if strings.ContainsAny(a.Filename, "\r\n") {
			return msg, fmt.Errorf("attachment filename cannot contain newlines")
		}
		msg.Attachments = append(msg.Attachments, smtp.Attachment{Name: a.Filename, Data: a.Data})
	}
	return msg, nil
}

func SaveDraft(database *db.DB, account config.AccountConfig, draft db.Draft) (int64, error) {
	draft.AccountConfigID = account.ID
	draft.AccountName = account.Name
	draft.AccountUser = account.User
	draft.Dirty = true
	return database.SaveDraft(draft)
}

func Allowed(cfg config.Config, capability string) bool {
	permissions := cfg.AgentPermissions
	if permissions == nil {
		permissions = []string{"accounts.read", "messages.metadata", "messages.search", "drafts.create", "outbox.read"}
	}
	for _, p := range permissions {
		if p == capability {
			return true
		}
	}
	return false
}

func ValidateRecipients(value string, required bool) error {
	recipients, err := addresses(value)
	if err != nil {
		return err
	}
	if required && len(recipients) == 0 {
		return fmt.Errorf("at least one recipient is required")
	}
	return nil
}
