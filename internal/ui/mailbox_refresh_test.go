package ui

import (
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
)

// TestMailboxesRefreshedMergesNewFolders verifies the folder-refresh result
// handler adopts newly discovered mailboxes into the in-memory list and rebuilds
// the sidebar so they become visible without a restart.
func TestMailboxesRefreshedMergesNewFolders(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(nil, cfg, "dev", false)
	m.accounts = []db.Account{{ID: 1, Name: "Personal"}}
	m.mailboxes = []db.Mailbox{{ID: 10, AccountID: 1, Name: "INBOX"}}
	m.rebuildSidebar()
	before := len(m.sidebarRows)

	next, _ := m.Update(MailboxesRefreshedMsg{
		AccountID: 1,
		Mailboxes: []db.Mailbox{{ID: 11, AccountID: 1, Name: "Receipts", DisplayName: "Receipts"}},
	})
	m = next.(Model)

	if len(m.mailboxes) != 2 {
		t.Fatalf("expected 2 mailboxes after refresh, got %d", len(m.mailboxes))
	}
	found := false
	for _, mb := range m.mailboxes {
		if mb.Name == "Receipts" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected newly discovered Receipts folder to be merged in")
	}
	if len(m.sidebarRows) <= before {
		t.Fatalf("expected sidebar to grow after merging a folder, was %d now %d", before, len(m.sidebarRows))
	}
}

// TestPrunableMailboxIDsGuards verifies the prune decision: folders gone from
// the server are pruned, but INBOX is protected and an empty server set (a
// likely transient LIST fault) prunes nothing.
func TestPrunableMailboxIDsGuards(t *testing.T) {
	existing := []db.Mailbox{
		{ID: 1, Name: "INBOX"},
		{ID: 2, Name: "Receipts"},
		{ID: 3, Name: "Travel"},
	}

	t.Run("prunes folders absent server-side, keeps present and INBOX", func(t *testing.T) {
		server := map[string]bool{"INBOX": true, "Travel": true}
		got := prunableMailboxIDs(existing, server)
		if len(got) != 1 || got[0] != 2 {
			t.Fatalf("expected only Receipts (id 2) pruned, got %v", got)
		}
	})

	t.Run("empty server set prunes nothing", func(t *testing.T) {
		if got := prunableMailboxIDs(existing, map[string]bool{}); got != nil {
			t.Fatalf("expected no pruning on empty server set, got %v", got)
		}
	})

	t.Run("INBOX missing from server is never pruned", func(t *testing.T) {
		server := map[string]bool{"Receipts": true, "Travel": true}
		for _, id := range prunableMailboxIDs(existing, server) {
			if id == 1 {
				t.Fatal("INBOX must never be pruned")
			}
		}
	})
}

// TestMailboxesRefreshedPrunesAndClearsActiveFolder verifies the handler drops
// pruned folders from the in-memory list, rebuilds the sidebar, and clears the
// content pane when the folder being viewed is the one pruned.
func TestMailboxesRefreshedPrunesAndClearsActiveFolder(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(nil, cfg, "dev", false)
	m.accounts = []db.Account{{ID: 1, Name: "Personal"}}
	m.mailboxes = []db.Mailbox{
		{ID: 10, AccountID: 1, Name: "INBOX"},
		{ID: 11, AccountID: 1, Name: "Receipts", DisplayName: "Receipts"},
	}
	m.rebuildSidebar()
	// Park the cursor on the Receipts row so it is the active folder.
	for i, row := range m.sidebarRows {
		if row.kind == rowKindMailbox && row.mailboxID == 11 {
			m.sidebarCursor = i
		}
	}
	m.messages = []db.Message{{ID: 99, MailboxID: 11, Subject: "old"}}
	m.filteredMessages = m.messages

	next, _ := m.Update(MailboxesRefreshedMsg{AccountID: 1, Removed: []int64{11}})
	m = next.(Model)

	if len(m.mailboxes) != 1 || m.mailboxes[0].ID != 10 {
		t.Fatalf("expected only INBOX to remain, got %+v", m.mailboxes)
	}
	if m.messages != nil || m.filteredMessages != nil {
		t.Fatal("expected content pane cleared after the active folder was pruned")
	}
}

// TestMailboxesRefreshedPruneClearsSyncingState verifies pruning a folder that
// is mid-sync drops its spinner entry. syncing is keyed by mailbox ID and only
// cleared on MailboxSyncedMsg, which can never arrive for a deleted mailbox, so
// without this the status-line spinner (len(syncing) > 0) would leak forever.
func TestMailboxesRefreshedPruneClearsSyncingState(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(nil, cfg, "dev", false)
	m.accounts = []db.Account{{ID: 1, Name: "Personal"}}
	m.mailboxes = []db.Mailbox{
		{ID: 10, AccountID: 1, Name: "INBOX"},
		{ID: 11, AccountID: 1, Name: "Receipts", DisplayName: "Receipts"},
	}
	m.rebuildSidebar()
	m.syncing[11] = true

	next, _ := m.Update(MailboxesRefreshedMsg{AccountID: 1, Removed: []int64{11}})
	m = next.(Model)

	if m.syncing[11] {
		t.Fatal("expected syncing entry for the pruned mailbox to be cleared")
	}
	if len(m.syncing) != 0 {
		t.Fatalf("expected no leaked syncing entries, got %d", len(m.syncing))
	}
}

// TestMailboxesRefreshedErrorIsNonFatal verifies a failed folder LIST leaves the
// mailbox list untouched and does not hijack the status line — folder refresh is
// a convenience that must never disrupt routine auto-sync.
func TestMailboxesRefreshedErrorIsNonFatal(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(nil, cfg, "dev", false)
	m.accounts = []db.Account{{ID: 1, Name: "Personal"}}
	m.mailboxes = []db.Mailbox{{ID: 10, AccountID: 1, Name: "INBOX"}}

	next, _ := m.Update(MailboxesRefreshedMsg{AccountID: 1, Err: errTest})
	m = next.(Model)

	if len(m.mailboxes) != 1 {
		t.Fatalf("expected mailbox list unchanged on error, got %d", len(m.mailboxes))
	}
	if m.statusMsg != "" {
		t.Fatalf("expected status line untouched on non-fatal folder error, got %q", m.statusMsg)
	}
}

var errTest = errTestType("list mailboxes: boom")

type errTestType string

func (e errTestType) Error() string { return string(e) }
