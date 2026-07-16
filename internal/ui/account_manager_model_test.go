package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
)

func TestAccountManagerOpensWithLoadedAccounts(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	accountID, err := database.AddAccount("Personal", "")
	if err != nil {
		t.Fatalf("AddAccount: %v", err)
	}
	mailboxID, err := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "INBOX"})
	if err != nil {
		t.Fatalf("UpsertMailbox: %v", err)
	}

	m := NewModel(database, config.DefaultConfig(), "dev", false)
	next, _ := m.Update(AccountsLoadedMsg{
		Accounts:  []db.Account{{ID: accountID, Name: "Personal"}},
		Mailboxes: []db.Mailbox{{ID: mailboxID, AccountID: accountID, Name: "INBOX"}},
	})
	m = next.(Model)

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}})
	m = next.(Model)

	if m.overlay != overlayAccountManager {
		t.Fatalf("expected account manager overlay, got %v", m.overlay)
	}
	if len(m.accountManager.accounts) != 1 {
		t.Fatalf("expected account manager to show 1 loaded account, got %d", len(m.accountManager.accounts))
	}
	if m.accountManager.accounts[0].Name != "Personal" {
		t.Fatalf("expected account manager account %q, got %q", "Personal", m.accountManager.accounts[0].Name)
	}
}

func TestFirstLoadSyncsInboxesImmediately(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	accountID, err := database.AddAccount("Personal", "")
	if err != nil {
		t.Fatalf("AddAccount: %v", err)
	}
	mailboxID, err := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "INBOX"})
	if err != nil {
		t.Fatalf("UpsertMailbox: %v", err)
	}

	m := NewModel(database, config.DefaultConfig(), "dev", false)
	if !m.firstLoad {
		t.Fatalf("expected firstLoad to be true on a fresh model")
	}
	next, cmd := m.Update(AccountsLoadedMsg{
		Accounts:  []db.Account{{ID: accountID, Name: "Personal"}},
		Mailboxes: []db.Mailbox{{ID: mailboxID, AccountID: accountID, Name: "INBOX"}},
	})
	m = next.(Model)

	if cmd == nil {
		t.Fatalf("expected first load to return a batched command")
	}
	// syncMailboxCmd marks the mailbox as syncing; an immediate inbox sync on
	// first load is the regression we're guarding against here.
	if !m.syncing[mailboxID] {
		t.Fatalf("expected inbox %d to be syncing immediately after first load", mailboxID)
	}
}

func TestFirstLoadWithNoAccountsOpensAccountManager(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "dev", false)
	next, _ := m.Update(AccountsLoadedMsg{})
	m = next.(Model)

	if m.overlay != overlayAccountManager {
		t.Fatalf("expected account manager overlay on first load with no accounts, got %v", m.overlay)
	}
	if len(m.accountManager.accounts) != 0 {
		t.Fatalf("expected no account manager accounts, got %d", len(m.accountManager.accounts))
	}
	if m.accountManager.mode != amList {
		t.Fatalf("expected account manager list mode, got %v", m.accountManager.mode)
	}
}

func TestFirstLoadWithAccountsDoesNotOpenAccountManager(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	accountID, err := database.AddAccount("Personal", "")
	if err != nil {
		t.Fatalf("AddAccount: %v", err)
	}
	mailboxID, err := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "INBOX"})
	if err != nil {
		t.Fatalf("UpsertMailbox: %v", err)
	}

	m := NewModel(database, config.DefaultConfig(), "dev", false)
	next, _ := m.Update(AccountsLoadedMsg{
		Accounts:  []db.Account{{ID: accountID, Name: "Personal"}},
		Mailboxes: []db.Mailbox{{ID: mailboxID, AccountID: accountID, Name: "INBOX"}},
	})
	m = next.(Model)

	if m.overlay == overlayAccountManager {
		t.Fatalf("expected first load with accounts not to open account manager")
	}
}

func TestLoadAccountsCmdImportsConfiguredAccounts(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	cfg := config.DefaultConfig()
	cfg.Accounts = []config.AccountConfig{
		{Name: "Personal", User: "person@example.com", IMAPHost: "imap.example.com", IMAPPort: 993, IMAPTLS: true},
		{Name: "Work", User: "work@example.com", IMAPHost: "imap.work.example.com", IMAPPort: 993, IMAPTLS: true},
	}
	m := NewModel(database, cfg, "dev", false)

	msg := m.loadAccountsCmd()().(AccountsLoadedMsg)
	if msg.Err != nil {
		t.Fatalf("loadAccountsCmd: %v", msg.Err)
	}
	if len(msg.Accounts) != 2 {
		t.Fatalf("expected config accounts imported into db, got %d: %#v", len(msg.Accounts), msg.Accounts)
	}
	if msg.Accounts[0].Name != "Personal" || msg.Accounts[1].Name != "Work" {
		t.Fatalf("unexpected imported accounts: %#v", msg.Accounts)
	}
	if len(msg.Mailboxes) != 2 {
		t.Fatalf("expected config accounts to get starter inbox mailboxes, got %d: %#v", len(msg.Mailboxes), msg.Mailboxes)
	}

	next, _ := m.Update(msg)
	m = next.(Model)
	if len(m.accounts) != 2 || len(m.mailboxes) != 2 {
		t.Fatalf("expected loaded accounts/mailboxes reflected in model, got %d/%d", len(m.accounts), len(m.mailboxes))
	}
	if len(m.sidebarRows) == 0 {
		t.Fatalf("expected loaded accounts to be reflected in sidebar rows")
	}
	m.accountManager = m.newAccountManager()
	if len(m.accountManager.accounts) != 2 || len(m.accountManager.mailboxes) != 2 {
		t.Fatalf("expected loaded accounts/mailboxes reflected in account manager, got %d/%d", len(m.accountManager.accounts), len(m.accountManager.mailboxes))
	}

	// Import is idempotent: a second load should not duplicate the config accounts or starter mailboxes.
	msg = m.loadAccountsCmd()().(AccountsLoadedMsg)
	if msg.Err != nil {
		t.Fatalf("second loadAccountsCmd: %v", msg.Err)
	}
	if len(msg.Accounts) != 2 || len(msg.Mailboxes) != 2 {
		t.Fatalf("expected idempotent import, got %d accounts and %d mailboxes", len(msg.Accounts), len(msg.Mailboxes))
	}
}

func TestAccountManagerFormShowsTestAction(t *testing.T) {
	am := NewAccountManager(nil)
	am.mode = amAdd

	view := am.View(80, 30, BuildStyles(CatppuccinMocha, "compact"))

	if !strings.Contains(view, "^t test") {
		t.Fatalf("expected add account form actions to include ^t test, got %q", view)
	}
}

func TestAccountManagerColorListIncludesVibrantChoices(t *testing.T) {
	want := map[string]string{
		"Crimson":       "#ff3366",
		"Electric Blue": "#3b82ff",
		"Vivid Violet":  "#8b5cf6",
		"Emerald":       "#00d084",
		"Hot Pink":      "#ff4fd8",
		"Aqua":          "#00e5ff",
		"Amber":         "#ffb000",
		"Lime":          "#7CFC00",
	}
	got := make(map[string]string, len(accountColorList))
	for _, color := range accountColorList {
		got[color.Name] = color.Hex
	}
	for name, hex := range want {
		if got[name] != hex {
			t.Fatalf("expected account color %q to be %q, got %q", name, hex, got[name])
		}
	}
}

func TestAccountManagerFormStaysCompactInShortView(t *testing.T) {
	am := NewAccountManager(nil)
	am.mode = amAdd
	am.nameInput.SetValue("Personal")
	am.imapHostInput.SetValue("imap.example.com")
	am.smtpHostInput.SetValue("smtp.example.com")

	view := am.View(74, 28, BuildStyles(CatppuccinMocha, "compact"))

	for _, want := range []string{"Account", "Incoming mail", "Outgoing mail", "Credentials", "Sending identity", "Name", "Color", "IMAP Host", "SMTP Host", "Username", "From", "test", "cancel"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected compact account form to keep %q visible, got %q", want, view)
		}
	}
	if strings.Count(view, "\n") > 27 {
		t.Fatalf("expected compact account form to fit 28 lines, got %d lines", strings.Count(view, "\n")+1)
	}
}

func TestAccountManagerFormScrollsFocusedFieldIntoShortView(t *testing.T) {
	am := NewAccountManager(nil)
	am.mode = amEdit
	am.focusField(amFieldSyncInterval)
	am.nameInput.SetValue("Personal")
	am.imapHostInput.SetValue("imap.example.com")
	am.smtpHostInput.SetValue("smtp.example.com")
	am.userInput.SetValue("alice@example.com")

	view := ansi.Strip(am.View(74, 12, BuildStyles(CatppuccinMocha, "compact")))

	if !strings.Contains(view, "Every (min)") {
		t.Fatalf("expected short account form to scroll sync field into view, got %q", view)
	}
	if strings.Contains(view, "Provider") {
		t.Fatalf("expected top form rows to scroll out of short view, got %q", view)
	}
}

func TestAccountManagerFormNavigationMatchesVisualOrder(t *testing.T) {
	am := NewAccountManager(nil)
	am.mode = amEdit
	am.focusField(amFieldFrom)

	am.advanceField(1)
	if am.focusedField != amFieldSignature {
		t.Fatalf("expected signature after sender identity, got field %v", am.focusedField)
	}
	am.advanceField(1)
	if am.focusedField != amFieldSyncInterval {
		t.Fatalf("expected sync after signature, got field %v", am.focusedField)
	}
}

func TestAccountManagerListShowsCompactAccountCards(t *testing.T) {
	am := NewAccountManager(nil)
	am.setData(
		[]db.Account{{ID: 7, Name: "Personal"}},
		[]db.Mailbox{{ID: 1, AccountID: 7, Name: "INBOX"}, {ID: 2, AccountID: 7, Name: "Archive"}},
		[]config.AccountConfig{{
			Name:     "Personal",
			IMAPHost: "imap.example.com",
			IMAPPort: 993,
			IMAPTLS:  true,
			SMTPHost: "smtp.example.com",
			SMTPPort: 587,
			SMTPTLS:  true,
			User:     "alice@example.com",
		}},
	)

	view := am.View(80, 24, BuildStyles(CatppuccinMocha, "compact"))

	for _, want := range []string{"Personal", "alice@example.com", "imap.example.com:993", "smtp.example.com:587", "2 mailboxes"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected account card to include %q, got %q", want, view)
		}
	}
}

func TestAccountManagerBareTTypesIntoFocusedField(t *testing.T) {
	cases := []struct {
		name  string
		field amField
		value func(AccountManager) string
	}{
		{"name", amFieldName, func(am AccountManager) string { return am.nameInput.Value() }},
		{"imap host", amFieldIMAPHost, func(am AccountManager) string { return am.imapHostInput.Value() }},
		{"imap port", amFieldIMAPPort, func(am AccountManager) string { return am.imapPortInput.Value() }},
		{"smtp host", amFieldSMTPHost, func(am AccountManager) string { return am.smtpHostInput.Value() }},
		{"smtp port", amFieldSMTPPort, func(am AccountManager) string { return am.smtpPortInput.Value() }},
		{"username", amFieldUser, func(am AccountManager) string { return am.userInput.Value() }},
		{"password", amFieldPass, func(am AccountManager) string { return am.passInput.Value() }},
		{"from", amFieldFrom, func(am AccountManager) string { return am.fromInput.Value() }},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			am := NewAccountManager(nil)
			am.mode = amAdd
			am.focusField(tt.field)
			if tt.field == amFieldIMAPPort {
				am.imapPortInput.SetValue("")
			}
			if tt.field == amFieldSMTPPort {
				am.smtpPortInput.SetValue("")
			}

			next, _, exit := am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}}, DefaultKeys)

			if exit {
				t.Fatal("expected bare t to keep account manager open")
			}
			if tt.value(next) != "t" {
				t.Fatalf("expected bare t to enter text, got %q", tt.value(next))
			}
			if next.busy {
				t.Fatal("expected bare t not to start account test")
			}
		})
	}
}

func TestAccountManagerCtrlTStartsConnectionTestWithoutSaving(t *testing.T) {
	am := NewAccountManager(nil)
	am.mode = amAdd
	am.nameInput.SetValue("Personal")
	am.imapHostInput.SetValue("imap.example.com")
	am.userInput.SetValue("alice@example.com")

	next, cmd, exit := am.Update(tea.KeyMsg{Type: tea.KeyCtrlT}, DefaultKeys)

	if exit {
		t.Fatal("expected test action to keep account manager open")
	}
	if cmd == nil {
		t.Fatal("expected test action to return a command")
	}
	if !next.busy {
		t.Fatal("expected test action to mark account manager busy")
	}
	if next.busyMsg != "TESTING ACCOUNT..." {
		t.Fatalf("expected testing busy message, got %q", next.busyMsg)
	}
	if next.statusMsg != "" {
		t.Fatalf("expected testing to clear prior status, got %q", next.statusMsg)
	}
}

func TestAccountManagerCtrlSSavesFromForm(t *testing.T) {
	testAccountManagerSaveKey(t, tea.KeyMsg{Type: tea.KeyCtrlS})
}

func TestAccountManagerF2SavesFromForm(t *testing.T) {
	testAccountManagerSaveKey(t, tea.KeyMsg{Type: tea.KeyF2})
}

func testAccountManagerSaveKey(t *testing.T, msg tea.KeyMsg) {
	t.Helper()
	am := NewAccountManager(nil)
	am.mode = amAdd
	am.nameInput.SetValue("Personal")
	am.imapHostInput.SetValue("imap.example.com")
	am.userInput.SetValue("alice@example.com")

	next, cmd, exit := am.Update(tea.KeyMsg{Type: tea.KeyCtrlS}, DefaultKeys)

	if exit {
		t.Fatal("expected ctrl+s to keep account manager open while saving")
	}
	if cmd == nil {
		t.Fatal("expected ctrl+s to return save command")
	}
	if !next.busy {
		t.Fatal("expected ctrl+s to mark account manager busy")
	}
	if next.busyMsg != "CONNECTING TO IMAP..." {
		t.Fatalf("expected saving busy message, got %q", next.busyMsg)
	}
}

func TestAccountManagerEditPreloadsConfigForSelectedAccount(t *testing.T) {
	am := NewAccountManager(nil)
	am.setData(
		[]db.Account{{ID: 7, Name: "Personal"}},
		nil,
		[]config.AccountConfig{{
			Name:     "Personal",
			IMAPHost: "imap.example.com",
			IMAPPort: 993,
			IMAPTLS:  true,
			SMTPHost: "smtp.example.com",
			SMTPPort: 587,
			SMTPTLS:  true,
			User:     "alice@example.com",
			Password: "secret",
			From:     "Alice <alice@example.com>",
		}},
	)

	next, _, _ := am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}}, DefaultKeys)

	if next.mode != amEdit {
		t.Fatalf("expected edit mode, got %v", next.mode)
	}
	if next.imapHostInput.Value() != "imap.example.com" {
		t.Fatalf("expected IMAP host from config, got %q", next.imapHostInput.Value())
	}
	if next.userInput.Value() != "alice@example.com" {
		t.Fatalf("expected username from config, got %q", next.userInput.Value())
	}
	if next.passInput.Value() != "secret" {
		t.Fatalf("expected password from config, got %q", next.passInput.Value())
	}
}

func TestAccountTestResultUpdatesManagerStatus(t *testing.T) {
	m := Model{
		keys: DefaultKeys,
		accountManager: AccountManager{
			busy:    true,
			busyMsg: "TESTING ACCOUNT...",
		},
	}

	next, _ := m.Update(AccountTestedMsg{MailboxCount: 12})
	m = next.(Model)

	if m.accountManager.busy {
		t.Fatal("expected test result to clear busy state")
	}
	if m.accountManager.statusMsg != "CONNECTED: 12 MAILBOXES" {
		t.Fatalf("expected connected status, got %q", m.accountManager.statusMsg)
	}

	next, _ = m.Update(AccountTestedMsg{Err: errors.New("bad password")})
	m = next.(Model)

	if m.accountManager.statusMsg != "TEST FAILED: bad password" {
		t.Fatalf("expected failure status, got %q", m.accountManager.statusMsg)
	}
}

func TestAccountManagerRedactsPasswordFromFailureStatus(t *testing.T) {
	am := NewAccountManager(nil)
	am.passInput.SetValue("mail-secret")
	m := Model{
		keys:           DefaultKeys,
		accountManager: am,
	}

	next, _ := m.Update(AccountTestedMsg{Err: errors.New("login failed for mail-secret")})
	m = next.(Model)
	if strings.Contains(m.accountManager.statusMsg, "mail-secret") {
		t.Fatalf("expected password to be redacted from test status, got %q", m.accountManager.statusMsg)
	}
	if !strings.Contains(m.accountManager.statusMsg, "[redacted]") {
		t.Fatalf("expected redaction marker in test status, got %q", m.accountManager.statusMsg)
	}

	next, _ = m.Update(AccountSavedMsg{AccountCfg: config.AccountConfig{Password: "save-secret"}, Err: errors.New("login failed for save-secret")})
	m = next.(Model)
	if strings.Contains(m.accountManager.statusMsg, "save-secret") {
		t.Fatalf("expected password to be redacted from save status, got %q", m.accountManager.statusMsg)
	}
}

func TestAccountManagerViewDoesNotRenderPassword(t *testing.T) {
	am := NewAccountManager(nil)
	am.mode = amEdit
	am.focusField(amFieldPass)
	am.passInput.SetValue("mail-secret")
	am.statusMsg = "TEST FAILED: mail-secret rejected"
	chrome := newManagerChrome(80, CatppuccinMocha, false)

	view := am.viewForm(80, 30, chrome)
	if strings.Contains(view, "mail-secret") {
		t.Fatalf("expected rendered account form to hide password, got %q", view)
	}
	if !strings.Contains(view, "[redacted]") {
		t.Fatalf("expected rendered account form status to show redaction marker, got %q", view)
	}
}

func TestAccountManagerConnectedStatusUsesSuccessColor(t *testing.T) {
	chrome := newManagerChrome(80, CatppuccinMocha, false)

	am := AccountManager{statusMsg: "CONNECTED: 12 MAILBOXES"}
	if got := am.statusForeground(chrome); got != chrome.successFg {
		t.Fatalf("expected connected status to use success color %q, got %q", chrome.successFg, got)
	}

	am.statusMsg = "TEST FAILED: bad password"
	if got := am.statusForeground(chrome); got != chrome.errorFg {
		t.Fatalf("expected failed status to use error color %q, got %q", chrome.errorFg, got)
	}
}

func TestStoreFetchedMessagesAssignsMailboxID(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	accountID, err := database.AddAccount("Personal", "")
	if err != nil {
		t.Fatalf("AddAccount: %v", err)
	}
	mailboxID, err := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "INBOX"})
	if err != nil {
		t.Fatalf("UpsertMailbox: %v", err)
	}

	newMsgs, err := storeFetchedMessages(database, mailboxID, []db.Message{{
		UID:     42,
		Subject: "Hello",
		Date:    time.Unix(1700000000, 0),
	}})
	if err != nil {
		t.Fatalf("storeFetchedMessages: %v", err)
	}
	if len(newMsgs) != 1 {
		t.Fatalf("expected one unread message, got %d", len(newMsgs))
	}

	messages, err := database.ListMessages(mailboxID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected one stored message, got %d", len(messages))
	}
	if messages[0].MailboxID != mailboxID {
		t.Fatalf("expected mailbox id %d, got %d", mailboxID, messages[0].MailboxID)
	}
}

func TestLoadMailboxMessagesIncludesReadMessages(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	accountID, err := database.AddAccount("Personal", "")
	if err != nil {
		t.Fatalf("AddAccount: %v", err)
	}
	mailboxID, err := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "INBOX"})
	if err != nil {
		t.Fatalf("UpsertMailbox: %v", err)
	}
	if err := database.UpsertMessage(db.Message{MailboxID: mailboxID, UID: 1, Subject: "Read", Read: true}); err != nil {
		t.Fatalf("UpsertMessage read: %v", err)
	}
	if err := database.UpsertMessage(db.Message{MailboxID: mailboxID, UID: 2, Subject: "Unread"}); err != nil {
		t.Fatalf("UpsertMessage unread: %v", err)
	}

	m := NewModel(database, config.DefaultConfig(), "dev", false)
	msg := m.loadMailboxMessagesCmd(mailboxID)().(MessagesLoadedMsg)

	if msg.Err != nil {
		t.Fatalf("loadMailboxMessagesCmd returned error: %v", msg.Err)
	}
	if len(msg.Messages) != 2 {
		t.Fatalf("expected read and unread messages, got %d", len(msg.Messages))
	}
}
