package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
)

func TestSyncRevokedTokenShowsStickyStatus(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	accountID, _ := database.AddAccount("gmail", "")
	inboxID, _ := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "INBOX"})

	m := NewModel(database, config.DefaultConfig(), "dev", false)
	next, _ := m.Update(AccountsLoadedMsg{
		Accounts:  []db.Account{{ID: accountID, Name: "gmail"}},
		Mailboxes: []db.Mailbox{{ID: inboxID, AccountID: accountID, Name: "INBOX"}},
	})
	m = next.(Model)

	revoked := errors.New(`oauth2 refresh: auth: refresh token: oauth2: "invalid_grant" "Token has been expired or revoked."`)
	next, cmd := m.Update(MailboxSyncedMsg{MailboxID: inboxID, Err: revoked})
	m = next.(Model)

	if !m.statusErr {
		t.Fatalf("expected statusErr true")
	}
	if !strings.Contains(m.statusMsg, "re-authenticate") || !strings.Contains(m.statusMsg, "gmail") {
		t.Fatalf("unexpected status message: %q", m.statusMsg)
	}
	if cmd != nil {
		t.Fatalf("expected nil cmd (sticky status, no auto-clear)")
	}
}

func TestSyncGenericErrorStillClears(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	accountID, _ := database.AddAccount("gmail", "")
	inboxID, _ := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "INBOX"})
	m := NewModel(database, config.DefaultConfig(), "dev", false)
	next, _ := m.Update(AccountsLoadedMsg{
		Accounts:  []db.Account{{ID: accountID, Name: "gmail"}},
		Mailboxes: []db.Mailbox{{ID: inboxID, AccountID: accountID, Name: "INBOX"}},
	})
	m = next.(Model)

	next, cmd := m.Update(MailboxSyncedMsg{MailboxID: inboxID, Err: errors.New("connection refused")})
	m = next.(Model)

	if cmd == nil {
		t.Fatalf("expected a clear-status command for a transient error")
	}
	if !strings.Contains(m.statusMsg, "sync failed") {
		t.Fatalf("unexpected status message: %q", m.statusMsg)
	}
}
