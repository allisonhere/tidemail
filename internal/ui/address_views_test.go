package ui

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/mailapp"
	"github.com/allisonhere/tidemail/internal/smtp"
)

func TestAliasSenderSurvivesDraftAndOutbox(t *testing.T) {
	account := config.AccountConfig{ID: "icloud", Name: "iCloud", User: "primary@icloud.com", From: "Primary <primary@icloud.com>", Aliases: "Work <work@example.com>"}
	c := NewCompose(account, []config.AccountConfig{account}, nil)
	if c.focusedField != composeFieldFrom {
		t.Fatal("single account with alias should focus From")
	}
	c.chooseSender(1)
	if got := c.selectedAccount().From; got != "Work <work@example.com>" {
		t.Fatalf("selected sender = %q", got)
	}
	reply := NewReply(db.Message{From: "person@example.com", To: "work@example.com"}, account, []config.AccountConfig{account}, nil)
	if got := reply.selectedAccount().From; got != "Work <work@example.com>" {
		t.Fatalf("reply sender = %q", got)
	}
	draft := c.toDraftRecord()
	draft.To = "person@example.com"
	prepared, err := mailapp.PrepareCompose(c.selectedAccount(), draft)
	if err != nil || prepared.From != "Work <work@example.com>" {
		t.Fatalf("prepared From = %q, err = %v", prepared.From, err)
	}
	if got := NewComposeFromDraft(draft, []config.AccountConfig{account}, nil).selectedAccount().From; got != "Work <work@example.com>" {
		t.Fatalf("draft sender = %q", got)
	}
	message, _ := json.Marshal(smtp.OutgoingMessage{From: "Work <work@example.com>"})
	draftJSON, _ := json.Marshal(draft)
	queued, err := outboxMessage(db.OutboxItem{MessageJSON: message, DraftJSON: draftJSON}, account)
	if err != nil || queued.From != "Work <work@example.com>" {
		t.Fatalf("retry sender = %q, err = %v", queued.From, err)
	}
	account.Aliases = ""
	queued, err = outboxMessage(db.OutboxItem{MessageJSON: message, DraftJSON: draftJSON}, account)
	if err == nil {
		t.Fatalf("removed alias retry should stop instead of sending from %q", queued.From)
	}
}

func TestAddressViewSidebarIsManual(t *testing.T) {
	account := config.AccountConfig{ID: "icloud", Name: "iCloud", Aliases: "work@example.com", AddressViews: "work@example.com"}
	rows := buildSidebarRows([]db.Account{{ID: 1, ConfigID: "icloud", Name: "iCloud"}}, nil, nil, nil, folderView{configs: []config.AccountConfig{account}})
	var found int
	for _, row := range rows {
		if row.kind == rowKindAddressView {
			found++
			if row.accountID != 1 || row.label != "work@example.com" {
				t.Fatalf("view = %+v", row)
			}
		}
	}
	if found != 1 {
		t.Fatalf("views = %d", found)
	}
	account.AddressViews = ""
	rows = buildSidebarRows([]db.Account{{ID: 1, ConfigID: "icloud", Name: "iCloud"}}, nil, nil, nil, folderView{configs: []config.AccountConfig{account}})
	for _, row := range rows {
		if row.kind == rowKindAddressView {
			t.Fatal("alias created an automatic view")
		}
	}
}

func TestAccountFormAddressFields(t *testing.T) {
	am := NewAccountManager(nil)
	am.populateFormFrom(config.AccountConfig{ID: "icloud", Aliases: "Work <work@example.com>", AddressViews: "work@example.com"})
	got := am.buildCfg()
	if got.Aliases != "Work <work@example.com>" || got.AddressViews != "work@example.com" {
		t.Fatalf("form = %+v", got)
	}
	am.aliasesInput.SetValue("invalid")
	if failure := am.validateForm(am.buildCfg()); failure.field != amFieldAliases {
		t.Fatalf("invalid alias accepted: %+v", failure)
	}
}

func TestAddressViewLoadsInModel(t *testing.T) {
	database, err := db.OpenPath(filepath.Join(t.TempDir(), "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	accountID, err := database.AddAccount("icloud", "iCloud", "")
	if err != nil {
		t.Fatal(err)
	}
	inboxID, err := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "INBOX"})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpsertMessage(db.Message{MailboxID: inboxID, UID: 1, To: "work@example.com", Subject: "Work"}); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Accounts = []config.AccountConfig{{ID: "icloud", Name: "iCloud", User: "primary@icloud.com", AddressViews: "work@example.com"}}
	m := NewModel(database, cfg, "dev", false)
	t.Cleanup(m.CloseSessions)
	m.accounts, _ = database.ListAccounts()
	m.mailboxes, _ = database.ListMailboxes(accountID)
	m.rebuildSidebar()
	for i, row := range m.sidebarRows {
		if row.kind == rowKindAddressView {
			m.sidebarCursor = i
			break
		}
	}
	if m.selectedAddressView() == nil {
		t.Fatal("view missing from sidebar")
	}
	loaded := m.loadAddressViewCmd()().(MessagesLoadedMsg)
	next, _ := m.Update(loaded)
	m = next.(Model)
	if len(m.messages) != 1 || m.messages[0].Subject != "Work" {
		t.Fatalf("visible messages = %+v", m.messages)
	}
}
