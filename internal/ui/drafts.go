package ui

import (
	"context"
	"strings"
	"time"

	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	imapClient "github.com/allisonhere/tide/internal/imap"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) saveDraftCmd(c ComposeModel) tea.Cmd {
	database := m.db
	draft := c.toDraftRecord()
	return func() tea.Msg {
		if database == nil {
			return DraftSavedMsg{DraftID: draft.ID}
		}
		// CreatedAt stays zero here: SaveDraft stamps inserts and preserves the
		// stored creation time (and remote linkage) on updates.
		draft.UpdatedAt = time.Now()
		id, err := database.SaveDraft(draft)
		return DraftSavedMsg{DraftID: id, Err: err}
	}
}

func (m *Model) deleteDraftCmd(id int64) tea.Cmd {
	database := m.db
	if id == 0 || database == nil {
		return func() tea.Msg { return DraftDeletedMsg{DraftID: id} }
	}
	// A draft mirrored from the server also has a remote original; delete that
	// first (remote-first, like message deletes) or it re-imports on next sync.
	var remote *db.Mailbox
	var remoteCfg config.AccountConfig
	var remoteUID uint32
	if draft, err := database.GetDraft(id); err == nil && draft.RemoteUID != 0 && draft.MailboxID != 0 {
		if mb := m.mailboxByID(draft.MailboxID); mb != nil {
			remote = mb
			remoteCfg = m.accountCfgForMailbox(draft.MailboxID)
			remoteUID = draft.RemoteUID
		}
	}
	return func() tea.Msg {
		if remote != nil && remoteCfg.IMAPHost != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			client := imapClient.New(remoteCfg)
			if err := client.Connect(ctx); err != nil {
				return DraftDeletedMsg{DraftID: id, Err: err}
			}
			defer client.Close()
			if err := client.DeleteMessages(ctx, remote.Name, []uint32{remoteUID}); err != nil {
				return DraftDeletedMsg{DraftID: id, Err: err}
			}
			if err := database.DeleteMessageByUID(remote.ID, remoteUID); err != nil {
				return DraftDeletedMsg{DraftID: id, Err: err}
			}
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
	// Only count remote drafts that haven't been mirrored into the drafts
	// table yet, so a synced mailbox isn't counted twice.
	remote, _ := m.db.UnmirroredDraftMessageCount(mb.ID)
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

// importRemoteDraftsCmd mirrors a drafts mailbox's synced messages into the
// drafts table so server-side drafts (e.g. written in webmail) can be opened,
// edited, and sent locally. Already-mirrored messages (matched by Message-ID,
// else mailbox+UID) are skipped, so locally-edited copies are never clobbered.
// Returns a refreshed DraftsLoadedMsg for the mailbox.
func (m *Model) importRemoteDraftsCmd(mailboxID int64) tea.Cmd {
	database := m.db
	accountName, accountUser := m.draftAccountIdentity(mailboxID)
	accountIndex := 0
	for i, acfg := range m.cfg.Accounts {
		if acfg.Name == accountName && acfg.User == accountUser {
			accountIndex = i
			break
		}
	}
	return func() tea.Msg {
		if database == nil {
			return DraftsLoadedMsg{MailboxID: mailboxID}
		}
		msgs, err := database.ListMessages(mailboxID)
		if err != nil {
			return DraftsLoadedMsg{MailboxID: mailboxID, Err: err}
		}
		for _, msg := range msgs {
			mirrored, err := database.HasDraftForRemote(msg.MessageID, mailboxID, msg.UID)
			if err != nil {
				return DraftsLoadedMsg{MailboxID: mailboxID, Err: err}
			}
			if mirrored {
				continue
			}
			draft := db.Draft{
				AccountName:     accountName,
				AccountUser:     accountUser,
				AccountIndex:    accountIndex,
				MailboxID:       mailboxID,
				RemoteUID:       msg.UID,
				RemoteMessageID: msg.MessageID,
				To:              msg.To,
				CC:              msg.CC,
				Subject:         msg.Subject,
				BodyText:        draftBodyText(msg),
				CreatedAt:       msg.Date,
				UpdatedAt:       msg.Date,
				LastRemoteSync:  time.Now(),
			}
			if _, err := database.SaveDraft(draft); err != nil {
				return DraftsLoadedMsg{MailboxID: mailboxID, Err: err}
			}
		}
		drafts, err := database.ListDrafts(accountName, accountUser)
		return DraftsLoadedMsg{MailboxID: mailboxID, Drafts: drafts, Err: err}
	}
}

// draftBodyText returns an editable plain-text body for a remote draft,
// converting HTML-only drafts (e.g. composed in webmail) to markdown.
func draftBodyText(msg db.Message) string {
	if strings.TrimSpace(msg.BodyText) != "" {
		return msg.BodyText
	}
	if msg.BodyHTML == "" {
		return ""
	}
	markdown, err := md.NewConverter("", true, nil).ConvertString(msg.BodyHTML)
	if err != nil {
		return ""
	}
	return markdown
}
