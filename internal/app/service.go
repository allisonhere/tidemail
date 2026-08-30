// Package app contains TideMail's frontend-neutral application service.
// Terminal and desktop frontends should express user intent through this
// package instead of coordinating SQLite, IMAP, SMTP, and AI independently.
package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/allisonhere/tide/internal/ai"
	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	imapclient "github.com/allisonhere/tide/internal/imap"
	"github.com/allisonhere/tide/internal/smtp"
)

const operationTimeout = 60 * time.Second

type Event struct {
	Name string `json:"name"`
	Data any    `json:"data,omitempty"`
}

type EventSink func(Event)

type Bootstrap struct {
	Accounts  []db.Account `json:"accounts"`
	Mailboxes []db.Mailbox `json:"mailboxes"`
	Config    PublicConfig `json:"config"`
}

// PublicConfig contains presentation preferences and account labels only.
// Credentials and provider keys must never cross the desktop bridge.
type PublicConfig struct {
	Theme      string               `json:"theme"`
	Display    config.DisplayConfig `json:"display"`
	Accounts   []AccountIdentity    `json:"accounts"`
	AIProvider string               `json:"aiProvider"`
}

type AccountIdentity struct {
	Name string `json:"name"`
	User string `json:"user"`
	From string `json:"from"`
}

type MessagePage struct {
	MailboxID int64        `json:"mailboxId"`
	Query     string       `json:"query,omitempty"`
	Messages  []db.Message `json:"messages"`
}

type MessageDetail struct {
	Message     db.Message       `json:"message"`
	Attachments []AttachmentInfo `json:"attachments"`
}

type AttachmentInfo struct {
	ID          int64  `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
}

type SyncResult struct {
	MailboxID int64 `json:"mailboxId"`
	Fetched   int   `json:"fetched"`
	New       int   `json:"new"`
}

type ComposeRequest struct {
	AccountName string   `json:"accountName"`
	To          []string `json:"to"`
	CC          []string `json:"cc"`
	BCC         []string `json:"bcc"`
	Subject     string   `json:"subject"`
	Body        string   `json:"body"`
	InReplyTo   string   `json:"inReplyTo"`
	References  string   `json:"references"`
	Attachments []struct {
		Name string `json:"name"`
		Data []byte `json:"data"`
	} `json:"attachments"`
}

type Service struct {
	db       *db.DB
	sessions *imapclient.SessionPool
	ownsPool bool

	mu   sync.RWMutex
	cfg  config.Config
	sink EventSink
}

func New(database *db.DB, cfg config.Config, sink EventSink) *Service {
	return &Service{db: database, cfg: cfg, sessions: imapclient.NewSessionPool(), ownsPool: true, sink: sink}
}

// NewWithSessions lets another frontend lifecycle own the shared IMAP pool.
// This is used by the TUI while its remaining orchestration is migrated.
func NewWithSessions(database *db.DB, cfg config.Config, sessions *imapclient.SessionPool, sink EventSink) *Service {
	return &Service{db: database, cfg: cfg, sessions: sessions, sink: sink}
}

func (s *Service) Close() {
	if s != nil && s.sessions != nil && s.ownsPool {
		s.sessions.Close()
	}
}

func (s *Service) emit(name string, data any) {
	if s.sink != nil {
		s.sink(Event{Name: name, Data: data})
	}
}

func (s *Service) Bootstrap() (Bootstrap, error) {
	if err := s.ensureConfiguredAccounts(); err != nil {
		return Bootstrap{}, err
	}
	accounts, err := s.db.ListAccounts()
	if err != nil {
		return Bootstrap{}, err
	}
	mailboxes := make([]db.Mailbox, 0)
	for _, account := range accounts {
		rows, err := s.db.ListMailboxes(account.ID)
		if err != nil {
			return Bootstrap{}, err
		}
		mailboxes = append(mailboxes, rows...)
	}
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()
	identities := make([]AccountIdentity, 0, len(cfg.Accounts))
	for _, account := range cfg.Accounts {
		identities = append(identities, AccountIdentity{Name: account.Name, User: account.User, From: account.From})
	}
	return Bootstrap{Accounts: accounts, Mailboxes: mailboxes, Config: PublicConfig{
		Theme: cfg.Theme, Display: cfg.Display, Accounts: identities, AIProvider: cfg.AI.Provider,
	}}, nil
}

func (s *Service) ListMessages(mailboxID int64, unified, unreadOnly bool) (MessagePage, error) {
	s.mu.RLock()
	unreadFirst := s.cfg.Display.UnreadFirst
	s.mu.RUnlock()
	var (
		messages []db.Message
		err      error
	)
	if unified {
		if unreadFirst {
			messages, err = s.db.ListUnifiedInboxUnreadFirst(unreadOnly)
		} else {
			messages, err = s.db.ListUnifiedInbox(unreadOnly)
		}
	} else if unreadOnly {
		messages, err = s.db.ListUnreadMessages(mailboxID)
	} else if unreadFirst {
		messages, err = s.db.ListMessagesUnreadFirst(mailboxID)
	} else {
		messages, err = s.db.ListMessages(mailboxID)
	}
	return MessagePage{MailboxID: mailboxID, Messages: messages}, err
}

func (s *Service) Search(query string) (MessagePage, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return MessagePage{Messages: []db.Message{}}, nil
	}
	s.mu.RLock()
	unreadFirst := s.cfg.Display.UnreadFirst
	s.mu.RUnlock()
	messages, err := s.db.SearchAllMessages(query, unreadFirst)
	return MessagePage{Query: query, Messages: messages}, err
}

func (s *Service) SyncMailbox(ctx context.Context, mailboxID int64) (SyncResult, error) {
	mailbox, err := s.db.GetMailbox(mailboxID)
	if err != nil {
		return SyncResult{}, err
	}
	account, err := s.db.GetAccount(mailbox.AccountID)
	if err != nil {
		return SyncResult{}, err
	}
	acfg, err := s.accountConfig(account.Name)
	if err != nil {
		return SyncResult{}, err
	}
	s.emit("sync.started", map[string]any{"mailboxId": mailboxID})
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	result := SyncResult{MailboxID: mailboxID}
	err = s.sessions.Do(ctx, acfg, func(client *imapclient.Client) error {
		since := mailbox.LastSynced
		serverMessages, uidValidity, stateErr := client.ServerState(ctx, mailbox.Name)
		if stateErr == nil && uidValidity != 0 {
			stored, readErr := s.db.MailboxUIDValidity(mailboxID)
			if readErr == nil && stored != 0 && stored != uidValidity {
				if err := s.db.ResetMailboxCache(mailboxID); err != nil {
					return err
				}
				since = time.Time{}
			}
			if err := s.db.SetMailboxUIDValidity(mailboxID, uidValidity); err != nil {
				return err
			}
		}
		if count, countErr := s.db.CountMessages(mailboxID); countErr == nil && count == 0 {
			since = time.Time{}
		}
		fetched, err := client.FetchSince(ctx, mailbox.Name, since)
		if err != nil {
			return err
		}
		result.Fetched = len(fetched)
		for i := range fetched {
			fetched[i].MailboxID = mailboxID
			deleted, err := s.db.MessageDeletedLocally(mailboxID, fetched[i].UID, fetched[i].MessageID)
			if err != nil {
				return err
			}
			if deleted {
				continue
			}
			existed, err := s.db.MessageExists(mailboxID, fetched[i].UID)
			if err != nil {
				return err
			}
			if err := s.db.UpsertMessage(fetched[i]); err != nil {
				return err
			}
			if !existed && !fetched[i].Read {
				result.New++
			}
		}
		if stateErr == nil {
			uids := make([]uint32, 0, len(serverMessages)+len(fetched))
			seen := make(map[uint32]bool, len(serverMessages))
			starred := make(map[uint32]bool, len(serverMessages))
			for _, message := range serverMessages {
				uids = append(uids, message.UID)
				seen[message.UID] = message.Seen
				starred[message.UID] = message.Flagged
			}
			for _, message := range fetched {
				uids = append(uids, message.UID)
			}
			if _, err := s.db.ReconcileMailboxUIDs(mailboxID, uids); err != nil {
				return err
			}
			if _, err := s.db.ApplyServerReadStates(mailboxID, seen); err != nil {
				return err
			}
			if _, err := s.db.ApplyServerStarredStates(mailboxID, starred); err != nil {
				return err
			}
		}
		if err := s.db.SetMailboxLastSynced(mailboxID, time.Now()); err != nil {
			return err
		}
		s.refreshUnread(mailboxID)
		return nil
	})
	if err != nil {
		s.emit("sync.failed", map[string]any{"mailboxId": mailboxID, "error": err.Error()})
		return SyncResult{}, err
	}
	s.emit("sync.completed", result)
	s.emit("messages.changed", map[string]any{"mailboxId": mailboxID})
	return result, nil
}

func (s *Service) Message(id int64) (MessageDetail, error) {
	message, err := s.db.GetMessage(id)
	if err != nil {
		return MessageDetail{}, err
	}
	attachments, err := s.db.GetAttachments(id)
	if err != nil {
		return MessageDetail{}, err
	}
	metadata := make([]AttachmentInfo, 0, len(attachments))
	for _, attachment := range attachments {
		metadata = append(metadata, AttachmentInfo{
			ID: attachment.ID, Filename: attachment.Filename, ContentType: attachment.ContentType, Size: attachment.Size,
		})
	}
	return MessageDetail{Message: message, Attachments: metadata}, nil
}

func (s *Service) AttachmentData(messageID, attachmentID int64) (db.Attachment, error) {
	attachments, err := s.db.GetAttachments(messageID)
	if err != nil {
		return db.Attachment{}, err
	}
	for _, attachment := range attachments {
		if attachment.ID == attachmentID {
			return attachment, nil
		}
	}
	return db.Attachment{}, fmt.Errorf("attachment %d not found", attachmentID)
}

func (s *Service) SetRead(ctx context.Context, id int64, read bool) error {
	if _, err := s.db.GetMessage(id); errors.Is(err, sql.ErrNoRows) {
		return s.db.MarkRead(id, read)
	}
	message, mailbox, acfg, err := s.resolveMessage(id)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	if acfg.IMAPHost != "" && message.UID != 0 {
		if err := s.sessions.Do(ctx, acfg, func(client *imapclient.Client) error {
			return client.MarkSeen(ctx, mailbox.Name, message.UID, read)
		}); err != nil {
			return err
		}
	}
	if err := s.db.MarkRead(id, read); err != nil {
		return err
	}
	s.refreshUnread(mailbox.ID)
	s.emit("messages.changed", map[string]any{"mailboxId": mailbox.ID, "messageId": id})
	return nil
}

func (s *Service) SetStarred(ctx context.Context, id int64, starred bool) error {
	if _, err := s.db.GetMessage(id); errors.Is(err, sql.ErrNoRows) {
		return s.db.MarkStarred(id, starred)
	}
	message, mailbox, acfg, err := s.resolveMessage(id)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	if acfg.IMAPHost != "" && message.UID != 0 {
		if err := s.sessions.Do(ctx, acfg, func(client *imapclient.Client) error {
			return client.MarkFlagged(ctx, mailbox.Name, message.UID, starred)
		}); err != nil {
			return err
		}
	}
	if err := s.db.MarkStarred(id, starred); err != nil {
		return err
	}
	s.emit("messages.changed", map[string]any{"mailboxId": mailbox.ID, "messageId": id})
	return nil
}

func (s *Service) Move(ctx context.Context, id, targetMailboxID int64) error {
	message, source, acfg, err := s.resolveMessage(id)
	if err != nil {
		return err
	}
	target, err := s.db.GetMailbox(targetMailboxID)
	if err != nil {
		return err
	}
	if source.AccountID != target.AccountID {
		return fmt.Errorf("cannot move a message between accounts")
	}
	if acfg.IMAPHost == "" || message.UID == 0 {
		if err := s.db.MoveMessage(id, target.ID); err != nil {
			return err
		}
		s.refreshUnread(source.ID)
		s.refreshUnread(target.ID)
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	if err := s.sessions.Do(ctx, acfg, func(client *imapclient.Client) error {
		return client.MoveMessage(ctx, source.Name, message.UID, target.Name)
	}); err != nil {
		return err
	}
	if err := s.db.MoveMessage(id, target.ID); err != nil {
		return err
	}
	s.refreshUnread(source.ID)
	s.refreshUnread(target.ID)
	s.emit("messages.changed", map[string]any{"mailboxId": source.ID, "targetMailboxId": target.ID})
	return nil
}

func (s *Service) Archive(ctx context.Context, id int64) error {
	_, source, _, err := s.resolveMessage(id)
	if err != nil {
		return err
	}
	target, err := s.db.FindArchiveMailbox(source.AccountID)
	if err != nil {
		return err
	}
	return s.Move(ctx, id, target.ID)
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	message, source, acfg, err := s.resolveMessage(id)
	if err != nil {
		return err
	}
	if trash, trashErr := s.db.FindTrashMailbox(source.AccountID); trashErr == nil && trash.ID != source.ID {
		return s.Move(ctx, id, trash.ID)
	}
	if acfg.IMAPHost == "" || message.UID == 0 {
		return s.db.DeleteMessage(id)
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	if err := s.sessions.Do(ctx, acfg, func(client *imapclient.Client) error {
		return client.DeleteMessage(ctx, source.Name, message.UID)
	}); err != nil {
		return err
	}
	if err := s.db.DeleteMessage(id); err != nil {
		return err
	}
	s.refreshUnread(source.ID)
	s.emit("messages.changed", map[string]any{"mailboxId": source.ID})
	return nil
}

func (s *Service) Send(ctx context.Context, request ComposeRequest) error {
	acfg, err := s.accountConfig(request.AccountName)
	if err != nil {
		return err
	}
	attachments := make([]smtp.Attachment, 0, len(request.Attachments))
	for _, attachment := range request.Attachments {
		attachments = append(attachments, smtp.Attachment{Name: attachment.Name, Data: attachment.Data})
	}
	message := smtp.OutgoingMessage{
		To: request.To, CC: request.CC, BCC: request.BCC, Subject: request.Subject,
		Body: request.Body, InReplyTo: request.InReplyTo, References: request.References,
		Attachments: attachments,
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	if err := smtp.Send(ctx, acfg, message); err != nil {
		return err
	}
	s.emit("send.completed", map[string]any{"accountName": request.AccountName})
	return nil
}

func (s *Service) SaveDraft(draft db.Draft) (int64, error) {
	draft.Dirty = true
	id, err := s.db.SaveDraft(draft)
	if err == nil {
		s.emit("drafts.changed", map[string]any{"draftId": id})
	}
	return id, err
}

func (s *Service) Draft(id int64) (db.Draft, error) { return s.db.GetDraft(id) }

func (s *Service) DeleteDraft(id int64) error {
	err := s.db.DeleteDraft(id)
	if err == nil {
		s.emit("drafts.changed", map[string]any{"draftId": id})
	}
	return err
}

func (s *Service) Contacts() ([]db.Contact, error) { return s.db.ListContacts() }

func (s *Service) Rules() ([]db.RuleRecord, error) { return s.db.ListRules() }

func (s *Service) Summarize(ctx context.Context, messageID int64) (string, error) {
	message, err := s.db.GetMessage(messageID)
	if err != nil {
		return "", err
	}
	s.mu.RLock()
	summarizer, err := ai.New(s.cfg.AI)
	s.mu.RUnlock()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	summary, err := summarizer.Summarize(ctx, message.Subject, message.BodyText)
	if err == nil {
		err = s.db.SaveSummary(messageID, summary)
	}
	return summary, err
}

func (s *Service) CheckGrammar(ctx context.Context, body string) (string, error) {
	s.mu.RLock()
	summarizer, err := ai.New(s.cfg.AI)
	s.mu.RUnlock()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	return summarizer.CheckGrammar(ctx, body)
}

func (s *Service) SaveConfig(cfg config.Config) error {
	if err := config.Save(cfg); err != nil {
		return err
	}
	s.mu.Lock()
	s.cfg = cfg
	s.mu.Unlock()
	s.emit("config.changed", nil)
	return nil
}

// Config returns a detached copy for trusted local frontends that provide a
// settings editor. Callers must not log the returned secrets.
func (s *Service) Config() config.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cfg := s.cfg
	cfg.Accounts = append([]config.AccountConfig(nil), s.cfg.Accounts...)
	return cfg
}

func (s *Service) resolveMessage(id int64) (db.Message, db.Mailbox, config.AccountConfig, error) {
	message, err := s.db.GetMessage(id)
	if err != nil {
		return db.Message{}, db.Mailbox{}, config.AccountConfig{}, err
	}
	mailbox, err := s.db.GetMailbox(message.MailboxID)
	if err != nil {
		return db.Message{}, db.Mailbox{}, config.AccountConfig{}, err
	}
	account, err := s.db.GetAccount(mailbox.AccountID)
	if err != nil {
		return db.Message{}, db.Mailbox{}, config.AccountConfig{}, err
	}
	acfg, _ := s.accountConfig(account.Name)
	return message, mailbox, acfg, nil
}

func (s *Service) accountConfig(name string) (config.AccountConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, account := range s.cfg.Accounts {
		if account.Name == name {
			return account, nil
		}
	}
	return config.AccountConfig{}, fmt.Errorf("account %q is not configured", name)
}

func (s *Service) refreshUnread(mailboxID int64) {
	unread, err := s.db.CountUnread(mailboxID)
	if err == nil {
		_ = s.db.SetMailboxUnreadCount(mailboxID, unread) //nolint:errcheck
	}
}

func (s *Service) ensureConfiguredAccounts() error {
	accounts, err := s.db.ListAccounts()
	if err != nil {
		return err
	}
	existing := make(map[string]db.Account, len(accounts))
	for _, account := range accounts {
		existing[strings.TrimSpace(account.Name)] = account
	}
	s.mu.RLock()
	configured := append([]config.AccountConfig(nil), s.cfg.Accounts...)
	s.mu.RUnlock()
	for _, accountCfg := range configured {
		name := strings.TrimSpace(accountCfg.Name)
		if name == "" {
			continue
		}
		account, ok := existing[name]
		if !ok {
			id, err := s.db.AddAccount(name, "")
			if err != nil {
				return fmt.Errorf("import configured account %s: %w", name, err)
			}
			account = db.Account{ID: id, Name: name}
			existing[name] = account
		}
		mailboxes, err := s.db.ListMailboxes(account.ID)
		if err != nil {
			return err
		}
		if len(mailboxes) == 0 {
			_, err = s.db.UpsertMailbox(db.Mailbox{
				AccountID: account.ID, Name: "INBOX", DisplayName: "Inbox", Delimiter: "/", Flags: []string{`\Inbox`},
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}
