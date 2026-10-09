package ui

import (
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
)

// A manual-only account (sync_minutes = -1) must not fetch mail on launch; the
// guide promises it refreshes only on an explicit request.
func TestLaunchSyncSkipsManualOnlyAccounts(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Accounts = []config.AccountConfig{
		{Name: "Auto", IMAPHost: "imap.example.com", SyncMinutes: 15},
		{Name: "Manual", IMAPHost: "imap.example.com", SyncMinutes: -1},
	}
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	var inbox [2]int64
	m := NewModel(database, cfg, "dev", false)
	for i, name := range []string{"Auto", "Manual"} {
		accID, err := database.AddAccount("", name, "")
		if err != nil {
			t.Fatal(err)
		}
		inbox[i], err = database.UpsertMailbox(db.Mailbox{AccountID: accID, Name: "INBOX", Delimiter: "/"})
		if err != nil {
			t.Fatal(err)
		}
		m.accounts = append(m.accounts, db.Account{ID: accID, Name: name})
		m.mailboxes = append(m.mailboxes, db.Mailbox{ID: inbox[i], AccountID: accID, Name: "INBOX"})
	}
	m.syncInboxesNowCmd()
	if !m.syncing[inbox[0]] {
		t.Fatal("the polling account's inbox should sync at launch")
	}
	if m.syncing[inbox[1]] {
		t.Fatal("the manual-only account's inbox must not sync at launch")
	}

	m.syncing = map[int64]bool{}
	m.startSyncTimers()
	if m.syncing[inbox[1]] {
		t.Fatal("startSyncTimers must not sync a manual-only account either")
	}
}
