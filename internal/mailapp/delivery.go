package mailapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	imapClient "github.com/allisonhere/tidemail/internal/imap"
	"github.com/allisonhere/tidemail/internal/smtp"
	"strings"
	"time"
)

type Result struct {
	Err, CleanupErr, SentCopyErr error
	Skipped                      bool
}
type Delivery struct {
	Send   func(context.Context, config.AccountConfig, smtp.OutgoingMessage) error
	Append func(*db.DB, *imapClient.SessionPool, config.AccountConfig, smtp.OutgoingMessage) error
}

func Enqueue(database *db.DB, cfg config.Config, account config.AccountConfig, msg smtp.OutgoingMessage, draft db.Draft, dueAt time.Time) (int64, error) {
	draft.AccountConfigID = account.ID
	draft.AccountName = account.Name
	draft.AccountUser = account.User
	msg.EnsureIdentity(smtp.AccountSender(account))
	payload, err := json.Marshal(msg)
	if err != nil {
		return 0, err
	}
	draftJSON, err := json.Marshal(draft)
	if err != nil {
		return 0, err
	}
	if dueAt.IsZero() {
		dueAt = time.Now().Add(time.Duration(cfg.Display.SendDelaySeconds) * time.Second)
	}
	recipients := append(append([]string{}, msg.To...), msg.CC...)
	return database.EnqueueOutbox(db.OutboxItem{AccountConfigID: account.ID, AccountName: account.Name, AccountUser: account.User, DraftID: draft.ID, Subject: msg.Subject, Recipients: strings.Join(recipients, ", "), MessageJSON: payload, DraftJSON: draftJSON, MaxAttempts: config.NormalizeSendMaxAttempts(cfg.Display.SendMaxAttempts), NextAttempt: dueAt.Unix()})
}

func ResolveOutboxAccount(cfg config.Config, item db.OutboxItem) (config.AccountConfig, bool) {
	for _, a := range cfg.Accounts {
		if item.AccountConfigID != "" {
			if a.ID == item.AccountConfigID {
				return a, true
			}
		} else if a.Name == item.AccountName && a.User == item.AccountUser {
			return a, true
		}
	}
	return config.AccountConfig{}, false
}

func OutboxMessage(item db.OutboxItem, account config.AccountConfig) (smtp.OutgoingMessage, error) {
	var msg smtp.OutgoingMessage
	err := json.Unmarshal(item.MessageJSON, &msg)
	msg.From = smtp.AccountSender(account)
	return msg, err
}

func Deliver(ctx context.Context, database *db.DB, sessions *imapClient.SessionPool, id int64, account config.AccountConfig, force bool, cleanup func() error) Result {
	return (Delivery{}).Deliver(ctx, database, sessions, id, account, force, cleanup)
}

func (d Delivery) Deliver(ctx context.Context, database *db.DB, sessions *imapClient.SessionPool, id int64, account config.AccountConfig, force bool, cleanup func() error) Result {
	var result Result
	item, err := database.GetOutbox(id)
	if err != nil {
		result.Err = err
		return result
	}
	if item.State != db.OutboxQueued {
		result.Skipped = true
		return result
	}
	msg, err := OutboxMessage(item, account)
	if err != nil {
		result.Err = err
		return result
	}
	var claimed bool
	if force {
		claimed, err = database.ClaimOutbox(id)
	} else {
		claimed, err = database.ClaimDueOutbox(id)
	}
	if err != nil {
		result.Err = err
		return result
	}
	if !claimed {
		result.Skipped = true
		return result
	}
	// Validate again after account resolution: configuration may have changed.
	validationDraft := db.Draft{To: strings.Join(msg.To, ", "), CC: strings.Join(msg.CC, ", "), BCC: strings.Join(msg.BCC, ", "), Subject: msg.Subject, InReplyTo: msg.InReplyTo, References: msg.References}
	if _, validationErr := Prepare(account, validationDraft, false); validationErr != nil {
		result.Err = validationErr
		result.CleanupErr = database.FinishOutbox(id, db.OutboxFailed, validationErr.Error(), 0)
		return result
	}
	msg.EnsureIdentity(msg.From)
	// Persist identity before SMTP so retries retain the same Message-ID.
	payload, err := json.Marshal(msg)
	if err == nil {
		_, err = database.Exec(`UPDATE outbox SET message_json=? WHERE id=? AND state='sending'`, payload, id)
	}
	if err != nil {
		result.Err = err
		_ = database.FinishOutbox(id, db.OutboxFailed, "Cannot persist message identity", 0)
		return result
	}
	send := d.Send
	if send == nil {
		send = smtp.Send
	}
	sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	err = send(sendCtx, account, msg)
	cancel()
	state, next, message := db.OutboxSent, int64(0), ""
	if err != nil {
		result.Err = err
		message = config.RedactSecrets(err.Error(), config.Config{Accounts: []config.AccountConfig{account}})
		state = db.OutboxFailed
		if errors.Is(err, smtp.ErrDeliveryUncertain) {
			state = db.OutboxUncertain
		} else if smtp.CanRetry(err) && item.Attempts+1 < item.MaxAttempts {
			state = db.OutboxQueued
			next = time.Now().Add(time.Minute).Unix()
		}
	}
	if saveErr := database.FinishOutbox(id, state, message, next); saveErr != nil {
		if err == nil {
			result.CleanupErr = fmt.Errorf("record sent status: %w", saveErr)
		} else {
			result.Err = fmt.Errorf("%v; save delivery status: %w", err, saveErr)
		}
		return result
	}
	if err == nil {
		appendSent := d.Append
		if appendSent == nil {
			appendSent = AppendToSentFolder
		}
		result.SentCopyErr = appendSent(database, sessions, account, msg)
		if item.DraftID != 0 {
			if cleanup != nil {
				result.CleanupErr = cleanup()
			} else {
				result.CleanupErr = database.DeleteDraft(item.DraftID)
			}
		}
	}
	return result
}

func ServerFilesSentMail(acfg config.AccountConfig) bool {
	host := strings.ToLower(strings.TrimSpace(acfg.SMTPHost))
	if host == "smtp.gmail.com" || host == "smtp-relay.gmail.com" {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(acfg.Provider), "Gmail")
}

// AppendToSentFolder puts a copy of a just-delivered message in the account's
// Sent folder. Any error is returned for reporting only — the mail is already
// delivered and recorded, so there is nothing to retry and nothing to fail.
func AppendToSentFolder(database *db.DB, sessions *imapClient.SessionPool, acfg config.AccountConfig, msg smtp.OutgoingMessage) error {
	if database == nil || sessions == nil || strings.TrimSpace(acfg.IMAPHost) == "" {
		return nil
	}
	if ServerFilesSentMail(acfg) {
		return nil
	}
	accountID, err := database.AccountIDByConfigID(acfg.ID)
	if err != nil {
		return err
	}
	// Report rather than guess: creating folders on someone's server as a side
	// effect of sending mail is too aggressive.
	sent, err := database.FindSentMailbox(accountID)
	if err != nil {
		return err
	}
	raw := smtp.BuildRaw(acfg, msg)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return sessions.Do(ctx, acfg, func(client *imapClient.Client) error {
		return client.AppendSent(ctx, sent.Name, raw, msg.Date)
	})
}
