package ui

import (
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func filterManagerModel(t *testing.T) Model {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	accountID, _ := database.AddAccount("Personal", "")
	inboxID, _ := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "INBOX", Delimiter: "/"})
	_ = inboxID
	m := NewModel(database, config.DefaultConfig(), "dev", false)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)
	next, _ = m.Update(AccountsLoadedMsg{
		Accounts:  []db.Account{{ID: accountID, Name: "Personal"}},
		Mailboxes: mustListMailboxes(t, database, accountID),
	})
	m = next.(Model)
	m.filterManager = m.newFilterManager()
	m.overlay = overlayFilterManager
	return m
}

func assertUniformWidth(t *testing.T, m Model) {
	t.Helper()
	for i, line := range strings.Split(m.View(), "\n") {
		if w := lipgloss.Width(line); w != m.width {
			t.Fatalf("view line %d width=%d, want %d (transparent gap / bleed)", i, w, m.width)
		}
	}
}

// A background sync must not dismiss the filter manager overlay.
func TestFilterManagerSurvivesSync(t *testing.T) {
	m := filterManagerModel(t)
	inbox := m.mailboxes[0]
	next, _ := m.Update(MailboxSyncedMsg{MailboxID: inbox.ID, Manual: false})
	m = next.(Model)
	if m.overlay != overlayFilterManager {
		t.Fatalf("expected filter manager to stay open after sync, overlay=%v", m.overlay)
	}
}

// Every modal mode (and a non-empty status line) must render with no transparent
// gaps, so an underlying repaint can never bleed through the modal.
func TestFilterManagerRendersOpaque(t *testing.T) {
	m := filterManagerModel(t)
	assertUniformWidth(t, m) // list mode, empty status

	m.filterManager.status = "applied to 3 of 5 matched"
	assertUniformWidth(t, m) // list mode, with status

	m.filterManager.mode = fmInput
	assertUniformWidth(t, m)

	m.filterManager.mode = fmReview
	m.filterManager.draftEn = "move newsletters from substack to Reading"
	assertUniformWidth(t, m)
}
