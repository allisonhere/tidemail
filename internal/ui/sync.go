package ui

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/mail"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	imapClient "github.com/allisonhere/tidemail/internal/imap"
	tea "github.com/charmbracelet/bubbletea"
)

// errNoAccountConfig reports an account row with no reachable [[account]] block
// in config.toml. Every sync path must stop on it. Syncing anyway used to mean
// dialing a zero-value AccountConfig — empty host, port 0 — which surfaced to
// users as the baffling "dial tcp :0: connect: connection refused".
var errNoAccountConfig = errors.New("no account settings found for this account — open Accounts and re-enter its server details")

// accountConfigFor resolves the config block that owns an account row, joining
// on the stable config ID. The name fallback covers rows the config_id
// migration left blank, and it only matches when exactly one config block
// claims that name — an ambiguous name is an unresolved account, not a guess.
func accountConfigFor(configs []config.AccountConfig, acc db.Account) (config.AccountConfig, error) {
	if acc.ConfigID != "" {
		for _, acfg := range configs {
			if acfg.ID == acc.ConfigID {
				return acfg, nil
			}
		}
		return config.AccountConfig{}, fmt.Errorf("%w (%q)", errNoAccountConfig, acc.Name)
	}
	var found config.AccountConfig
	matches := 0
	for _, acfg := range configs {
		if acfg.Name == acc.Name {
			found = acfg
			matches++
		}
	}
	if matches != 1 {
		return config.AccountConfig{}, fmt.Errorf("%w (%q)", errNoAccountConfig, acc.Name)
	}
	return found, nil
}

// sortAccountsByConfigOrder re-derives account row order from config.toml,
// which is the single source of truth for the order accounts appear in. The
// database's own position column is only ever an insertion-order artifact, so
// leaning on it let the sidebar disagree with the order compose and the
// account manager used. Rows with no reachable config block keep their
// relative order, at the end.
func sortAccountsByConfigOrder(accounts []db.Account, configs []config.AccountConfig) []db.Account {
	if len(accounts) < 2 || len(configs) == 0 {
		return accounts
	}
	rank := make(map[int64]int, len(accounts))
	for i, acc := range accounts {
		acfg, err := accountConfigFor(configs, acc)
		if err != nil {
			rank[acc.ID] = len(configs) + i
			continue
		}
		rank[acc.ID] = len(configs)
		for j, c := range configs {
			if c.ID == acfg.ID {
				rank[acc.ID] = j
				break
			}
		}
	}
	sorted := append([]db.Account(nil), accounts...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return rank[sorted[i].ID] < rank[sorted[j].ID]
	})
	return sorted
}

// accountConfigFor is the Model-scoped form of the resolver above.
func (m Model) accountConfigFor(acc db.Account) (config.AccountConfig, error) {
	return accountConfigFor(m.cfg.Accounts, acc)
}

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
	// Adopt rows written before config_id existed, so they are not re-imported
	// as duplicates below.
	links := make([]db.AccountLink, 0, len(configs))
	draftLinks := make([]db.DraftAccountLink, 0, len(configs))
	for _, accountCfg := range configs {
		links = append(links, db.AccountLink{ConfigID: accountCfg.ID, Name: accountCfg.Name})
		draftLinks = append(draftLinks, db.DraftAccountLink{
			ConfigID: accountCfg.ID, Name: accountCfg.Name, User: accountCfg.User,
		})
	}
	if err := database.MigrateAccountConfigIDs(links); err != nil {
		return accounts, fmt.Errorf("link configured accounts: %w", err)
	}
	if err := database.MigrateDraftAccountConfigIDs(draftLinks); err != nil {
		return accounts, fmt.Errorf("link drafts and outbox to accounts: %w", err)
	}
	if len(links) > 0 {
		refreshed, err := database.ListAccounts()
		if err != nil {
			return accounts, err
		}
		accounts = refreshed
	}
	existing := make(map[string]db.Account, len(accounts))
	claimed := make(map[string]bool, len(configs))
	for _, accountCfg := range configs {
		if accountCfg.ID != "" {
			claimed[accountCfg.ID] = true
		}
	}
	// Rows whose config block is gone. A version of TideMail older than stable
	// account IDs does not know the `id` field, so saving config.toml from one —
	// an instance left running across an upgrade, or a downgrade — drops every
	// id. The next launch then stamps a fresh set, matches nothing here, and
	// imports the whole account list again. Adopting the stale row instead keeps
	// that from multiplying the sidebar on every restart, and keeps the cached
	// mail attached to the account it belongs to.
	orphansByName := make(map[string][]db.Account)
	for _, account := range accounts {
		if account.ConfigID != "" {
			existing[account.ConfigID] = account
			if !claimed[account.ConfigID] {
				name := strings.TrimSpace(account.Name)
				orphansByName[name] = append(orphansByName[name], account)
			}
		}
	}
	// Oldest first: the earliest row is the one that has been syncing longest
	// and holds the real cache, so it is the one worth reconnecting.
	for name := range orphansByName {
		sort.Slice(orphansByName[name], func(i, j int) bool {
			return orphansByName[name][i].ID < orphansByName[name][j].ID
		})
	}
	adoptOrphan := func(name string) (db.Account, bool) {
		queue := orphansByName[name]
		if len(queue) == 0 {
			return db.Account{}, false
		}
		// Pop it, so two config accounts sharing a display name cannot both
		// claim the same row.
		orphansByName[name] = queue[1:]
		return queue[0], true
	}
	changed := false
	for _, accountCfg := range configs {
		name := strings.TrimSpace(accountCfg.Name)
		if name == "" || accountCfg.ID == "" {
			continue
		}
		account, ok := existing[accountCfg.ID]
		if !ok {
			if orphan, found := adoptOrphan(name); found {
				if err := database.UpdateAccount(orphan.ID, accountCfg.ID, name, orphan.Color); err != nil {
					return accounts, fmt.Errorf("adopt existing account %s: %w", name, err)
				}
				account = db.Account{ID: orphan.ID, ConfigID: accountCfg.ID, Name: name, Position: orphan.Position, Color: orphan.Color}
			} else {
				accountID, err := database.AddAccount(accountCfg.ID, name, "")
				if err != nil {
					return accounts, fmt.Errorf("import configured account %s: %w", name, err)
				}
				account = db.Account{ID: accountID, ConfigID: accountCfg.ID, Name: name}
			}
			existing[accountCfg.ID] = account
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
	unreadFirst := m.cfg.Display.UnreadFirst
	return func() tea.Msg {
		var (
			msgs []db.Message
			err  error
		)
		if unreadFirst {
			msgs, err = database.ListMessagesUnreadFirst(mailboxID)
		} else {
			msgs, err = database.ListMessages(mailboxID)
		}
		if err != nil {
			return MessagesLoadedMsg{MailboxID: mailboxID, Err: err}
		}
		return MessagesLoadedMsg{MailboxID: mailboxID, Messages: msgs}
	}
}

func (m *Model) loadUnifiedInboxCmd() tea.Cmd {
	database := m.db
	unreadOnly := m.showUnreadOnly
	unreadFirst := m.cfg.Display.UnreadFirst
	return func() tea.Msg {
		var (
			msgs []db.Message
			err  error
		)
		if unreadFirst {
			msgs, err = database.ListUnifiedInboxUnreadFirst(unreadOnly)
		} else {
			msgs, err = database.ListUnifiedInbox(unreadOnly)
		}
		if err != nil {
			return MessagesLoadedMsg{Err: err}
		}
		return MessagesLoadedMsg{MailboxID: 0, Messages: msgs}
	}
}

func (m *Model) searchAllMessagesCmd(query string) tea.Cmd {
	database := m.db
	unreadFirst := m.cfg.Display.UnreadFirst
	query = strings.TrimSpace(query)
	return func() tea.Msg {
		msgs, err := database.SearchAllMessages(query, unreadFirst)
		if err != nil {
			return MessagesLoadedMsg{Search: true, Query: query, Err: err}
		}
		return MessagesLoadedMsg{Search: true, Query: query, Messages: msgs}
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

// syncPollInterval maps sync_minutes to a polling cadence, following its
// three-way meaning (see config.AccountConfig):
//
//	< 0  manual only — ok is false, no timer at all
//	  0  push — IDLE drives refreshes, so this is only the safety net
//	> 0  poll every N minutes (IDLE additionally accelerates it)
//
// Split out from scheduleNextSync so the mapping is testable without waiting on
// a live tea.Every timer.
func syncPollInterval(syncMinutes int) (time.Duration, bool) {
	switch {
	case syncMinutes < 0:
		return 0, false
	case syncMinutes == 0:
		return pushSafetyPollInterval, true
	default:
		return time.Duration(syncMinutes) * time.Minute, true
	}
}

// scheduleNextSync arms the next background refresh for an account.
func (m *Model) scheduleNextSync(accountID int64) tea.Cmd {
	for _, acc := range m.accounts {
		if acc.ID != accountID {
			continue
		}
		acfg, err := m.accountConfigFor(acc)
		if err != nil {
			return nil
		}
		interval, ok := syncPollInterval(acfg.SyncMinutes)
		if !ok {
			return nil
		}
		return tea.Every(interval, func(t time.Time) tea.Msg {
			return AutoSyncMsg{AccountID: accountID}
		})
	}
	return nil
}

// pushSafetyPollInterval is how often a push-only account (sync_minutes = 0)
// polls anyway. IDLE is silent across a connection that has wedged without
// dropping — the server never announces anything and the watcher never sees an
// error to reconnect on — so push with no fallback can stall indefinitely with
// nothing to show the user. This poll bounds that staleness without
// reintroducing the per-minute chatter push exists to avoid.
const pushSafetyPollInterval = 30 * time.Minute

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
	acfg, err := m.accountConfigFor(acc)
	if err != nil {
		return func() tea.Msg {
			return MailboxesRefreshedMsg{AccountID: accountID, Err: err}
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

		byName := make(map[string]db.Mailbox, len(existing))
		for _, mb := range existing {
			byName[mb.Name] = mb
		}

		var added []db.Mailbox
		var updated []db.Mailbox
		var removed []int64
		err = sessions.Do(ctx, acfg, func(client *imapClient.Client) error {
			infos, listErr := client.ListMailboxes(ctx)
			if listErr != nil {
				return listErr
			}
			server := make(map[string]bool, len(infos))
			for _, info := range infos {
				server[info.Name] = true
				mb := db.Mailbox{
					AccountID:   accountID,
					Name:        info.Name,
					DisplayName: cleanDisplayName(info.Name),
					Delimiter:   info.Delimiter,
					Flags:       info.Flags,
				}
				// Upsert even when the mailbox is already known: the row may
				// predate special-use flags entirely (accounts imported from
				// the config file get a bare INBOX), and without this refresh
				// \Sent and friends could never be learned.
				id, upsertErr := database.UpsertMailbox(mb)
				if upsertErr != nil {
					continue
				}
				mb.ID = id
				if !known[info.Name] {
					added = append(added, mb)
				} else if prev := byName[info.Name]; !sameMailboxMetadata(prev, mb) {
					mb.ID = prev.ID
					updated = append(updated, mb)
				}
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
		return MailboxesRefreshedMsg{AccountID: accountID, Mailboxes: added, Updated: updated, Removed: removed, Err: err}
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

// backfillPageSize is how many older messages one "load more" step pulls. Sized
// to match the initial sync's window so paging back feels like one more screenful
// of the same, and small enough that the fetch stays interactive.
const backfillPageSize = imapClient.MessagesPerInitialSync

// loadOlderMessagesCmd pages further back into a mailbox's server history,
// starting just below the oldest UID already cached. The initial sync only
// reaches the most recent MessagesPerInitialSync messages, so without this the
// archive simply ends there — and since search only covers what is cached, the
// ceiling silently limits search too.
//
// Shares the m.syncing gate with syncMailboxCmd: both fetch and store into the
// same mailbox, so letting them overlap would reintroduce the write collisions
// the gate exists to prevent.
func (m *Model) loadOlderMessagesCmd(mailboxID int64) tea.Cmd {
	if m.syncing[mailboxID] || m.olderExhausted[mailboxID] {
		return nil
	}
	database := m.db
	mailbox, err := database.GetMailbox(mailboxID)
	if err != nil {
		return func() tea.Msg {
			return OlderMessagesLoadedMsg{MailboxID: mailboxID, Err: fmt.Errorf("load mailbox: %w", err)}
		}
	}
	oldestUID, err := database.OldestMessageUID(mailboxID)
	if err != nil {
		return func() tea.Msg {
			return OlderMessagesLoadedMsg{MailboxID: mailboxID, Err: fmt.Errorf("oldest message: %w", err)}
		}
	}
	if oldestUID <= 1 {
		// Nothing cached yet, or the cache already reaches UID 1 — either way
		// there is no older history to ask for.
		return func() tea.Msg {
			return OlderMessagesLoadedMsg{MailboxID: mailboxID, Exhausted: true}
		}
	}
	acc, err := database.GetAccount(mailbox.AccountID)
	if err != nil {
		return func() tea.Msg {
			return OlderMessagesLoadedMsg{MailboxID: mailboxID, Err: fmt.Errorf("load account: %w", err)}
		}
	}
	acfg, err := m.accountConfigFor(acc)
	if err != nil {
		return func() tea.Msg {
			return OlderMessagesLoadedMsg{MailboxID: mailboxID, Err: err}
		}
	}
	m.syncing[mailboxID] = true
	sessions := m.sessions
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		var result tea.Msg
		err := sessions.Do(ctx, acfg, func(client *imapClient.Client) error {
			msgs, fetchErr := client.FetchOlderThan(ctx, mailbox.Name, oldestUID, backfillPageSize)
			if fetchErr != nil {
				result = OlderMessagesLoadedMsg{MailboxID: mailboxID, Err: fetchErr}
				return nil
			}
			if _, storeErr := storeFetchedMessages(database, mailboxID, msgs); storeErr != nil {
				result = OlderMessagesLoadedMsg{MailboxID: mailboxID, Err: storeErr}
				return nil
			}
			// A short page means the server had less than we asked for, so this
			// was the last one. An empty page means we were already at the end.
			result = OlderMessagesLoadedMsg{
				MailboxID: mailboxID,
				Count:     len(msgs),
				Exhausted: len(msgs) < backfillPageSize,
			}
			return nil
		})
		if err != nil {
			return OlderMessagesLoadedMsg{MailboxID: mailboxID, Err: err}
		}
		return result
	}
}

// Opening a folder should fill it, but syncing on every cursor movement would
// fire a request per row while scrolling the sidebar. The settle command is
// cancellable: superseded commands return nil, so Bubble Tea neither updates nor
// redraws for every folder the cursor passed on the way to its destination.
const (
	folderSettleDelay = 500 * time.Millisecond
	folderStaleAfter  = 15 * time.Minute
	// coldSyncTimeout covers a first-ever fetch of a folder, which downloads a
	// whole page of bodies rather than the handful an incremental sync sees.
	coldSyncTimeout = 5 * time.Minute
)

// sameMailboxMetadata reports whether a server LIST entry tells us anything new
// about a folder we already store.
func sameMailboxMetadata(a, b db.Mailbox) bool {
	if a.Delimiter != b.Delimiter || len(a.Flags) != len(b.Flags) {
		return false
	}
	have := make(map[string]bool, len(a.Flags))
	for _, f := range a.Flags {
		have[strings.ToLower(f)] = true
	}
	for _, f := range b.Flags {
		if !have[strings.ToLower(f)] {
			return false
		}
	}
	return true
}

// scheduleFolderSettle arms a background sync for the highlighted folder.
func (m *Model) scheduleFolderSettle() tea.Cmd {
	m.cancelFolderSettle()
	selected := m.selectedMailbox()
	if selected == nil {
		return nil
	}
	m.folderSettleSeq++
	seq := m.folderSettleSeq
	id := selected.ID
	ctx, cancel := context.WithCancel(context.Background())
	m.folderSettleCancel = cancel
	return func() tea.Msg {
		timer := time.NewTimer(folderSettleDelay)
		defer timer.Stop()
		select {
		case <-timer.C:
			return FolderSettledMsg{Seq: seq, MailboxID: id}
		case <-ctx.Done():
			return nil
		}
	}
}

func (m *Model) cancelFolderSettle() {
	if m.folderSettleCancel != nil {
		m.folderSettleCancel()
		m.folderSettleCancel = nil
	}
	m.folderSettlePending = 0
	m.folderSettleSeq++
}

// accountHasSyncInFlight prevents a passive folder refresh from sitting in the
// per-account SessionPool queue behind known message sync work. exceptMailbox
// allows the caller to ignore its own mailbox ID.
func (m Model) accountHasSyncInFlight(accountID, exceptMailbox int64) bool {
	for mailboxID := range m.syncing {
		if mailboxID == exceptMailbox {
			continue
		}
		if mb := m.mailboxByID(mailboxID); mb != nil && mb.AccountID == accountID {
			return true
		}
	}
	return false
}

// rearmDeferredFolderSettle resumes a passive refresh that deliberately did
// not queue behind another sync for the same account.
func (m *Model) rearmDeferredFolderSettle() tea.Cmd {
	pending := m.folderSettlePending
	if pending == 0 {
		return nil
	}
	selected := m.selectedMailbox()
	if selected == nil || selected.ID != pending {
		m.folderSettlePending = 0
		return nil
	}
	if m.accountHasSyncInFlight(selected.AccountID, 0) || !m.shouldAutoSyncFolder(*selected) {
		return nil
	}
	return m.scheduleFolderSettle()
}

// shouldAutoSyncFolder reports whether resting on this folder should fetch it.
// Inboxes are excluded because the timers and the IDLE watcher already cover
// them, and an account set to manual-only is never touched in the background.
func (m Model) shouldAutoSyncFolder(mb db.Mailbox) bool {
	if isInboxMailbox(mb) {
		return false
	}
	for _, acc := range m.accounts {
		if acc.ID != mb.AccountID {
			continue
		}
		acfg, err := m.accountConfigFor(acc)
		if err != nil {
			return false
		}
		if acfg.SyncMinutes < 0 {
			return false
		}
	}
	// A row that has never synced decodes to the Unix epoch, not the zero
	// time, so IsZero alone would miss it.
	if mb.LastSynced.IsZero() || mb.LastSynced.Unix() <= 0 {
		return true
	}
	return time.Since(mb.LastSynced) >= folderStaleAfter
}

func (m *Model) syncMailboxCmd(mailboxID int64, manual bool) tea.Cmd {
	return m.syncMailboxCmdWithMode(mailboxID, manual, false)
}

func (m *Model) syncPassiveFolderCmd(mailboxID int64) tea.Cmd {
	return m.syncMailboxCmdWithMode(mailboxID, false, true)
}

func (m *Model) syncMailboxCmdWithMode(mailboxID int64, manual, passive bool) tea.Cmd {
	// One sync per mailbox at a time. Launch timers, the startup sweep, and IDLE
	// nudges all target the inbox and can otherwise stack up on it, which costs a
	// redundant full fetch and — because the concurrent writers collide — throws
	// SQLITE_BUSY out of storeFetchedMessages. That aborts one sync partway
	// through storing while its sibling still advances last_synced, so the
	// dropped messages fall outside the next sync window. Returning nil is safe
	// at every call site: tea.Batch discards nil commands.
	if m.syncing[mailboxID] {
		if manual {
			// Manual syncs get feedback — a keypress that silently does nothing
			// reads as the app being broken. Auto and IDLE syncs stay quiet.
			m.setStatus("sync already in progress", false)
			return m.clearStatusCmd()
		}
		return nil
	}
	m.syncing[mailboxID] = true
	// Only a sync the user explicitly asked for animates. Timers, IDLE nudges,
	// the startup inbox sweep, and passive folder refreshes all run quietly so a
	// background fetch never lights up the status line (or keeps the frame clock
	// running) while the user is trying to read.
	if manual {
		if m.syncVisible == nil {
			m.syncVisible = make(map[int64]bool)
		}
		m.syncVisible[mailboxID] = true
	}
	database := m.db
	mailbox, err := database.GetMailbox(mailboxID)
	if err != nil {
		return func() tea.Msg {
			return MailboxSyncedMsg{MailboxID: mailboxID, Err: fmt.Errorf("load mailbox: %w", err), Manual: manual, Passive: passive}
		}
	}
	acc, err := database.GetAccount(mailbox.AccountID)
	if err != nil {
		return func() tea.Msg {
			return MailboxSyncedMsg{MailboxID: mailboxID, Err: fmt.Errorf("load account: %w", err), Manual: manual, Passive: passive}
		}
	}
	acfg, err := m.accountConfigFor(acc)
	if err != nil {
		return func() tea.Msg {
			return MailboxSyncedMsg{MailboxID: mailboxID, Err: err, Manual: manual, Passive: passive}
		}
	}
	sessions := m.sessions
	// A folder that has never synced pulls a full page of message bodies, which
	// is a different order of work from an incremental fetch — Gmail's Sent Mail
	// needs well over a minute. Give the cold case room rather than tearing the
	// connection down mid-response.
	timeout := 60 * time.Second
	if mailbox.LastSynced.IsZero() || mailbox.LastSynced.Unix() <= 0 {
		timeout = coldSyncTimeout
	}
	fetch := func() tea.Msg {
		t0 := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
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

			// A cold mailbox has no floor to fetch from, so take the most
			// recent page instead of everything since the epoch.
			cold := false
			if existing, countErr := database.CountMessages(mailboxID); countErr == nil && existing == 0 {
				since = time.Time{}
				cold = true
			}
			// Keep the inbox's proven first page, but ask for less from other
			// folders: they are fetched whole-body and Sent in particular can
			// be heavy enough that the server drops the connection.
			limit := imapClient.MessagesPerInitialSync
			if cold && !isInboxMailbox(mailbox) {
				limit = imapClient.MessagesPerFolderFirstSync
			}
			fetchStart := time.Now()
			msgs, err := client.FetchSinceLimit(ctx, mailbox.Name, since, limit)
			fetchDur := time.Since(fetchStart)
			if err != nil {
				logFetch(acc.Name, mailbox.Name, 0, connectDur, fetchDur, time.Since(t0), err)
				result = MailboxSyncedMsg{MailboxID: mailboxID, Err: err, Manual: manual, Passive: passive, Total: time.Since(t0)}
				return nil
			}
			newMsgs, err := storeFetchedMessages(database, mailboxID, msgs)
			if err != nil {
				logFetch(acc.Name, mailbox.Name, len(msgs), connectDur, fetchDur, time.Since(t0), err)
				result = MailboxSyncedMsg{MailboxID: mailboxID, Err: err, Manual: manual, Passive: passive, Total: time.Since(t0)}
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
			syncedAt := time.Now()
			if e := database.SetMailboxLastSynced(mailboxID, syncedAt); e != nil {
				writeErr = e
			}
			if e := database.SetMailboxUnreadCount(mailboxID, unread); e != nil {
				writeErr = e
			}
			logFetch(acc.Name, mailbox.Name, len(msgs), connectDur, fetchDur, time.Since(t0), writeErr)
			result = MailboxSyncedMsg{MailboxID: mailboxID, NewCount: len(newMsgs), NewMessages: newMsgs, Manual: manual, Passive: passive, SyncedAt: syncedAt, Total: time.Since(t0)}
			return nil
		})
		if err != nil {
			logFetch(acc.Name, mailbox.Name, 0, time.Since(connectStart), 0, time.Since(t0), err)
			return MailboxSyncedMsg{MailboxID: mailboxID, Err: err, Manual: manual, Passive: passive, Total: time.Since(t0)}
		}
		return result
	}
	// Start the spinner animation while this sync runs (no-op if already running).
	// Quiet syncs skip it entirely; the loop is gated on syncVisible anyway.
	if !manual {
		return fetch
	}
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
