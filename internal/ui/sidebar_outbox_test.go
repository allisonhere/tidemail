package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tidemail/internal/db"
)

// The Outbox used to be reachable only by knowing the "O" shortcut, so a
// message stuck in it was invisible to anyone who did not — which is how a
// failed send sat unnoticed for a day and a half. It has a row of its own now.

func newSidebarOutboxModel(t *testing.T, items ...db.OutboxItem) Model {
	t.Helper()
	m := newStatusBarModel(t, items...)
	m.accounts = []db.Account{{ID: 1, Name: "Personal"}}
	m.mailboxes = []db.Mailbox{{ID: 10, AccountID: 1, Name: "INBOX", DisplayName: "Inbox"}}
	m.sidebarRows = buildSidebarRows(m.accounts, m.mailboxes, map[int64]bool{}, map[string]bool{})
	m.focused = paneAccounts
	return m
}

// outboxRowIndex is where the Outbox sits in the sidebar.
func outboxRowIndex(t *testing.T, m Model) int {
	t.Helper()
	for i, row := range m.sidebarRows {
		if row.kind == rowKindOutbox {
			return i
		}
	}
	t.Fatal("no Outbox row in the sidebar")
	return -1
}

func TestSidebarShowsAnOutboxRow(t *testing.T) {
	m := newSidebarOutboxModel(t)

	if got := ansi.Strip(m.renderAccountsPane()); !strings.Contains(got, "Outbox") {
		t.Fatalf("expected an Outbox row in the sidebar, got %q", got)
	}
	// Beside the Unified Inbox: both span accounts, one in each direction.
	if i := outboxRowIndex(t, m); m.sidebarRows[i-1].kind != rowKindUnified {
		t.Fatalf("expected the Outbox to follow the Unified Inbox, got %v", m.sidebarRows)
	}
}

// With no accounts there is no Unified Inbox either; an empty sidebar stays
// empty rather than showing a lone Outbox.
func TestSidebarHasNoOutboxRowWithoutAccounts(t *testing.T) {
	rows := buildSidebarRows(nil, nil, map[int64]bool{}, map[string]bool{})
	for _, row := range rows {
		if row.kind == rowKindOutbox {
			t.Fatal("expected no Outbox row without accounts")
		}
	}
}

// The badge counts what needs a person, not what is merely in flight.
func TestSidebarOutboxBadgeCountsOnlyTrouble(t *testing.T) {
	for _, tc := range []struct {
		what  string
		items []db.OutboxItem
		badge string
	}{
		{"nothing", nil, ""},
		{"a send in its undo window", []db.OutboxItem{outboxItem(db.OutboxQueued, 0)}, ""},
		{"already delivered", []db.OutboxItem{outboxItem(db.OutboxSent, 1)}, ""},
		{"one failed", []db.OutboxItem{agedOutboxItem(db.OutboxFailed, 3, time.Hour)}, "(1)"},
		{
			"failed plus unconfirmed",
			[]db.OutboxItem{outboxItem(db.OutboxFailed, 3), outboxItem(db.OutboxUncertain, 1)},
			"(2)",
		},
	} {
		m := newSidebarOutboxModel(t, tc.items...)
		row := ansi.Strip(m.renderOutboxRow(false, 40))
		if tc.badge == "" {
			if strings.Contains(row, "(") {
				t.Fatalf("%s: expected no badge, got %q", tc.what, row)
			}
			continue
		}
		if !strings.Contains(row, tc.badge) {
			t.Fatalf("%s: expected badge %s, got %q", tc.what, tc.badge, row)
		}
	}
}

// A failed send is the thing worth noticing, so the badge is in the error
// color rather than the ordinary unread blue.
func TestSidebarOutboxBadgeUsesTheErrorColor(t *testing.T) {
	trueColor(t)

	m := newSidebarOutboxModel(t, outboxItem(db.OutboxFailed, 3))
	if got := m.renderOutboxRow(false, 40); !strings.Contains(got, foreground(t, m.styles.Theme.Error)) {
		t.Fatalf("expected the badge in the error color, got %q", got)
	}
}

// Enter and Space open it. The row is not a folder, so neither may fall
// through to the folder-sync path.
func TestSidebarOutboxRowOpensOnEnterAndSpace(t *testing.T) {
	for _, keyMsg := range []tea.KeyMsg{
		{Type: tea.KeyEnter},
		{Type: tea.KeySpace},
	} {
		m := newSidebarOutboxModel(t)
		m.sidebarCursor = outboxRowIndex(t, m)

		if !m.selectedOutboxRow() {
			t.Fatal("expected the cursor to be on the Outbox row")
		}
		if m.selectedMailbox() != nil {
			t.Fatal("the Outbox row must not read as a mailbox")
		}

		next, _ := m.Update(keyMsg)
		if got := next.(Model).overlay; got != overlayOutbox {
			t.Fatalf("%v: expected the Outbox overlay to open, got %v", keyMsg.Type, got)
		}
	}
}

// Rebuilding the sidebar — every sync does it — must not slide the cursor off
// the Outbox onto a folder.
func TestSidebarOutboxSelectionSurvivesARebuild(t *testing.T) {
	m := newSidebarOutboxModel(t)
	m.sidebarCursor = outboxRowIndex(t, m)

	kind, id := m.currentSidebarSelection()
	if kind != rowKindOutbox {
		t.Fatalf("currentSidebarSelection = %v, want the Outbox row", kind)
	}

	m.mailboxes = append(m.mailboxes, db.Mailbox{ID: 11, AccountID: 1, Name: "Archive", DisplayName: "Archive"})
	m.sidebarRows = buildSidebarRows(m.accounts, m.mailboxes, map[int64]bool{}, map[string]bool{})
	m.sidebarCursor = 0
	m.restoreSidebarSelection(kind, id)

	if !m.selectedOutboxRow() {
		t.Fatalf("expected the cursor back on the Outbox row, got row %d", m.sidebarCursor)
	}
}
