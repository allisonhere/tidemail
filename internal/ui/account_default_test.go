package ui

import (
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
)

// The default account is the From account for a brand-new message and the
// account focused at startup. It is stored as a stable config ID, so editing
// or reordering accounts cannot move it.

func TestSpaceSetsTheDefaultAccount(t *testing.T) {
	m, saved, calls := newOrderedModel(t, nil, "")
	m = openAccounts(t, m)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = next.(Model)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("space on an account should report the new default upward")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)

	if m.cfg.DefaultAccountID != "bbb" {
		t.Fatalf("DefaultAccountID = %q, want bbb", m.cfg.DefaultAccountID)
	}
	if *calls != 1 {
		t.Fatalf("configSave called %d times, want 1", *calls)
	}
	if saved.DefaultAccountID != "bbb" {
		t.Fatalf("persisted DefaultAccountID = %q, want bbb", saved.DefaultAccountID)
	}
	if m.accountManager.defaultConfigID != "bbb" {
		t.Fatalf("account manager default = %q, want bbb", m.accountManager.defaultConfigID)
	}
}

func TestDefaultAccountIsStarredOnce(t *testing.T) {
	m, _, _ := newOrderedModel(t, nil, "bbb")
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)
	m = openAccounts(t, m)

	view := ansiEscapes.ReplaceAllString(m.View(), "")
	if got := strings.Count(view, "★"); got != 1 {
		t.Fatalf("star count = %d, want exactly 1:\n%s", got, view)
	}
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "★") && !strings.Contains(line, "Work") {
			t.Fatalf("the star is on the wrong account: %q", strings.TrimSpace(line))
		}
	}
}

func TestNoStarWithoutAnExplicitDefault(t *testing.T) {
	m, _, _ := newOrderedModel(t, nil, "")
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)
	m = openAccounts(t, m)

	view := ansiEscapes.ReplaceAllString(m.View(), "")
	if strings.Contains(view, "★") {
		t.Fatalf("the list starred an account nobody chose:\n%s", view)
	}
}

func TestComposeUsesTheDefaultAccount(t *testing.T) {
	m, _, _ := newOrderedModel(t, nil, "ccc")

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = next.(Model)

	if m.overlay != overlayCompose {
		t.Fatalf("overlay = %v, want compose", m.overlay)
	}
	if got := m.compose.selectedAccount().ID; got != "ccc" {
		t.Fatalf("compose sender = %q, want ccc", got)
	}
	if m.compose.accountIndex != 2 {
		t.Fatalf("accountIndex = %d, want 2 — ctrl+u must cycle from the default", m.compose.accountIndex)
	}
}

func TestComposeFallsBackToTheFirstAccount(t *testing.T) {
	m, _, _ := newOrderedModel(t, nil, "")

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = next.(Model)

	if got := m.compose.selectedAccount().ID; got != "aaa" {
		t.Fatalf("compose sender = %q, want the first account with no default set", got)
	}
}

func TestPaletteComposeUsesTheDefaultAccount(t *testing.T) {
	m, _, _ := newOrderedModel(t, nil, "bbb")

	next, _ := m.executeCommand("compose")
	m = next.(Model)

	if m.overlay != overlayCompose {
		t.Fatalf("overlay = %v, want compose", m.overlay)
	}
	if got := m.compose.selectedAccount().ID; got != "bbb" {
		t.Fatalf("palette compose sender = %q, want bbb", got)
	}
}

func TestStartupFocusesTheDefaultAccountInbox(t *testing.T) {
	m, _, _ := newOrderedModel(t, nil, "ccc")

	row := m.sidebarRows[m.sidebarCursor]
	if row.kind != rowKindMailbox {
		t.Fatalf("startup row kind = %v, want a mailbox row", row.kind)
	}
	mb := m.mailboxByID(row.mailboxID)
	if mb == nil || !strings.EqualFold(mb.Name, "INBOX") {
		t.Fatalf("startup row = %#v, want an INBOX", mb)
	}
	acc := m.accountByID(mb.AccountID)
	if acc == nil || acc.Name != "Old" {
		t.Fatalf("startup account = %#v, want the default account Old", acc)
	}
}

func TestStartupWithoutADefaultKeepsTheUnifiedInbox(t *testing.T) {
	m, _, _ := newOrderedModel(t, nil, "")

	if !m.selectedUnifiedInbox() {
		row := m.sidebarRows[m.sidebarCursor]
		t.Fatalf("startup row = %#v, want the unified inbox when no default is set", row)
	}
}

func TestDeletingTheDefaultAccountClearsIt(t *testing.T) {
	m, saved, _ := newOrderedModel(t, nil, "bbb")

	next, _ := m.Update(AccountDeletedMsg{
		AccountID:   m.accounts[1].ID,
		ConfigID:    "bbb",
		AccountName: "Work",
	})
	m = next.(Model)

	if saved.DefaultAccountID != "" {
		t.Fatalf("persisted DefaultAccountID = %q, want it cleared with the account", saved.DefaultAccountID)
	}
	fallback, ok := saved.DefaultAccount()
	if !ok || fallback.ID != "aaa" {
		t.Fatalf("fallback account = %q, want aaa", fallback.ID)
	}
}

func TestEditingTheDefaultAccountKeepsIt(t *testing.T) {
	m, _, _ := newOrderedModel(t, nil, "bbb")

	edited := m.cfg.Accounts[1]
	edited.Name = "Work (renamed)"
	m = runAccountPersistence(t, m, AccountSavedMsg{
		AccountCfg: edited,
		EditID:     m.accounts[1].ID,
	})

	if m.cfg.DefaultAccountID != "bbb" {
		t.Fatalf("DefaultAccountID = %q, want bbb to survive an edit of that account", m.cfg.DefaultAccountID)
	}
	got, ok := m.cfg.DefaultAccount()
	if !ok || got.Name != "Work (renamed)" {
		t.Fatalf("DefaultAccount() = %q, want the renamed account", got.Name)
	}
}

func TestDefaultAccountSurvivesReordering(t *testing.T) {
	m, _, _ := newOrderedModel(t, nil, "aaa")
	m = openAccounts(t, m)

	m = pressAccounts(t, m, 'J')

	if m.cfg.DefaultAccountID != "aaa" {
		t.Fatalf("DefaultAccountID = %q, want aaa — the default is an ID, not a position", m.cfg.DefaultAccountID)
	}
	if got := configOrder(m.cfg); got != "bbb,aaa,ccc" {
		t.Fatalf("order = %s, want bbb,aaa,ccc", got)
	}
}

func TestFilterScopeFallsBackToTheDefaultAccount(t *testing.T) {
	m, _, _ := newOrderedModel(t, nil, "ccc")
	// No mailbox selected: the scope is the default account, not row zero.
	m.sidebarCursor = -1

	want := m.accounts[2].ID
	if got := m.filterScopeAccountID(); got != want {
		t.Fatalf("filterScopeAccountID() = %d, want the default account %d", got, want)
	}
}

func TestSetDefaultOnAnUnconfiguredRowIsRejected(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	// An orphan row: a database account with no reachable [[account]] block.
	if _, err := database.AddAccount("orphan", "Ghost", ""); err != nil {
		t.Fatalf("AddAccount: %v", err)
	}

	m := NewModel(database, config.DefaultConfig(), "dev", false)
	next, _ := m.Update(AccountsLoadedMsg{Accounts: []db.Account{{ID: 1, ConfigID: "orphan", Name: "Ghost"}}})
	m = next.(Model)
	m = openAccounts(t, m)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = next.(Model)

	if cmd != nil {
		t.Fatal("an account with no settings must not become the default sender")
	}
	if m.accountManager.defaultConfigID != "" {
		t.Fatalf("default = %q, want it unset", m.accountManager.defaultConfigID)
	}
	if !strings.Contains(m.accountManager.statusMsg, "no settings") {
		t.Fatalf("status = %q, want it to explain the account has no settings", m.accountManager.statusMsg)
	}
}
