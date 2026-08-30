package ui

import (
	"context"
	"fmt"
	"log"
	"net/mail"
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
	service := m.appService
	return func() tea.Msg {
		state, err := service.Bootstrap()
		if err != nil {
			return AccountsLoadedMsg{Err: err}
		}
		return AccountsLoadedMsg{Accounts: state.Accounts, Mailboxes: state.Mailboxes}
	}
}

func (m *Model) loadMailboxMessagesCmd(mailboxID int64) tea.Cmd {
	service := m.appService
	return func() tea.Msg {
		page, err := service.ListMessages(mailboxID, false, false)
		if err != nil {
			return MessagesLoadedMsg{MailboxID: mailboxID, Err: err}
		}
		return MessagesLoadedMsg{MailboxID: mailboxID, Messages: page.Messages}
	}
}

func (m *Model) loadUnifiedInboxCmd() tea.Cmd {
	service := m.appService
	unreadOnly := m.showUnreadOnly
	return func() tea.Msg {
		page, err := service.ListMessages(0, true, unreadOnly)
		if err != nil {
			return MessagesLoadedMsg{Err: err}
		}
		return MessagesLoadedMsg{MailboxID: 0, Messages: page.Messages}
	}
}

func (m *Model) searchAllMessagesCmd(query string) tea.Cmd {
	service := m.appService
	query = strings.TrimSpace(query)
	return func() tea.Msg {
		page, err := service.Search(query)
		if err != nil {
			return MessagesLoadedMsg{Search: true, Query: query, Err: err}
		}
		return MessagesLoadedMsg{Search: true, Query: query, Messages: page.Messages}
	}
}

func (m *Model) visibleMessagesCmd() tea.Cmd {
	if m.selectedUnifiedInbox() {
		return m.loadUnifiedInboxCmd()
	}
	if selected := m.selectedMailbox(); selected != nil {
		cmd := m.loadMailboxMessagesCmd(selected.ID)
		if m.selectedDraftsMailbox() {
			cmd = tea.Batch(cmd, m.loadDraftsCmd(selected.ID))
		}
		return cmd
	}
	return nil
}

func (m *Model) loadAddressBookCmd() tea.Cmd {
	database := m.db
	return func() tea.Msg {
		addrs, err := database.AutocompleteAddresses()
		return AddressBookLoadedMsg{Addresses: addrs, Err: err}
	}
}

func (m *Model) notifyCmd(mailboxID int64, newMsgs []db.Message) tea.Cmd {
	database := m.db
	return func() tea.Msg {
		// Account name gives multi-account context; best-effort, not required.
		var acctName string
		if mailbox, err := database.GetMailbox(mailboxID); err == nil {
			if acc, err := database.GetAccount(mailbox.AccountID); err == nil {
				acctName = acc.Name
			}
		}
		title, body := composeNotification(acctName, newMsgs)
		// notify-send is fire-and-forget; exec the non-interactive version.
		// "--" terminates option parsing so a sender/subject beginning with "-"
		// (attacker-controlled) can't be misread as a notify-send flag.
		_ = exec.Command("notify-send", "-a", "tidemail", "-i", "mail-unread", "--", title, body).Run()
		return nil
	}
}

// composeNotification builds the title/body for a new-mail desktop notification:
// one message shows "sender" / "subject"; a batch shows "N new messages" with a
// capped "sender — subject" list. All dynamic text is single-lined and markup-escaped.
func composeNotification(acctName string, msgs []db.Message) (title, body string) {
	const maxLines = 5
	suffix := ""
	if acctName != "" {
		suffix = " · " + notifyEscape(acctName)
	}
	if len(msgs) == 1 {
		return notifyEscape(senderDisplay(msgs[0].From)) + suffix, notifyEscape(subjectOrDefault(msgs[0].Subject))
	}
	title = fmt.Sprintf("%d new messages", len(msgs)) + suffix
	lines := make([]string, 0, maxLines+1)
	for i, msg := range msgs {
		if i >= maxLines {
			lines = append(lines, fmt.Sprintf("…and %d more", len(msgs)-maxLines))
			break
		}
		lines = append(lines, notifyEscape(senderDisplay(msg.From)+" — "+subjectOrDefault(msg.Subject)))
	}
	return title, strings.Join(lines, "\n")
}

// senderDisplay prefers the From header's display name, falling back to the bare
// address (or the raw header if it doesn't parse). Newlines are collapsed so the
// value never spans notification rows.
func senderDisplay(from string) string {
	from = strings.TrimSpace(from)
	out := from
	if addr, err := mail.ParseAddress(from); err == nil {
		if name := strings.TrimSpace(addr.Name); name != "" {
			out = name
		} else {
			out = addr.Address
		}
	}
	return strings.Join(strings.Fields(out), " ")
}

func subjectOrDefault(subject string) string {
	if s := strings.Join(strings.Fields(subject), " "); s != "" {
		return s
	}
	return "(no subject)"
}

// notifyEscape escapes the GLib/Pango markup chars that notification daemons
// (dunst, GNOME) interpret in body text, so untrusted sender/subject content can't
// break or inject into the notification.
func notifyEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
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
		// Also trigger an immediate sync for every inbox on startup,
		// so the user sees fresh mail without waiting for the first timer tick.
		for _, mb := range m.mailboxes {
			if mb.AccountID == acc.ID && isInboxMailbox(mb) {
				cmds = append(cmds, m.syncMailboxCmd(mb.ID, false))
			}
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// syncInboxesNowCmd kicks off an immediate one-shot sync of every inbox mailbox.
// Auto-sync timers (tea.Every) only fire after the first interval elapses, so
// without this the app would show only cached mail on launch until the first
// timer tick — call this on first load so fresh mail arrives right away.
func (m *Model) syncInboxesNowCmd() tea.Cmd {
	var cmds []tea.Cmd
	for _, mb := range m.mailboxes {
		if isInboxMailbox(mb) {
			cmds = append(cmds, m.syncMailboxCmd(mb.ID, false))
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// folderRefreshInterval throttles how often an account's full folder tree is
// re-enumerated from the server. Message sync may run every minute, but the
// folder set changes rarely, so the LIST is gated to at most once per account
// per this interval no matter how aggressive the message sync cadence is.
const folderRefreshInterval = time.Hour

// refreshMailboxesCmd re-enumerates an account's server folders, upserts any
// that aren't stored yet, and prunes ones that vanished server-side. It rides
// the per-account auto-sync timer (never the per-mailbox message sync), so a
// single LIST covers the whole tree; it reuses the session pool instead of
// dialing fresh; and it treats any failure as non-fatal.
//
// Pruning mirrors the user's intent — a folder deleted in the webmail UI should
// disappear locally too — but is guarded against mass deletion: an empty server
// list is treated as a hiccup, not "all folders deleted", so nothing is pruned,
// and INBOX is never pruned. See prunableMailboxIDs for why this can't lose
// locally-composed drafts.
func (m *Model) refreshMailboxesCmd(accountID int64) tea.Cmd {
	database := m.db
	var acc db.Account
	for _, a := range m.accounts {
		if a.ID == accountID {
			acc = a
			break
		}
	}
	if acc.ID == 0 {
		return nil
	}
	var acfg config.AccountConfig
	for _, a := range m.cfg.Accounts {
		if a.Name == acc.Name {
			acfg = a
			break
		}
	}
	sessions := m.sessions
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		existing, err := database.ListMailboxes(accountID)
		if err != nil {
			return MailboxesRefreshedMsg{AccountID: accountID, Err: err}
		}
		known := make(map[string]bool, len(existing))
		for _, mb := range existing {
			known[mb.Name] = true
		}

		var added []db.Mailbox
		var removed []int64
		err = sessions.Do(ctx, acfg, func(client *imapClient.Client) error {
			infos, listErr := client.ListMailboxes(ctx)
			if listErr != nil {
				return listErr
			}
			server := make(map[string]bool, len(infos))
			for _, info := range infos {
				server[info.Name] = true
				if known[info.Name] {
					continue
				}
				mb := db.Mailbox{
					AccountID:   accountID,
					Name:        info.Name,
					DisplayName: cleanDisplayName(info.Name),
					Delimiter:   info.Delimiter,
					Flags:       info.Flags,
				}
				id, upsertErr := database.UpsertMailbox(mb)
				if upsertErr != nil {
					continue
				}
				mb.ID = id
				added = append(added, mb)
			}

			for _, id := range prunableMailboxIDs(existing, server) {
				// Clear cached messages + FTS first (the FTS mirror has no FK
				// cascade), then drop the now-empty mailbox row.
				if e := database.ResetMailboxCache(id); e != nil {
					continue
				}
				if e := database.DeleteMailbox(id); e != nil {
					continue
				}
				removed = append(removed, id)
			}
			return nil
		})
		return MailboxesRefreshedMsg{AccountID: accountID, Mailboxes: added, Removed: removed, Err: err}
	}
}

// prunableMailboxIDs decides which stored folders should be deleted because they
// no longer exist server-side. server is the set of folder names the LIST
// returned. Safety guards: never prune on an empty server set (a likely
// transient fault — a real account always has INBOX), and never prune INBOX.
//
// Pruning is safe for local data: only cached messages (a mirror of server
// state the server itself just deleted) cascade away with the mailbox row.
// Locally-composed drafts live in the drafts table keyed by account, not by a
// mailbox foreign key, so they survive a folder being pruned.
func prunableMailboxIDs(existing []db.Mailbox, server map[string]bool) []int64 {
	if len(server) == 0 {
		return nil
	}
	var ids []int64
	for _, mb := range existing {
		if server[mb.Name] || isInboxMailbox(mb) {
			continue
		}
		ids = append(ids, mb.ID)
	}
	return ids
}

func (m *Model) syncMailboxCmd(mailboxID int64, manual bool) tea.Cmd {
	m.syncing[mailboxID] = true
	database := m.db
	mailbox, err := database.GetMailbox(mailboxID)
	if err != nil {
		return func() tea.Msg {
			return MailboxSyncedMsg{MailboxID: mailboxID, Err: fmt.Errorf("load mailbox: %w", err), Manual: manual}
		}
	}
	acc, err := database.GetAccount(mailbox.AccountID)
	if err != nil {
		return func() tea.Msg {
			return MailboxSyncedMsg{MailboxID: mailboxID, Err: fmt.Errorf("load account: %w", err), Manual: manual}
		}
	}
	var acfg config.AccountConfig
	for _, a := range m.cfg.Accounts {
		if a.Name == acc.Name {
			acfg = a
			break
		}
	}
	sessions := m.sessions
	fetch := func() tea.Msg {
		t0 := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		// "connect" in the fetch log now covers session acquisition: a queue
		// wait plus either a NOOP revalidation or a fresh dial.
		connectStart := time.Now()
		var result tea.Msg
		err := sessions.Do(ctx, acfg, func(client *imapClient.Client) error {
			connectDur := time.Since(connectStart)
			since := mailbox.LastSynced

			// Snapshot the server's full message state up front. It drives three
			// things the additive SINCE fetch can't: detecting a UIDVALIDITY change
			// (every stored UID is then meaningless), reconciling away messages
			// removed server-side, and adopting read/unread changes made elsewhere.
			// A failure here is non-fatal — sync proceeds without it rather than
			// aborting.
			serverMsgs, uidValidity, uidErr := client.ServerState(ctx, mailbox.Name)
			if uidErr == nil && uidValidity != 0 {
				if stored, e := database.MailboxUIDValidity(mailboxID); e == nil && stored != 0 && stored != uidValidity {
					_ = database.ResetMailboxCache(mailboxID) //nolint:errcheck
					since = time.Time{}
				}
				_ = database.SetMailboxUIDValidity(mailboxID, uidValidity) //nolint:errcheck
			}

			if existing, countErr := database.CountMessages(mailboxID); countErr == nil && existing == 0 {
				since = time.Time{}
			}
			fetchStart := time.Now()
			msgs, err := client.FetchSince(ctx, mailbox.Name, since)
			fetchDur := time.Since(fetchStart)
			if err != nil {
				logFetch(acc.Name, mailbox.Name, 0, connectDur, fetchDur, time.Since(t0), err)
				result = MailboxSyncedMsg{MailboxID: mailboxID, Err: err, Manual: manual, Total: time.Since(t0)}
				return nil
			}
			newMsgs, err := storeFetchedMessages(database, mailboxID, msgs)
			if err != nil {
				logFetch(acc.Name, mailbox.Name, len(msgs), connectDur, fetchDur, time.Since(t0), err)
				result = MailboxSyncedMsg{MailboxID: mailboxID, Err: err, Manual: manual, Total: time.Since(t0)}
				return nil
			}
			if uidErr == nil {
				// Reconcile: drop locally-cached messages no longer on the server.
				// Include the just-fetched UIDs so a message that arrived between
				// the snapshot and the fetch isn't mistaken for a removal.
				reconcileSet := make([]uint32, 0, len(serverMsgs)+len(msgs))
				seenByUID := make(map[uint32]bool, len(serverMsgs))
				flaggedByUID := make(map[uint32]bool, len(serverMsgs))
				for _, sm := range serverMsgs {
					reconcileSet = append(reconcileSet, sm.UID)
					seenByUID[sm.UID] = sm.Seen
					flaggedByUID[sm.UID] = sm.Flagged
				}
				for _, fm := range msgs {
					reconcileSet = append(reconcileSet, fm.UID)
				}
				_, _ = database.ReconcileMailboxUIDs(mailboxID, reconcileSet) //nolint:errcheck
				// Adopt server read/unread state for messages we still hold, before
				// the unread count is recomputed below.
				_, _ = database.ApplyServerReadStates(mailboxID, seenByUID) //nolint:errcheck
				// Adopt stars added or removed in another client. The full state
				// snapshot is needed because SINCE does not revisit older mail.
				_, _ = database.ApplyServerStarredStates(mailboxID, flaggedByUID) //nolint:errcheck
			}
			// Auto-apply saved filter rules to newly-arrived mail while the connection
			// is live. Filter failures must not abort the sync, so they are not fatal.
			// Rules that move/delete/archive/mark-read mail drop it from newMsgs so it
			// is neither counted as new nor notified about.
			if len(newMsgs) > 0 {
				newMsgs, _ = applyRulesOnArrival(ctx, database, client, mailbox, newMsgs)
			}
			unread, _ := database.CountUnread(mailboxID)
			// A failed bookkeeping write (e.g. last-synced) can cause endless re-syncs, so
			// don't drop it silently — fold it into the fetch log.
			var writeErr error
			if e := database.SetMailboxLastSynced(mailboxID, time.Now()); e != nil {
				writeErr = e
			}
			if e := database.SetMailboxUnreadCount(mailboxID, unread); e != nil {
				writeErr = e
			}
			logFetch(acc.Name, mailbox.Name, len(msgs), connectDur, fetchDur, time.Since(t0), writeErr)
			result = MailboxSyncedMsg{MailboxID: mailboxID, NewCount: len(newMsgs), NewMessages: newMsgs, Manual: manual, Total: time.Since(t0)}
			return nil
		})
		if err != nil {
			logFetch(acc.Name, mailbox.Name, 0, time.Since(connectStart), 0, time.Since(t0), err)
			return MailboxSyncedMsg{MailboxID: mailboxID, Err: err, Manual: manual, Total: time.Since(t0)}
		}
		return result
	}
	// Start the spinner animation while this sync runs (no-op if already running).
	return tea.Batch(fetch, m.ensureSpinner())
}

// storeFetchedMessages upserts msgs and returns the genuinely-new unread ones
// (previously-unseen UIDs), used to drive desktop notifications. Callers derive the
// "N new" count from len(newMsgs).
func storeFetchedMessages(database *db.DB, mailboxID int64, msgs []db.Message) ([]db.Message, error) {
	var newMsgs []db.Message
	for _, msg := range msgs {
		msg.MailboxID = mailboxID
		deleted, delErr := database.MessageDeletedLocally(mailboxID, msg.UID, msg.MessageID)
		if delErr != nil {
			return newMsgs, delErr
		}
		if deleted {
			continue
		}
		// Determine novelty before the upsert: SINCE re-fetches mail we already hold,
		// and the upsert would otherwise make every poll look like new arrivals.
		existed, exErr := database.MessageExists(mailboxID, msg.UID)
		if err := database.UpsertMessage(msg); err != nil {
			return newMsgs, err
		}
		if exErr == nil && !existed && !msg.Read {
			newMsgs = append(newMsgs, msg)
		}
	}
	return newMsgs, nil
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
