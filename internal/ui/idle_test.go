package ui

import (
	"testing"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	imapClient "github.com/allisonhere/tide/internal/imap"
)

func TestIdleEventTriggersInboxSyncAndRearms(t *testing.T) {
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

	// Install a watcher the model believes is current. The bogus config makes
	// its connect attempts fail fast; backoff keeps it quiet for the test.
	w := imapClient.NewWatcher(config.AccountConfig{Name: "Personal", IMAPHost: "127.0.0.1", IMAPPort: 1}, "INBOX", nil)
	defer w.Close()
	m.idleWatchers[accountID] = idleWatcherEntry{watcher: w, mailboxID: mailboxID, mailboxName: "INBOX"}

	next, cmd := m.Update(IdleEventMsg{AccountID: accountID, MailboxID: mailboxID, Watcher: w})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("expected an idle event to produce a sync + re-arm command batch")
	}
	if !m.syncing[mailboxID] {
		t.Fatal("expected the idle event to start syncing the inbox")
	}
}

func TestIdleEventFromStaleWatcherIsIgnored(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "dev", false)

	stale := imapClient.NewWatcher(config.AccountConfig{Name: "Old", IMAPHost: "127.0.0.1", IMAPPort: 1}, "INBOX", nil)
	defer stale.Close()
	// Not registered in m.idleWatchers → the model must drop its events.
	next, cmd := m.Update(IdleEventMsg{AccountID: 1, MailboxID: 2, Watcher: stale})
	m = next.(Model)
	if cmd != nil {
		t.Fatal("expected a stale watcher's event to be dropped without commands")
	}
	if m.syncing[2] {
		t.Fatal("expected no sync from a stale watcher's event")
	}
}

func TestStartIdleWatchersRespectsSyncMinutesGate(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	cfg := config.DefaultConfig()
	cfg.Accounts = []config.AccountConfig{
		{Name: "Push", IMAPHost: "127.0.0.1", IMAPPort: 1, SyncMinutes: 5},
		{Name: "ManualOnly", IMAPHost: "127.0.0.1", IMAPPort: 1, SyncMinutes: 0},
	}
	m := NewModel(database, cfg, "dev", false)
	m.accounts = []db.Account{{ID: 1, Name: "Push"}, {ID: 2, Name: "ManualOnly"}}
	m.mailboxes = []db.Mailbox{
		{ID: 10, AccountID: 1, Name: "INBOX"},
		{ID: 20, AccountID: 2, Name: "INBOX"},
	}

	cmd := m.startIdleWatchers()
	defer m.stopIdleWatchers()

	if cmd == nil {
		t.Fatal("expected a wait command for the push-enabled account")
	}
	if m.idleWatchers[1].watcher == nil {
		t.Fatal("expected a watcher for the account with sync_minutes > 0")
	}
	if m.idleWatchers[2].watcher != nil {
		t.Fatal("expected no watcher for a manual-only (sync_minutes = 0) account")
	}
}

// TestStartIdleWatchersReconcilesInsteadOfRestarting guards against the
// one-second sync loop: startIdleWatchers runs on every AccountsLoadedMsg
// (which every sync completion emits), and restarting watchers there made each
// fresh watcher fire its gap-repair event → sync → account reload → restart →
// event → … forever. Unchanged accounts must keep their existing watcher.
func TestStartIdleWatchersReconcilesInsteadOfRestarting(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	cfg := config.DefaultConfig()
	cfg.Accounts = []config.AccountConfig{
		{Name: "Push", IMAPHost: "127.0.0.1", IMAPPort: 1, SyncMinutes: 5},
	}
	m := NewModel(database, cfg, "dev", false)
	m.accounts = []db.Account{{ID: 1, Name: "Push"}}
	m.mailboxes = []db.Mailbox{{ID: 10, AccountID: 1, Name: "INBOX"}}

	if cmd := m.startIdleWatchers(); cmd == nil {
		t.Fatal("expected the first reconcile to start a watcher")
	}
	defer m.stopIdleWatchers()
	first := m.idleWatchers[1].watcher

	// Same accounts again (what a routine AccountsLoadedMsg delivers): the
	// watcher must survive untouched and no new wait command may be issued.
	if cmd := m.startIdleWatchers(); cmd != nil {
		t.Fatal("expected no new wait commands when nothing changed")
	}
	if m.idleWatchers[1].watcher != first {
		t.Fatal("expected the unchanged account to keep its existing watcher")
	}
	select {
	case <-first.Done():
		t.Fatal("expected the unchanged account's watcher to stay running")
	default:
	}

	// A config change (edited credentials) must swap the watcher out.
	m.cfg.Accounts[0].Password = "rotated"
	if cmd := m.startIdleWatchers(); cmd == nil {
		t.Fatal("expected a changed account config to start a replacement watcher")
	}
	if m.idleWatchers[1].watcher == first {
		t.Fatal("expected a changed account config to replace the watcher")
	}
	select {
	case <-first.Done():
	default:
		t.Fatal("expected the replaced watcher to be closed")
	}
}
