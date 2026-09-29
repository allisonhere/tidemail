package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/db"
)

// withMailboxes gives the model n stored folders spread over two accounts.
func withMailboxes(t *testing.T, m Model, database *db.DB, n int) Model {
	t.Helper()
	a, err := database.AddAccount(fmt.Sprintf("acct-a%d", n), "A", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := database.AddAccount(fmt.Sprintf("acct-b%d", n), "B", "")
	if err != nil {
		t.Fatal(err)
	}
	m.mailboxes = nil
	for i := range n {
		acc := a
		if i%2 == 1 {
			acc = b
		}
		id, err := database.UpsertMailbox(db.Mailbox{AccountID: acc, Name: fmt.Sprintf("F%d-%d", n, i), Delimiter: "/"})
		if err != nil {
			t.Fatal(err)
		}
		m.mailboxes = append(m.mailboxes, db.Mailbox{ID: id, AccountID: acc, Name: fmt.Sprintf("F%d-%d", n, i)})
	}
	return m
}

func TestSyncAllConfirmsOnlyWithManyFolders(t *testing.T) {
	m, f := newNeedsYouModel(t)

	// A few folders: sync starts at once.
	m = withMailboxes(t, m, f.database, syncAllConfirmThreshold)
	got, cmd := press(t, m, "F")
	if got.overlay != overlayNone || cmd == nil {
		t.Fatalf("overlay %v, cmd nil = %v", got.overlay, cmd == nil)
	}

	// Many folders: ask first, with the count.
	m = withMailboxes(t, m, f.database, syncAllConfirmThreshold+5)
	m, cmd = press(t, m, "F")
	if m.overlay != overlaySyncAllConfirm || cmd != nil {
		t.Fatalf("overlay %v, cmd nil = %v", m.overlay, cmd == nil)
	}
	if view := strings.Join(strings.Fields(m.View()), " "); !strings.Contains(view, "Sync all 15 folders across 2 accounts?") || !strings.Contains(view, "take a while") {
		t.Fatal("the confirmation should give the count and the warning")
	}

	// Cancel: nothing runs.
	cancelled, cmd := press(t, m, "esc")
	if cancelled.overlay != overlayNone || cmd != nil {
		t.Fatal("cancelling must not sync")
	}

	// Confirm from the palette path too.
	next, _ := m.executeCommand("sync-all")
	m = next.(Model)
	if m.overlay != overlaySyncAllConfirm {
		t.Fatal("the palette command should ask as well")
	}
	m, cmd = press(t, m, "y")
	if m.overlay != overlayNone || cmd == nil {
		t.Fatal("confirming should start the sync")
	}
}
