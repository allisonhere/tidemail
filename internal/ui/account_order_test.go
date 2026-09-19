package ui

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
)

// Account order lives in config.toml. These tests pin that down: the database's
// position column is insertion order and must never win, and Shift+J/Shift+K
// rewrite the config rather than the database.

// ansiEscapes strips styling so a rendered view can be asserted on as text.
var ansiEscapes = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func orderedAcct(id, name, user string) config.AccountConfig {
	return config.AccountConfig{
		ID: id, Name: name, User: user,
		IMAPHost: "imap." + name + ".example", IMAPPort: 993, IMAPTLS: true,
		SMTPHost: "smtp." + name + ".example", SMTPPort: 587, SMTPTLS: true,
	}
}

// newOrderedModel builds a model with three configured accounts, matching
// database rows (inserted in config order), an INBOX each, and the config-save
// seam captured. saveErr, when non-nil, makes every config write fail.
func newOrderedModel(t *testing.T, saveErr error, defaultID string) (Model, *config.Config, *int) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	cfg := config.DefaultConfig()
	cfg.Accounts = []config.AccountConfig{
		orderedAcct("aaa", "Personal", "mira@example.com"),
		orderedAcct("bbb", "Work", "mira@work.example"),
		orderedAcct("ccc", "Old", "mira@isp.example"),
	}
	cfg.DefaultAccountID = defaultID

	for _, a := range cfg.Accounts {
		accountID, err := database.AddAccount(a.ID, a.Name, "")
		if err != nil {
			t.Fatalf("AddAccount %s: %v", a.Name, err)
		}
		if _, err := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "INBOX"}); err != nil {
			t.Fatalf("UpsertMailbox %s: %v", a.Name, err)
		}
	}

	orig := configSave
	saved := &config.Config{}
	calls := new(int)
	configSave = func(c config.Config) error {
		*calls++
		if saveErr != nil {
			return saveErr
		}
		*saved = c
		return nil
	}
	t.Cleanup(func() { configSave = orig })

	m := NewModel(database, cfg, "dev", false)
	loaded := m.loadAccountsCmd()().(AccountsLoadedMsg)
	if loaded.Err != nil {
		t.Fatalf("loadAccountsCmd: %v", loaded.Err)
	}
	next, _ := m.Update(loaded)
	return next.(Model), saved, calls
}

// openAccounts opens the account manager overlay.
func openAccounts(t *testing.T, m Model) Model {
	t.Helper()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}})
	m = next.(Model)
	if m.overlay != overlayAccountManager {
		t.Fatalf("expected the account manager overlay, got %v", m.overlay)
	}
	return m
}

// pressAccounts sends a rune to the account manager and runs whatever command
// it returns back through the model, the way the bubbletea loop would.
func pressAccounts(t *testing.T, m Model, r rune) Model {
	t.Helper()
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	m = next.(Model)
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			break
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			_ = batch
			break
		}
		next, cmd = m.Update(msg)
		m = next.(Model)
	}
	return m
}

func managerOrder(m Model) string {
	names := make([]string, 0, len(m.accountManager.accounts))
	for _, a := range m.accountManager.accounts {
		names = append(names, a.Name)
	}
	return strings.Join(names, ",")
}

func modelOrder(m Model) string {
	names := make([]string, 0, len(m.accounts))
	for _, a := range m.accounts {
		names = append(names, a.Name)
	}
	return strings.Join(names, ",")
}

func configOrder(c config.Config) string {
	ids := make([]string, 0, len(c.Accounts))
	for _, a := range c.Accounts {
		ids = append(ids, a.ID)
	}
	return strings.Join(ids, ",")
}

func TestShiftJMovesAccountDownAndPersists(t *testing.T) {
	m, saved, calls := newOrderedModel(t, nil, "")
	m = openAccounts(t, m)

	m = pressAccounts(t, m, 'J')

	if got := configOrder(m.cfg); got != "bbb,aaa,ccc" {
		t.Fatalf("config order = %s, want bbb,aaa,ccc", got)
	}
	if *calls != 1 {
		t.Fatalf("configSave called %d times, want 1", *calls)
	}
	if got := configOrder(*saved); got != "bbb,aaa,ccc" {
		t.Fatalf("persisted order = %s, want bbb,aaa,ccc", got)
	}
	if got := managerOrder(m); got != "Work,Personal,Old" {
		t.Fatalf("account manager order = %s, want Work,Personal,Old", got)
	}
	if got := modelOrder(m); got != "Work,Personal,Old" {
		t.Fatalf("sidebar account order = %s, want Work,Personal,Old", got)
	}
	if m.accountManager.cursor != 1 {
		t.Fatalf("cursor = %d, want 1 — the highlight follows the moved account", m.accountManager.cursor)
	}
}

func TestShiftKMovesAccountUp(t *testing.T) {
	m, _, _ := newOrderedModel(t, nil, "")
	m = openAccounts(t, m)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = next.(Model)
	if m.accountManager.cursor != 1 {
		t.Fatalf("setup: cursor = %d, want 1", m.accountManager.cursor)
	}

	m = pressAccounts(t, m, 'K')

	if got := configOrder(m.cfg); got != "bbb,aaa,ccc" {
		t.Fatalf("config order = %s, want bbb,aaa,ccc", got)
	}
	if m.accountManager.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.accountManager.cursor)
	}
}

func TestShiftMovesAtTheEndsAreNoops(t *testing.T) {
	m, _, calls := newOrderedModel(t, nil, "")
	m = openAccounts(t, m)

	m = pressAccounts(t, m, 'K') // already at the top
	if got := configOrder(m.cfg); got != "aaa,bbb,ccc" {
		t.Fatalf("order = %s, want it unchanged at the top", got)
	}

	for i := 0; i < 2; i++ {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
		m = next.(Model)
	}
	m = pressAccounts(t, m, 'J') // already at the bottom
	if got := configOrder(m.cfg); got != "aaa,bbb,ccc" {
		t.Fatalf("order = %s, want it unchanged at the bottom", got)
	}
	if *calls != 0 {
		t.Fatalf("configSave called %d times, want 0 for a no-op move", *calls)
	}
}

func TestReorderRevertsWhenTheConfigWriteFails(t *testing.T) {
	m, _, _ := newOrderedModel(t, errors.New("disk full"), "")
	m = openAccounts(t, m)

	m = pressAccounts(t, m, 'J')

	if got := configOrder(m.cfg); got != "aaa,bbb,ccc" {
		t.Fatalf("config order = %s, want it unchanged after a failed write", got)
	}
	if got := managerOrder(m); got != "Personal,Work,Old" {
		t.Fatalf("account manager order = %s, want the optimistic move reverted", got)
	}
	if m.accountManager.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 — the highlight follows the reverted account", m.accountManager.cursor)
	}
	if !strings.HasPrefix(m.accountManager.statusMsg, "SAVE FAILED") {
		t.Fatalf("status = %q, want a SAVE FAILED message", m.accountManager.statusMsg)
	}
}

func TestSidebarOrderFollowsConfigNotDatabasePosition(t *testing.T) {
	m, _, _ := newOrderedModel(t, nil, "")

	// The database rows were inserted in config order; flip the config and the
	// rows must follow on the next rebuild.
	m.cfg.ReorderAccounts([]string{"ccc", "bbb", "aaa"})
	m.rebuildSidebar()

	if got := modelOrder(m); got != "Old,Work,Personal" {
		t.Fatalf("account order = %s, want Old,Work,Personal", got)
	}
	var rowNames []string
	for _, row := range m.sidebarRows {
		if row.kind == rowKindAccount {
			if acc := m.accountByID(row.accountID); acc != nil {
				rowNames = append(rowNames, acc.Name)
			}
		}
	}
	if got := strings.Join(rowNames, ","); got != "Old,Work,Personal" {
		t.Fatalf("sidebar rows = %s, want Old,Work,Personal", got)
	}
}

func TestSortAccountsByConfigOrderParksUnresolvedRowsLast(t *testing.T) {
	configs := []config.AccountConfig{{ID: "aaa", Name: "Personal"}, {ID: "bbb", Name: "Work"}}
	accounts := []db.Account{
		{ID: 1, ConfigID: "orphan", Name: "Gone"},
		{ID: 2, ConfigID: "bbb", Name: "Work"},
		{ID: 3, ConfigID: "", Name: "Nameless"},
		{ID: 4, ConfigID: "aaa", Name: "Personal"},
	}

	got := sortAccountsByConfigOrder(accounts, configs)

	var ids []int64
	for _, a := range got {
		ids = append(ids, a.ID)
	}
	want := []int64{4, 2, 1, 3}
	if len(ids) != len(want) {
		t.Fatalf("got %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("order = %v, want %v — configured rows first, unresolved ones last in their original order", ids, want)
		}
	}
	if accounts[0].ID != 1 {
		t.Fatal("sortAccountsByConfigOrder must not mutate its input")
	}
}

func TestAccountCardsJoinConfigsByIDNotName(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	// Two accounts sharing a display name: pairing cards by name showed each of
	// them the other's server details.
	cfg := config.DefaultConfig()
	cfg.Accounts = []config.AccountConfig{
		orderedAcct("aaa", "Mail", "first@example.com"),
		orderedAcct("bbb", "Mail", "second@example.com"),
	}
	for _, a := range cfg.Accounts {
		if _, err := database.AddAccount(a.ID, a.Name, ""); err != nil {
			t.Fatalf("AddAccount: %v", err)
		}
	}

	m := NewModel(database, cfg, "dev", false)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)
	loaded := m.loadAccountsCmd()().(AccountsLoadedMsg)
	next, _ = m.Update(loaded)
	m = next.(Model)
	m = openAccounts(t, m)

	view := ansiEscapes.ReplaceAllString(m.View(), "")
	for _, want := range []string{"first@example.com", "second@example.com"} {
		if !strings.Contains(view, want) {
			t.Fatalf("account list is missing %q; cards are still joined by display name:\n%s", want, view)
		}
	}
}
