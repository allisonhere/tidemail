package ui

import (
	"strings"
	"time"

	"github.com/allisonhere/tide/internal/db"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) saveDraftCmd(c ComposeModel) tea.Cmd {
	database := m.db
	draft := c.toDraftRecord()
	return func() tea.Msg {
		if database == nil {
			return DraftSavedMsg{DraftID: draft.ID}
		}
		now := time.Now()
		if draft.CreatedAt.IsZero() {
			draft.CreatedAt = now
		}
		draft.UpdatedAt = now
		id, err := database.SaveDraft(draft)
		return DraftSavedMsg{DraftID: id, Err: err}
	}
}

func (m *Model) deleteDraftCmd(id int64) tea.Cmd {
	database := m.db
	return func() tea.Msg {
		if id == 0 || database == nil {
			return DraftDeletedMsg{DraftID: id}
		}
		return DraftDeletedMsg{DraftID: id, Err: database.DeleteDraft(id)}
	}
}

func (m Model) closeComposeSavingDraft() (tea.Model, tea.Cmd) {
	if !m.compose.hasContent() {
		id := m.compose.draftID
		m.overlay = overlayNone
		m.compose = ComposeModel{}
		if id != 0 {
			return m, m.deleteDraftCmd(id)
		}
		return m, nil
	}
	cmd := m.saveDraftCmd(m.compose)
	m.overlay = overlayNone
	m.compose = ComposeModel{}
	m.setStatus("draft saved", false)
	return m, cmd
}

func (m Model) discardComposeDraft() (tea.Model, tea.Cmd) {
	id := m.compose.draftID
	m.overlay = overlayNone
	m.compose = ComposeModel{}
	if id != 0 {
		m.setStatus("draft discarded", false)
		return m, tea.Batch(m.deleteDraftCmd(id), m.clearStatusCmd())
	}
	return m, nil
}

func draftSubject(d db.Draft) string {
	if d.Subject != "" {
		return d.Subject
	}
	return "(no subject)"
}

func (m Model) selectedDraftsMailbox() bool {
	mb := m.selectedMailbox()
	if mb == nil {
		return false
	}
	return m.isDraftsMailbox(*mb)
}

func (m Model) isDraftsMailbox(mb db.Mailbox) bool {
	if hasFlag(mb.Flags, "\\Drafts") {
		return true
	}
	return isCommonDraftsName(mb.Name) || isCommonDraftsName(mb.DisplayName)
}

func (m Model) draftsSidebarCount(mb db.Mailbox) int64 {
	if m.db == nil {
		return 0
	}
	accountName, accountUser := m.draftAccountIdentity(mb.ID)
	local, _ := m.db.DraftCount(accountName, accountUser)
	remote, _ := m.db.CountMessages(mb.ID)
	return local + remote
}

func isCommonDraftsName(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	return name == "drafts" || strings.HasSuffix(name, "/drafts") || strings.HasSuffix(name, ".drafts")
}

func (m Model) draftAccountIdentity(mailboxID int64) (string, string) {
	mb := m.mailboxByID(mailboxID)
	if mb == nil {
		return "", ""
	}
	accountName := m.accountName(mb.AccountID)
	for _, acfg := range m.cfg.Accounts {
		if acfg.Name == accountName {
			return acfg.Name, acfg.User
		}
	}
	return accountName, ""
}

func (m *Model) loadDraftsCmd(mailboxID int64) tea.Cmd {
	database := m.db
	accountName, accountUser := m.draftAccountIdentity(mailboxID)
	return func() tea.Msg {
		if database == nil {
			return DraftsLoadedMsg{MailboxID: mailboxID}
		}
		drafts, err := database.ListDrafts(accountName, accountUser)
		return DraftsLoadedMsg{MailboxID: mailboxID, Drafts: drafts, Err: err}
	}
}
