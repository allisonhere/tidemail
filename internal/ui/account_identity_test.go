package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
)

// These are regression tests for issue #18: multi-account setups where adding
// an account collapsed two config blocks into one, and deleting an account took
// the survivor's credentials with it. The cause was that an account's identity
// was its display name.

// newIdentityModel builds a model over a temp DB with two configured accounts
// and returns it with the config-save seam captured.
func newIdentityModel(t *testing.T, accounts ...config.AccountConfig) (Model, *config.Config, *bool) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	orig := configSave
	saved := &config.Config{}
	called := new(bool)
	configSave = func(c config.Config) error { *saved, *called = c, true; return nil }
	t.Cleanup(func() { configSave = orig })

	cfg := config.DefaultConfig()
	cfg.Accounts = accounts
	m := NewModel(database, cfg, "dev", false)

	loaded := m.loadAccountsCmd()().(AccountsLoadedMsg)
	if loaded.Err != nil {
		t.Fatalf("loadAccountsCmd: %v", loaded.Err)
	}
	next, _ := m.Update(loaded)
	return next.(Model), saved, called
}

func acct(name, host, user, pass string) config.AccountConfig {
	return config.AccountConfig{
		Name: name, User: user, Password: pass,
		IMAPHost: host, IMAPPort: 993, IMAPTLS: true,
		SMTPHost: host, SMTPPort: 587, SMTPTLS: true,
	}
}

func runAccountPersistence(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	next, cmd := m.Update(msg)
	m = next.(Model)
	if cmd == nil {
		t.Fatal("expected account persistence command")
	}
	next, _ = m.Update(cmd())
	return next.(Model)
}

// Adding a second account under a display name the first already uses must not
// overwrite the first account's server settings. This is symptom 1 of #18:
// "adding a second account causes both entries to become the same".
func TestSavingDuplicateDisplayNameKeepsPeerIntact(t *testing.T) {
	m, _, _ := newIdentityModel(t, acct("David Blangstrup", "imap.gigahost.dk", "d@blangstrup.info", "gigapass"))
	original := m.cfg.Accounts[0]

	// A second, genuinely different account that happens to share the name.
	second := acct("David Blangstrup", "imap.gmail.com", "d@gmail.com", "gmailpass")
	second.ID = config.NewAccountID()
	accountID, err := m.db.AddAccount(second.ID, second.Name, "")
	if err != nil {
		t.Fatalf("AddAccount: %v", err)
	}
	account, err := m.db.GetAccount(accountID)
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}

	m = runAccountPersistence(t, m, AccountSavedMsg{Account: account, AccountCfg: second, EditID: account.ID})

	if len(m.cfg.Accounts) != 2 {
		t.Fatalf("expected both accounts in config, got %d: %#v", len(m.cfg.Accounts), m.cfg.Accounts)
	}
	kept, ok := m.cfg.AccountByID(original.ID)
	if !ok {
		t.Fatalf("first account lost its config block: %#v", m.cfg.Accounts)
	}
	if kept.IMAPHost != "imap.gigahost.dk" || kept.User != "d@blangstrup.info" || kept.Password != "gigapass" {
		t.Fatalf("first account was overwritten by its same-named peer: %#v", kept)
	}
}

// Renaming an account must update its own config block, not append a second one
// (which ensureConfiguredAccounts would then re-import as a ghost DB account).
func TestRenamingAccountUpdatesItsOwnConfigBlock(t *testing.T) {
	m, _, _ := newIdentityModel(t,
		acct("Personal", "imap.example.com", "me@example.com", "pw1"),
		acct("Work", "imap.work.example.com", "me@work.example.com", "pw2"),
	)
	if len(m.accounts) != 2 {
		t.Fatalf("expected 2 imported accounts, got %d", len(m.accounts))
	}

	var work db.Account
	for _, a := range m.accounts {
		if a.Name == "Work" {
			work = a
		}
	}
	renamed, ok := m.cfg.AccountByID(work.ConfigID)
	if !ok {
		t.Fatalf("Work has no config block: %#v", m.cfg.Accounts)
	}
	renamed.Name = "Personal" // rename onto the peer's name — the worst case
	if err := m.db.UpdateAccount(work.ID, renamed.ID, renamed.Name, ""); err != nil {
		t.Fatalf("UpdateAccount: %v", err)
	}
	account, err := m.db.GetAccount(work.ID)
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}

	m = runAccountPersistence(t, m, AccountSavedMsg{Account: account, AccountCfg: renamed, EditID: account.ID})

	if len(m.cfg.Accounts) != 2 {
		t.Fatalf("rename changed the account count: %#v", m.cfg.Accounts)
	}
	peer, ok := m.cfg.AccountByID(m.cfg.Accounts[0].ID)
	if !ok || peer.IMAPHost != "imap.example.com" {
		t.Fatalf("the renamed account clobbered its peer: %#v", m.cfg.Accounts)
	}
	got, ok := m.cfg.AccountByID(renamed.ID)
	if !ok || got.Name != "Personal" || got.IMAPHost != "imap.work.example.com" {
		t.Fatalf("rename did not land on the right block: %#v", m.cfg.Accounts)
	}

	// The reload must not resurrect the old name as a ghost account.
	reloaded := m.loadAccountsCmd()().(AccountsLoadedMsg)
	if reloaded.Err != nil {
		t.Fatalf("reload: %v", reloaded.Err)
	}
	if len(reloaded.Accounts) != 2 {
		t.Fatalf("rename created a ghost account: %#v", reloaded.Accounts)
	}
}

// Deleting one of two same-named accounts must leave the other's config block —
// host, user and password — untouched. Symptom 2 of #18.
func TestDeletingSameNamedAccountKeepsPeerCredentials(t *testing.T) {
	m, saved, called := newIdentityModel(t,
		acct("David Blangstrup", "imap.gigahost.dk", "d@blangstrup.info", "gigapass"),
		acct("David Blangstrup", "imap.gmail.com", "d@gmail.com", "gmailpass"),
	)
	if len(m.cfg.Accounts) != 2 {
		t.Fatalf("expected 2 configured accounts, got %d", len(m.cfg.Accounts))
	}
	keep := m.cfg.Accounts[0]
	drop := m.cfg.Accounts[1]

	var dropID int64
	for _, a := range m.accounts {
		if a.ConfigID == drop.ID {
			dropID = a.ID
		}
	}
	if dropID == 0 {
		t.Fatalf("second account never reached the database: %#v", m.accounts)
	}

	delMsg := deleteAccountCmd(dropID, drop.ID, drop.Name)().(AccountDeletedMsg)
	if delMsg.Err != nil {
		t.Fatalf("deleteAccountCmd: %v", delMsg.Err)
	}
	m = runAccountPersistence(t, m, delMsg)

	if !*called {
		t.Fatal("expected the config to be saved after a delete")
	}
	if len(m.cfg.Accounts) != 1 {
		t.Fatalf("delete removed the peer too: %#v", m.cfg.Accounts)
	}
	survivor := m.cfg.Accounts[0]
	if survivor.ID != keep.ID || survivor.IMAPHost != "imap.gigahost.dk" ||
		survivor.User != "d@blangstrup.info" || survivor.Password != "gigapass" {
		t.Fatalf("the surviving account lost its settings: %#v", survivor)
	}
	if len(saved.Accounts) != 1 || saved.Accounts[0].ID != keep.ID {
		t.Fatalf("saved config is wrong: %#v", saved.Accounts)
	}
}

// An account row with no reachable config block must produce an error, not a
// connection attempt against an empty host. This is what surfaced in the issue's
// fetch log as "dial tcp :0: connect: connection refused".
func TestSyncWithoutConfigBlockErrorsInsteadOfDialingNothing(t *testing.T) {
	m, _, _ := newIdentityModel(t, acct("Personal", "imap.example.com", "me@example.com", "pw"))

	// An orphan: a row in the database that no [[account]] block owns.
	orphanID, err := m.db.AddAccount(config.NewAccountID(), "Orphan", "")
	if err != nil {
		t.Fatalf("AddAccount: %v", err)
	}
	mailboxID, err := m.db.UpsertMailbox(db.Mailbox{AccountID: orphanID, Name: "INBOX", DisplayName: "Inbox"})
	if err != nil {
		t.Fatalf("UpsertMailbox: %v", err)
	}
	loaded := m.loadAccountsCmd()().(AccountsLoadedMsg)
	next, _ := m.Update(loaded)
	m = next.(Model)

	cmd := m.syncMailboxCmd(mailboxID, true)
	if cmd == nil {
		t.Fatal("expected syncMailboxCmd to report the problem, got no command")
	}
	msg, ok := cmd().(MailboxSyncedMsg)
	if !ok {
		t.Fatalf("expected MailboxSyncedMsg, got %T", cmd())
	}
	if msg.Err == nil {
		t.Fatal("expected an error for an account with no config block")
	}
	if !errors.Is(msg.Err, errNoAccountConfig) {
		t.Fatalf("expected errNoAccountConfig, got %v", msg.Err)
	}
	if strings.Contains(msg.Err.Error(), ":0") {
		t.Fatalf("still dialing an empty host: %v", msg.Err)
	}
}

// The upgrade path: a database written by a build that keyed accounts by name
// must be adopted, not re-imported. A ghost account here is what produced the
// issue's half-initialized second account with a starter INBOX.
func TestUpgradeAdoptsLegacyAccountRows(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	// Two accounts as an older build left them: rows with no config link.
	for _, name := range []string{"David Blangstrup", "Gmail"} {
		accountID, err := database.AddAccount("", name, "")
		if err != nil {
			t.Fatalf("AddAccount: %v", err)
		}
		if _, err := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "INBOX", DisplayName: "Inbox"}); err != nil {
			t.Fatalf("UpsertMailbox: %v", err)
		}
	}

	orig := configSave
	configSave = func(config.Config) error { return nil }
	defer func() { configSave = orig }()

	cfg := config.DefaultConfig()
	cfg.Accounts = []config.AccountConfig{
		acct("David Blangstrup", "imap.gigahost.dk", "d@blangstrup.info", "gigapass"),
		acct("Gmail", "imap.gmail.com", "d@gmail.com", "gmailpass"),
	}
	m := NewModel(database, cfg, "dev", false)

	loaded := m.loadAccountsCmd()().(AccountsLoadedMsg)
	if loaded.Err != nil {
		t.Fatalf("loadAccountsCmd: %v", loaded.Err)
	}
	if len(loaded.Accounts) != 2 {
		t.Fatalf("expected the two existing rows to be adopted, got %d: %#v", len(loaded.Accounts), loaded.Accounts)
	}
	for _, a := range loaded.Accounts {
		if a.ConfigID == "" {
			t.Fatalf("account %q was not linked to its config block", a.Name)
		}
		if _, err := accountConfigFor(m.cfg.Accounts, a); err != nil {
			t.Fatalf("account %q cannot resolve its settings after upgrade: %v", a.Name, err)
		}
	}
	// One INBOX each — no starter mailbox created for a ghost.
	if len(loaded.Mailboxes) != 2 {
		t.Fatalf("expected 2 mailboxes, got %d: %#v", len(loaded.Mailboxes), loaded.Mailboxes)
	}
}

func TestAccountSaveConfigFailureDoesNotMutateDatabase(t *testing.T) {
	m, _, _ := newIdentityModel(t, acct("Personal", "imap.old.example", "me@example.com", "pw"))
	before := m.accounts[0]
	updated, ok := m.cfg.AccountByID(before.ConfigID)
	if !ok {
		t.Fatal("missing account config")
	}
	updated.Name = "Renamed"

	orig := configSave
	configSave = func(config.Config) error { return errors.New("disk full") }
	defer func() { configSave = orig }()
	next, _ := m.Update(AccountSavedMsg{AccountCfg: updated, EditID: before.ID})
	m = next.(Model)
	after, err := m.db.GetAccount(before.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Name != before.Name || after.ConfigID != before.ConfigID {
		t.Fatalf("database changed despite config failure: before=%#v after=%#v", before, after)
	}
}

func TestAccountSaveDatabaseWriteRunsInCommand(t *testing.T) {
	m, saved, _ := newIdentityModel(t, acct("Personal", "imap.example.com", "me@example.com", "password"))
	before, err := m.db.ListAccounts()
	if err != nil {
		t.Fatal(err)
	}
	added := acct("Work", "imap.work.example.com", "me@work.example.com", "work-password")
	added.ID = config.NewAccountID()

	next, cmd := m.Update(AccountSavedMsg{AccountCfg: added})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("expected asynchronous database save")
	}
	during, err := m.db.ListAccounts()
	if err != nil {
		t.Fatal(err)
	}
	if len(during) != len(before) {
		t.Fatalf("database changed on the UI update path: before=%d during=%d", len(before), len(during))
	}
	if len(m.cfg.Accounts) != 1 {
		t.Fatalf("in-memory config changed before database completion: %#v", m.cfg.Accounts)
	}
	if len(saved.Accounts) != 2 {
		t.Fatalf("candidate config was not persisted before database command: %#v", saved.Accounts)
	}

	next, _ = m.Update(cmd())
	m = next.(Model)
	after, err := m.db.ListAccounts()
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before)+1 || len(m.cfg.Accounts) != 2 {
		t.Fatalf("database completion did not publish the account: db=%#v config=%#v", after, m.cfg.Accounts)
	}
}

func TestAccountSaveDatabaseFailureRollsBackConfig(t *testing.T) {
	m, saved, _ := newIdentityModel(t, acct("Personal", "imap.example.com", "me@example.com", "password"))
	added := acct("Work", "imap.work.example.com", "me@work.example.com", "work-password")
	added.ID = config.NewAccountID()

	next, cmd := m.Update(AccountSavedMsg{AccountCfg: added})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("expected asynchronous database save")
	}
	if err := m.db.Close(); err != nil {
		t.Fatal(err)
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	if len(m.cfg.Accounts) != 1 || len(saved.Accounts) != 1 {
		t.Fatalf("failed database save did not restore config: memory=%#v saved=%#v", m.cfg.Accounts, saved.Accounts)
	}
	if !m.statusErr || !strings.Contains(m.statusMsg, "save account database") {
		t.Fatalf("database failure was not surfaced: %q", m.statusMsg)
	}
}

func TestReplyOnOrphanedAccountReportsError(t *testing.T) {
	m, _, _ := newIdentityModel(t, acct("Personal", "imap.example.com", "me@example.com", "password"))
	orphanID, err := m.db.AddAccount(config.NewAccountID(), "Orphan", "")
	if err != nil {
		t.Fatal(err)
	}
	mailboxID, err := m.db.UpsertMailbox(db.Mailbox{AccountID: orphanID, Name: "INBOX"})
	if err != nil {
		t.Fatal(err)
	}
	m.accounts, _ = m.db.ListAccounts()
	m.mailboxes, _ = m.db.ListMailboxes(orphanID)
	message := db.Message{ID: 1, MailboxID: mailboxID, Subject: "Orphaned"}
	m.messages = []db.Message{message}
	m.filteredMessages = []db.Message{message}
	m.focused = paneMessages

	next, _ := m.executeCommand("reply")
	m = next.(Model)
	if m.overlay == overlayCompose {
		t.Fatal("orphaned reply opened compose")
	}
	if !m.statusErr || !strings.Contains(m.statusMsg, errNoAccountConfig.Error()) {
		t.Fatalf("orphaned reply did not explain the problem: %q", m.statusMsg)
	}
}

func TestAccountDeleteConfigFailurePreservesDatabase(t *testing.T) {
	m, _, _ := newIdentityModel(t, acct("Personal", "imap.example.com", "me@example.com", "pw"))
	account := m.accounts[0]
	orig := configSave
	configSave = func(config.Config) error { return errors.New("disk full") }
	defer func() { configSave = orig }()

	next, _ := m.Update(AccountDeletedMsg{AccountID: account.ID, ConfigID: account.ConfigID, AccountName: account.Name})
	m = next.(Model)
	if _, err := m.db.GetAccount(account.ID); err != nil {
		t.Fatalf("database account deleted despite config failure: %v", err)
	}
	if _, ok := m.cfg.AccountByID(account.ConfigID); !ok {
		t.Fatal("in-memory account config removed despite config failure")
	}
}

func TestAccountCfgForMailboxDoesNotFallBackToPeer(t *testing.T) {
	m, _, _ := newIdentityModel(t, acct("Personal", "imap.example.com", "me@example.com", "pw"))
	orphanID, err := m.db.AddAccount(config.NewAccountID(), "Orphan", "")
	if err != nil {
		t.Fatal(err)
	}
	mailboxID, err := m.db.UpsertMailbox(db.Mailbox{AccountID: orphanID, Name: "INBOX"})
	if err != nil {
		t.Fatal(err)
	}
	m.accounts, _ = m.db.ListAccounts()
	m.mailboxes, _ = m.db.ListMailboxes(orphanID)
	got, err := m.accountCfgForMailbox(mailboxID)
	if !errors.Is(err, errNoAccountConfig) {
		t.Fatalf("expected errNoAccountConfig, got %v", err)
	}
	if got.ID != "" || got.IMAPHost != "" {
		t.Fatalf("orphan mailbox inherited peer credentials: %#v", got)
	}
}

func TestDuplicateAccountNamesAreDisambiguatedInSidebar(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Accounts = []config.AccountConfig{
		{ID: "cfg-a", Name: "Shared", User: "first@example.com"},
		{ID: "cfg-b", Name: "Shared", User: "second@example.com"},
	}
	m := NewModel(nil, cfg, "dev", false)
	m.accounts = []db.Account{
		{ID: 1, ConfigID: "cfg-a", Name: "Shared"},
		{ID: 2, ConfigID: "cfg-b", Name: "Shared"},
	}
	if got := m.renderAccountHeader(2, false, 60); !strings.Contains(got, "second@example.com") {
		t.Fatalf("duplicate account header was not disambiguated: %q", got)
	}
}

// newOrphanModel returns a model with one healthy account plus an account row
// that no [[account]] block owns, and a message sitting in the orphan's inbox.
func newOrphanModel(t *testing.T) (Model, db.Message, int64) {
	t.Helper()
	m, _, _ := newIdentityModel(t, acct("Personal", "imap.example.com", "me@example.com", "password"))
	orphanID, err := m.db.AddAccount(config.NewAccountID(), "Orphan", "")
	if err != nil {
		t.Fatal(err)
	}
	mailboxID, err := m.db.UpsertMailbox(db.Mailbox{AccountID: orphanID, Name: "INBOX"})
	if err != nil {
		t.Fatal(err)
	}
	m.accounts, _ = m.db.ListAccounts()
	m.mailboxes, _ = m.db.ListMailboxes(orphanID)
	message := db.Message{ID: 1, MailboxID: mailboxID, UID: 7, Subject: "Orphaned"}
	m.messages = []db.Message{message}
	m.filteredMessages = []db.Message{message}
	m.focused = paneMessages
	return m, message, mailboxID
}

// Archive, move and delete act on a whole selection, so an account they cannot
// resolve rejects the entire request. A half-applied destructive batch is worse
// than none, and the undo window would only cover the part that ran — so the
// batch here deliberately mixes a healthy message with an orphaned one.
func TestDestructiveBatchRejectedWhenAccountUnresolved(t *testing.T) {
	for _, tc := range []struct {
		name     string
		schedule func(m *Model, msgs []db.Message) tea.Cmd
		want     string
	}{
		{"archive", func(m *Model, msgs []db.Message) tea.Cmd { return m.scheduleArchive(msgs) }, "archive failed"},
		{"delete", func(m *Model, msgs []db.Message) tea.Cmd { return m.scheduleDelete(msgs) }, "delete failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, orphanMsg, _ := newOrphanModel(t)
			orphan := m.accounts[len(m.accounts)-1]
			healthy := m.accounts[0]
			// Archive resolves its target folder before the account, so both
			// accounts need one for the account check to be reached at all.
			for _, accountID := range []int64{healthy.ID, orphan.ID} {
				if _, err := m.db.UpsertMailbox(db.Mailbox{
					AccountID: accountID, Name: "Archive", Flags: []string{`\Archive`},
				}); err != nil {
					t.Fatal(err)
				}
			}
			healthyBox, err := m.db.UpsertMailbox(db.Mailbox{AccountID: healthy.ID, Name: "INBOX"})
			if err != nil {
				t.Fatal(err)
			}
			m.mailboxes = nil
			for _, accountID := range []int64{healthy.ID, orphan.ID} {
				boxes, _ := m.db.ListMailboxes(accountID)
				m.mailboxes = append(m.mailboxes, boxes...)
			}
			healthyMsg := db.Message{ID: 2, MailboxID: healthyBox, UID: 9, Subject: "Fine"}

			tc.schedule(&m, []db.Message{healthyMsg, orphanMsg})

			// The healthy message must not be scheduled on its own: the whole
			// request is refused, not quietly trimmed to the part that worked.
			if len(m.pendingDestructiveActions) != 0 {
				t.Fatalf("scheduled a partial destructive batch: %#v", m.pendingDestructiveActions)
			}
			if !m.statusErr || !strings.Contains(m.statusMsg, tc.want) ||
				!strings.Contains(m.statusMsg, errNoAccountConfig.Error()) {
				t.Fatalf("batch rejection was not explained: %q", m.statusMsg)
			}
		})
	}
}

// Star and folder creation report through the message they already return, so
// the normal handler shows the reason, and neither touches local or remote
// state first.
func TestResultMessageCallersReportUnresolvedAccount(t *testing.T) {
	m, message, _ := newOrphanModel(t)

	starMsg, ok := m.setMessageStarredCmd(message, true)().(MessageStarredUpdatedMsg)
	if !ok {
		t.Fatal("star did not return its usual result message")
	}
	if starMsg.Err == nil || !errors.Is(starMsg.Err, errNoAccountConfig) {
		t.Fatalf("star did not report the unresolved account: %v", starMsg.Err)
	}
	stored, err := m.db.GetMessage(message.ID)
	if err == nil && stored.Starred {
		t.Fatal("star mutated the database despite an unresolved account")
	}

	m.movePicker.messages = []db.Message{message}
	folderMsg, ok := m.createFolderCmd(m.accounts[len(m.accounts)-1].ID, "", "New")().(FolderCreatedMsg)
	if !ok {
		t.Fatal("folder creation did not return its usual result message")
	}
	if folderMsg.Err == nil || !errors.Is(folderMsg.Err, errNoAccountConfig) {
		t.Fatalf("folder creation did not report the unresolved account: %v", folderMsg.Err)
	}
}

// A filter run spans mailboxes that may belong to different accounts, and it
// already reports partial progress. One orphaned account must not stop the run
// for every healthy one — unlike the single-batch destructive actions above.
func TestFilterRunSkipsUnresolvedMailboxAndReports(t *testing.T) {
	m, _, orphanMailboxID := newOrphanModel(t)
	healthy := m.accounts[0]
	healthyMailboxID, err := m.db.UpsertMailbox(db.Mailbox{AccountID: healthy.ID, Name: "INBOX"})
	if err != nil {
		t.Fatal(err)
	}
	m.mailboxes, _ = m.db.ListMailboxes(healthy.ID)
	orphanBoxes, _ := m.db.ListMailboxes(m.accounts[len(m.accounts)-1].ID)
	m.mailboxes = append(m.mailboxes, orphanBoxes...)

	cmd := m.applyRulesCmd([]int64{healthyMailboxID, orphanMailboxID}, true, 0)
	if cmd == nil {
		t.Fatal("expected a filter run command")
	}
	run, ok := cmd().(FilterRunMsg)
	if !ok {
		t.Fatalf("expected FilterRunMsg, got %T", cmd())
	}
	// With no rules configured the run stops on that, which still proves the
	// unresolved mailbox did not abort it before the command was built.
	if run.Err != nil && errors.Is(run.Err, errNoAccountConfig) {
		t.Fatalf("an unresolved mailbox aborted the whole run: %v", run.Err)
	}
}

// A TideMail older than stable account IDs does not know the `id` field, so
// saving config.toml from one — an instance left running across an upgrade, or
// a downgrade — strips every id. The next launch stamps a fresh set, and before
// this fix matched nothing and imported the whole account list again. Doing that
// twice is how a four-account setup became twelve.
func TestStaleAccountRowsAreAdoptedNotDuplicated(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	configs := []config.AccountConfig{
		acct("Gmail", "imap.gmail.com", "me@gmail.com", "pw1"),
		acct("alliehere.com", "mail.alliehere.com", "allie@alliehere.com", "pw2"),
	}
	for i := range configs {
		configs[i].ID = config.NewAccountID()
	}

	accounts, err := database.ListAccounts()
	if err != nil {
		t.Fatal(err)
	}
	if accounts, err = ensureConfiguredAccounts(database, accounts, configs); err != nil {
		t.Fatalf("first import: %v", err)
	}
	if len(accounts) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(accounts))
	}
	// Give the original rows some cached mail so we can prove it survives.
	firstIDs := map[string]int64{}
	for _, a := range accounts {
		firstIDs[a.Name] = a.ID
		if _, err := database.UpsertMailbox(db.Mailbox{AccountID: a.ID, Name: "Archive"}); err != nil {
			t.Fatal(err)
		}
	}

	// The old binary strips the ids; the new one stamps fresh ones. Twice.
	for pass := 0; pass < 2; pass++ {
		for i := range configs {
			configs[i].ID = config.NewAccountID()
		}
		if accounts, err = ensureConfiguredAccounts(database, accounts, configs); err != nil {
			t.Fatalf("re-import pass %d: %v", pass, err)
		}
		if len(accounts) != 2 {
			t.Fatalf("pass %d left %d accounts, want 2: %#v", pass, len(accounts), accounts)
		}
	}

	// Same rows throughout, now carrying the current ids, cache intact.
	for _, a := range accounts {
		if a.ID != firstIDs[a.Name] {
			t.Fatalf("%s moved to a new row (%d, was %d) — its cached mail was abandoned",
				a.Name, a.ID, firstIDs[a.Name])
		}
		var want string
		for _, c := range configs {
			if c.Name == a.Name {
				want = c.ID
			}
		}
		if a.ConfigID != want {
			t.Fatalf("%s config_id = %q, want %q", a.Name, a.ConfigID, want)
		}
		boxes, err := database.ListMailboxes(a.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(boxes) < 2 {
			t.Fatalf("%s lost its mailboxes: %#v", a.Name, boxes)
		}
	}
}

// A row that another config account still owns must never be taken from it.
func TestAdoptionNeverStealsALiveAccountsRow(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	live := acct("Shared", "imap.one.example", "one@example.com", "pw1")
	live.ID = config.NewAccountID()
	accounts, err := ensureConfiguredAccounts(database, nil, []config.AccountConfig{live})
	if err != nil {
		t.Fatal(err)
	}
	liveRow := accounts[0].ID

	// A second account with the same display name arrives. The first one's row
	// is still claimed, so this must get its own.
	second := acct("Shared", "imap.two.example", "two@example.com", "pw2")
	second.ID = config.NewAccountID()
	accounts, err = ensureConfiguredAccounts(database, accounts, []config.AccountConfig{live, second})
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 2 {
		t.Fatalf("expected 2 rows, got %d: %#v", len(accounts), accounts)
	}
	for _, a := range accounts {
		if a.ID == liveRow && a.ConfigID != live.ID {
			t.Fatalf("the live account's row was reassigned to %q", a.ConfigID)
		}
	}
}
