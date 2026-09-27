package ui

// Sync all: syncs every folder of every account. With many folders that is
// many IMAP round trips, so above a threshold TideMail asks first.

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// syncAllConfirmThreshold is the folder count above which sync all asks
// before starting.
const syncAllConfirmThreshold = 10

// requestSyncAll syncs every folder, asking first when there are many.
func (m Model) requestSyncAll() (tea.Model, tea.Cmd) {
	if len(m.mailboxes) == 0 {
		return m, nil
	}
	if len(m.mailboxes) > syncAllConfirmThreshold {
		m.overlay = overlaySyncAllConfirm
		return m, nil
	}
	return m, m.syncAllCmd()
}

func (m Model) syncAllCmd() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(m.mailboxes))
	for _, mb := range m.mailboxes {
		cmds = append(cmds, m.syncMailboxCmd(mb.ID, true))
	}
	return tea.Batch(cmds...)
}

func (m Model) handleSyncAllConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case keyMatches(msg, m.keys.Yes), keyMatches(msg, m.keys.Confirm):
		m.overlay = overlayNone
		return m, m.syncAllCmd()
	case keyMatches(msg, m.keys.No), keyMatches(msg, m.keys.Cancel):
		m.overlay = overlayNone
	}
	return m, nil
}

// syncAllConfirmText explains what sync all is about to do.
func (m Model) syncAllConfirmText() string {
	accounts := map[int64]bool{}
	for _, mb := range m.mailboxes {
		accounts[mb.AccountID] = true
	}
	noun := "accounts"
	if len(accounts) == 1 {
		noun = "account"
	}
	return fmt.Sprintf("Sync all %d folders across %d %s?\n\nWith this many folders it can take a while. Mail stays usable meanwhile.",
		len(m.mailboxes), len(accounts), noun)
}
