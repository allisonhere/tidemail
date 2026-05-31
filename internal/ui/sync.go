package ui

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	imapClient "github.com/allisonhere/tide/internal/imap"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) loadAccountsCmd() tea.Cmd {
	database := m.db
	configuredAccounts := append([]config.AccountConfig(nil), m.cfg.Accounts...)
	return func() tea.Msg {
		accounts, err := database.ListAccounts()
		if err != nil {
			return AccountsLoadedMsg{Err: err}
		}
		if len(configuredAccounts) > 0 {
			accounts, err = ensureConfiguredAccounts(database, accounts, configuredAccounts)
			if err != nil {
				return AccountsLoadedMsg{Err: err}
			}
		}
		var mailboxes []db.Mailbox
		for _, a := range accounts {
			mbs, err2 := database.ListMailboxes(a.ID)
			if err2 != nil {
				return AccountsLoadedMsg{Err: err2}
			}
			mailboxes = append(mailboxes, mbs...)
		}
		return AccountsLoadedMsg{Accounts: accounts, Mailboxes: mailboxes}
	}
}

func ensureConfiguredAccounts(database *db.DB, accounts []db.Account, configs []config.AccountConfig) ([]db.Account, error) {
	existing := make(map[string]db.Account, len(accounts))
	for _, account := range accounts {
		existing[strings.TrimSpace(account.Name)] = account
	}
	changed := false
	for _, accountCfg := range configs {
		name := strings.TrimSpace(accountCfg.Name)
		if name == "" {
			continue
		}
		account, ok := existing[name]
		if !ok {
			accountID, err := database.AddAccount(name, "")
			if err != nil {
				return accounts, fmt.Errorf("import configured account %s: %w", name, err)
			}
			account = db.Account{ID: accountID, Name: name}
			existing[name] = account
			changed = true
		}
		mailboxes, err := database.ListMailboxes(account.ID)
		if err != nil {
			return accounts, fmt.Errorf("list imported account mailboxes %s: %w", name, err)
		}
		if len(mailboxes) == 0 {
			if _, err := database.UpsertMailbox(db.Mailbox{AccountID: account.ID, Name: "INBOX", DisplayName: cleanDisplayName("INBOX"), Delimiter: "/"}); err != nil {
				return accounts, fmt.Errorf("create starter inbox for %s: %w", name, err)
			}
			changed = true
		}
	}
	if !changed {
		return accounts, nil
	}
	return database.ListAccounts()
}

func (m *Model) loadMailboxMessagesCmd(mailboxID int64) tea.Cmd {
	database := m.db
	return func() tea.Msg {
		msgs, err := database.ListMessages(mailboxID)
		if err != nil {
			return MessagesLoadedMsg{MailboxID: mailboxID, Err: err}
		}
		return MessagesLoadedMsg{MailboxID: mailboxID, Messages: msgs}
	}
}

func (m *Model) loadUnifiedInboxCmd() tea.Cmd {
	database := m.db
	unreadOnly := m.showUnreadOnly
	return func() tea.Msg {
		msgs, err := database.ListUnifiedInbox(unreadOnly)
		if err != nil {
			return MessagesLoadedMsg{Err: err}
		}
		return MessagesLoadedMsg{MailboxID: 0, Messages: msgs}
	}
}

func (m *Model) loadAddressBookCmd() tea.Cmd {
	database := m.db
	return func() tea.Msg {
		addrs, err := database.ListAddresses()
		return AddressBookLoadedMsg{Addresses: addrs, Err: err}
	}
}

func (m *Model) notifyCmd(mailboxID int64, newCount int) tea.Cmd {
	database := m.db
	return func() tea.Msg {
		mailbox, err := database.GetMailbox(mailboxID)
		if err != nil {
			return nil
		}
		acc, err := database.GetAccount(mailbox.AccountID)
		if err != nil {
			return nil
		}
		// notify-send is fire-and-forget; exec the non-interactive version.
		title := acc.Name
		body := fmt.Sprintf("%s — %d new", mailbox.DisplayName, newCount)
		_ = exec.Command("notify-send", "-a", "tidemail", "-i", "mail-unread", title, body).Run()
		return nil
	}
}

func (m *Model) scheduleNextSync(accountID int64) tea.Cmd {
	for _, acc := range m.accounts {
		if acc.ID != accountID {
			continue
		}
		for _, acfg := range m.cfg.Accounts {
			if acfg.Name != acc.Name {
				continue
			}
			if acfg.SyncMinutes <= 0 {
				return nil
			}
			return tea.Every(time.Duration(acfg.SyncMinutes)*time.Minute, func(t time.Time) tea.Msg {
				return AutoSyncMsg{AccountID: accountID}
			})
		}
	}
	return nil
}

func (m *Model) startSyncTimers() tea.Cmd {
	var cmds []tea.Cmd
	for _, acc := range m.accounts {
		if cmd := m.scheduleNextSync(acc.ID); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func (m *Model) syncMailboxCmd(mailboxID int64, manual bool) tea.Cmd {
	m.syncing[mailboxID] = true
	database := m.db
	mailbox, _ := database.GetMailbox(mailboxID)
	acc, _ := database.GetAccount(mailbox.AccountID)
	var acfg config.AccountConfig
	for _, a := range m.cfg.Accounts {
		if a.Name == acc.Name {
			acfg = a
			break
		}
	}
	return func() tea.Msg {
		t0 := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		client := imapClient.New(acfg)
		connectStart := time.Now()
		if err := client.Connect(ctx); err != nil {
			logFetch(acc.Name, mailbox.Name, 0, time.Since(connectStart), 0, time.Since(t0), err)
			return MailboxSyncedMsg{MailboxID: mailboxID, Err: err, Manual: manual, Total: time.Since(t0)}
		}
		defer client.Close()
		connectDur := time.Since(connectStart)
		since := mailbox.LastSynced
		if existing, countErr := database.CountMessages(mailboxID); countErr == nil && existing == 0 {
			since = time.Time{}
		}
		fetchStart := time.Now()
		msgs, err := client.FetchSince(ctx, mailbox.Name, since)
		fetchDur := time.Since(fetchStart)
		if err != nil {
			logFetch(acc.Name, mailbox.Name, 0, connectDur, fetchDur, time.Since(t0), err)
			return MailboxSyncedMsg{MailboxID: mailboxID, Err: err, Manual: manual, Total: time.Since(t0)}
		}
		newCount, err := storeFetchedMessages(database, mailboxID, msgs)
		if err != nil {
			logFetch(acc.Name, mailbox.Name, len(msgs), connectDur, fetchDur, time.Since(t0), err)
			return MailboxSyncedMsg{MailboxID: mailboxID, Err: err, Manual: manual, Total: time.Since(t0)}
		}
		unread, _ := database.CountUnread(mailboxID)
		database.SetMailboxLastSynced(mailboxID, time.Now()) //nolint:errcheck
		database.SetMailboxUnreadCount(mailboxID, unread)    //nolint:errcheck
		logFetch(acc.Name, mailbox.Name, len(msgs), connectDur, fetchDur, time.Since(t0), nil)
		return MailboxSyncedMsg{MailboxID: mailboxID, NewCount: newCount, Manual: manual, Total: time.Since(t0)}
	}
}

func storeFetchedMessages(database *db.DB, mailboxID int64, msgs []db.Message) (int, error) {
	newCount := 0
	for _, msg := range msgs {
		msg.MailboxID = mailboxID
		if err := database.UpsertMessage(msg); err != nil {
			return newCount, err
		}
		if !msg.Read {
			newCount++
		}
	}
	return newCount, nil
}

func logFetch(account, mailbox string, msgCount int, connectDur, fetchDur, totalDur time.Duration, err error) {
	logPath, pathErr := config.LogPath()
	if pathErr != nil {
		return
	}
	f, openErr := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if openErr != nil {
		return
	}
	defer f.Close()
	logger := log.New(f, "", log.LstdFlags)
	if err != nil {
		logger.Printf("fetch\taccount=%s mailbox=%s connect=%v fetch=%v total=%v error=%q",
			account, mailbox, connectDur.Round(time.Millisecond), fetchDur.Round(time.Millisecond), totalDur.Round(time.Millisecond), err)
	} else {
		logger.Printf("fetch\taccount=%s mailbox=%s msgs=%d connect=%v fetch=%v total=%v",
			account, mailbox, msgCount, connectDur.Round(time.Millisecond), fetchDur.Round(time.Millisecond), totalDur.Round(time.Millisecond))
	}
}
