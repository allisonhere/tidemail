package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	imapClient "github.com/allisonhere/tide/internal/imap"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type remoteDeleteAction int

const (
	remoteDeleteExpunge remoteDeleteAction = iota
	remoteDeleteMoveToTrash
)

func (m Model) renderMessagesPane() string {
	w := m.articlesPaneWidth()
	h := m.articlesPaneContentHeight()
	msgUnread, msgRead, msgSelected, headerActive, borderColor, borderFocus := m.messageRowStyles()

	rows := []string{}
	if m.selectedDraftsMailbox() {
		end := min(m.listOffset+m.articleRowsVisible(), len(m.drafts))
		for i := m.listOffset; i < end; i++ {
			draft := m.drafts[i]
			age := m.formatTime(draft.UpdatedAt)
			style := msgRead
			if i == m.messageCursor {
				style = msgSelected
			}
			prefix := "✎ "
			if !m.iconsEnabled() {
				prefix = "d "
			}
			rows = append(rows, style.Width(w).Render(renderArticleRow(prefix, unescapeDisplayText(draftSubject(draft)), age, w)))
		}
		if len(m.drafts) == 0 {
			rows = append(rows, msgRead.Render("  no drafts"))
		}
	} else {
		rowCount := m.activeMessageRowCount()
		end := min(m.listOffset+m.articleRowsVisible(), rowCount)
		for i := m.listOffset; i < end; i++ {
			msg2 := m.filteredMessages[i]
			threadCount := 1
			threadUnread := 0
			var thread messageThread
			if m.threadedMessagesEnabled() {
				thread = m.messageThreads[i]
				msg2 = thread.Representative
				threadCount = thread.Count
				threadUnread = thread.UnreadCount
			} else if !msg2.Read {
				threadUnread = 1
			}
			age := m.formatTime(msg2.Date)
			style := msgRead
			if threadUnread > 0 {
				style = msgUnread
			}
			dot := m.messageRowPrefix(threadUnread == 0)
			if m.messageRowSelected(msg2, thread) {
				dot = "✓ "
				// When selected but not the cursor, use green checkmark + text
				if i != m.messageCursor {
					selFg := lipgloss.Color("#a6e3a1")
					style = style.Foreground(selFg)
				}
			}
			if i == m.messageCursor {
				style = msgSelected
			}
			subject := m.messageRowTitle(msg2)
			if threadCount > 1 {
				subject = fmt.Sprintf("%s (%d)", subject, threadCount)
			}
			if m.cfg.Display.ShowSender {
				senderW := min(22, max(0, w/3))
				rows = append(rows, style.Width(w).Render(renderArticleRowWithSender(dot, senderDisplay(msg2.From), subject, age, w, senderW)))
			} else {
				rows = append(rows, style.Width(w).Render(renderArticleRow(dot, subject, age, w)))
			}
		}

		if len(m.filteredMessages) == 0 {
			if m.searchMode {
				rows = append(rows, msgRead.Render("  no results"))
			} else {
				rows = append(rows, msgRead.Render("  no messages"))
			}
		}
	}

	focused := m.focused == paneMessages
	title := "Messages"
	if m.selectedUnifiedInbox() {
		title = "Unified Inbox"
	}
	if m.searchMode {
		if m.searchQuery != "" {
			title = fmt.Sprintf("Search: %s", m.searchQuery)
		} else {
			title = "Search:"
		}
	}
	if m.showUnreadOnly {
		title += " (unread)"
	}
	// Surface an active multi-selection so it's clear a bulk action (d/a/m/x)
	// will apply to those messages, not just the focused row.
	if n := len(m.selectedMessages); n > 0 {
		title += fmt.Sprintf("  ✓ %d selected", n)
	}

	var headerLine string
	if m.searchMode {
		// Dark badge fills from text through the gap to the hint area.
		badgeStyle := lipgloss.NewStyle().
			Background(lipgloss.Color("#2a2a2a")).
			Foreground(lipgloss.Color("#ffffff")).
			Bold(true)
		badgeText := "> " + m.headerLabel(title)
		badgeW := lipgloss.Width(badgeText)
		hint := m.renderPaneHint(paneMessages)
		hintW := lipgloss.Width(hint)
		gap := max(0, w-badgeW-hintW)
		left := badgeStyle.Width(badgeW + gap).Render(badgeText)
		headerLine = m.styles.PaneHeaderInactive.Width(w).Render(left + hint)
	} else {
		headerLine = m.renderPaneHeaderWithAccent(paneMessages, title, focused, w, headerActive)
	}

	contentRows := append([]string{headerLine}, rows...)
	for viewLineCount(contentRows) < h {
		contentRows = append(contentRows, msgRead.Width(w).Render(""))
	}

	bg := m.styles.Theme.Bg
	content := clampView(strings.Join(contentRows, "\n"), w, h, bg)
	return lipgloss.NewStyle().
		Background(bg).
		Border(lipPaneBorder(m.styles.PlainUI), false, false, true, false).
		BorderForeground(lipgloss.Color(func() string {
			if focused || m.searchMode {
				return string(borderFocus)
			}
			return string(borderColor)
		}())).
		BorderBackground(bg).
		Width(w).Height(h).
		Render(content)
}

func (m Model) messageRowStyles() (lipgloss.Style, lipgloss.Style, lipgloss.Style, lipgloss.Style, lipgloss.Color, lipgloss.Color) {
	accent := m.selectedMailboxAccountColor()
	unread := m.styles.ArticleUnread
	read := m.styles.ArticleRead
	selected := m.styles.ArticleSelected
	headerActive := m.styles.PaneHeaderActive
	border := m.styles.Theme.Border
	borderFocus := m.styles.Theme.BorderFocus
	if accent != "" {
		unreadBg := terminalColorAsColor(unread.GetBackground())
		if unreadBg == "" {
			unreadBg = m.styles.Theme.Bg
		}
		leg := accentReadableOn(accent, unreadBg, 3)
		unread = unread.Foreground(leg)
		headerActive = headerActive.
			Background(accent).
			Foreground(readableText(m.styles.Theme.Fg, accent, 4.5))
		borderFocus = accent
	}
	return unread, read, selected, headerActive, border, borderFocus
}

func (m *Model) clearSelection() {
	m.selectedMessages = make(map[int64]bool)
}

func (m Model) hasSelection() bool {
	return len(m.selectedMessages) > 0
}

func (m *Model) applyFilter() {
	if m.searchActive() && !m.showUnreadOnly {
		m.filteredMessages = m.messages
		m.rebuildMessageThreads()
		return
	}
	q := strings.ToLower(m.searchQuery)
	if q == "" && !m.showUnreadOnly {
		m.filteredMessages = m.messages
		m.rebuildMessageThreads()
		return
	}
	filtered := make([]db.Message, 0, len(m.messages))
	for _, msg := range m.messages {
		if m.showUnreadOnly && msg.Read {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(msg.Subject), q) {
			continue
		}
		filtered = append(filtered, msg)
	}
	m.filteredMessages = filtered
	m.rebuildMessageThreads()
}

func (m Model) messageRowTitle(msg db.Message) string {
	subject := unescapeDisplayText(msg.Subject)
	if !m.searchActive() {
		return subject
	}
	context := strings.TrimSpace(msg.AccountName)
	if name := strings.TrimSpace(msg.MailboxName); name != "" {
		if context != "" {
			context += " / " + name
		} else {
			context = name
		}
	}
	if context == "" {
		return subject
	}
	return subject + " [" + context + "]"
}

func (m *Model) setMessageReadCmd(msg db.Message, read, advance bool) tea.Cmd {
	database := m.db
	sessions := m.sessions
	mailbox := m.mailboxByID(msg.MailboxID)
	acfg := m.accountCfgForMailbox(msg.MailboxID)
	return func() tea.Msg {
		if mailbox != nil && acfg.IMAPHost != "" && msg.UID != 0 {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := sessions.Do(ctx, acfg, func(client *imapClient.Client) error {
				return client.MarkSeen(ctx, mailbox.Name, msg.UID, read)
			}); err != nil {
				return MessageReadUpdatedMsg{MessageID: msg.ID, MailboxID: msg.MailboxID, WasRead: msg.Read, Read: read, Advance: advance, Err: err}
			}
		}
		if err := database.MarkRead(msg.ID, read); err != nil {
			return MessageReadUpdatedMsg{
				MessageID: msg.ID,
				MailboxID: msg.MailboxID,
				WasRead:   msg.Read,
				Read:      read,
				Advance:   advance,
				Err:       err,
			}
		}
		return MessageReadUpdatedMsg{
			MessageID: msg.ID,
			MailboxID: msg.MailboxID,
			WasRead:   msg.Read,
			Read:      read,
			Advance:   advance,
		}
	}
}

func (m *Model) archiveMessageCmd(msg db.Message) tea.Cmd {
	database := m.db
	sessions := m.sessions
	mailbox := m.mailboxByID(msg.MailboxID)
	acfg := m.accountCfgForMailbox(msg.MailboxID)
	return func() tea.Msg {
		if mailbox == nil {
			return MessageMovedMsg{MessageID: msg.ID, FromMailboxID: msg.MailboxID, Action: "archive", Err: fmt.Errorf("mailbox not found")}
		}
		archive, err := database.FindArchiveMailbox(mailbox.AccountID)
		if err != nil {
			return MessageMovedMsg{MessageID: msg.ID, FromMailboxID: msg.MailboxID, Action: "archive", Err: err}
		}
		if acfg.IMAPHost != "" && msg.UID != 0 {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			if err := sessions.Do(ctx, acfg, func(client *imapClient.Client) error {
				return client.MoveMessage(ctx, mailbox.Name, msg.UID, archive.Name)
			}); err != nil {
				return MessageMovedMsg{MessageID: msg.ID, FromMailboxID: msg.MailboxID, ToMailboxID: archive.ID, Action: "archive", Err: err}
			}
		}
		if err := database.MoveMessage(msg.ID, archive.ID); err != nil {
			return MessageMovedMsg{MessageID: msg.ID, FromMailboxID: msg.MailboxID, ToMailboxID: archive.ID, Action: "archive", Err: err}
		}
		return MessageMovedMsg{MessageID: msg.ID, FromMailboxID: msg.MailboxID, ToMailboxID: archive.ID, Action: "archive"}
	}
}

// deleteMessagesCmd deletes one or more messages. Messages are grouped by
// mailbox so each group shares a single IMAP connection — one connection per
// message trips per-user connection caps (Gmail: 15, Dovecot default: 10) and
// the overflow fails. The remote operation runs first; the local row (and its
// resync tombstone) is only removed for messages the server actually deleted,
// so a failed remote delete leaves the message visible instead of silently
// diverging from the server.
func (m *Model) deleteMessagesCmd(msgs []db.Message) tea.Cmd {
	database := m.db
	sessions := m.sessions
	type deleteBatch struct {
		acfg    config.AccountConfig
		mailbox db.Mailbox
		trash   *db.Mailbox
		msgs    []db.Message
	}
	var batches []*deleteBatch
	byMailbox := map[int64]*deleteBatch{}
	missing := 0
	for _, msg := range msgs {
		mailbox := m.mailboxByID(msg.MailboxID)
		if mailbox == nil {
			missing++
			continue
		}
		b := byMailbox[mailbox.ID]
		if b == nil {
			b = &deleteBatch{acfg: m.accountCfgForMailbox(msg.MailboxID), mailbox: *mailbox}
			if trash, err := database.FindTrashMailbox(mailbox.AccountID); err == nil {
				b.trash = &trash
			}
			byMailbox[mailbox.ID] = b
			batches = append(batches, b)
		}
		b.msgs = append(b.msgs, msg)
	}
	return func() tea.Msg {
		out := MessagesDeletedMsg{Failed: missing}
		if missing > 0 {
			out.Err = fmt.Errorf("mailbox not found")
		}
		for _, b := range batches {
			if err := deleteBatchRemote(sessions, b.acfg, b.mailbox, b.trash, b.msgs); err != nil {
				out.Failed += len(b.msgs)
				if out.Err == nil {
					out.Err = err
				}
				continue
			}
			for _, msg := range b.msgs {
				if err := database.DeleteMessage(msg.ID); err != nil {
					out.Failed++
					if out.Err == nil {
						out.Err = err
					}
					continue
				}
				out.Deleted = append(out.Deleted, MessageRef{ID: msg.ID, MailboxID: msg.MailboxID})
			}
		}
		return out
	}
}

// deleteBatchRemote deletes a mailbox's batch on the server over the account's
// pooled connection.
func deleteBatchRemote(sessions *imapClient.SessionPool, acfg config.AccountConfig, mailbox db.Mailbox, trash *db.Mailbox, msgs []db.Message) error {
	var uids []uint32
	for _, msg := range msgs {
		if msg.UID != 0 {
			uids = append(uids, msg.UID)
		}
	}
	if acfg.IMAPHost == "" || len(uids) == 0 {
		return nil // local-only message(s); nothing to do on the server
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return sessions.Do(ctx, acfg, func(client *imapClient.Client) error {
		action, target := remoteDeletePlan(mailbox, trash)
		if action == remoteDeleteMoveToTrash {
			return client.MoveMessages(ctx, mailbox.Name, uids, target.Name)
		}
		return client.DeleteMessages(ctx, mailbox.Name, uids)
	})
}

func remoteDeletePlan(source db.Mailbox, trash *db.Mailbox) (remoteDeleteAction, *db.Mailbox) {
	if trash == nil || source.ID == trash.ID || source.Name == trash.Name {
		return remoteDeleteExpunge, nil
	}
	return remoteDeleteMoveToTrash, trash
}

func (m *Model) openedMessageCmd(msg db.Message) tea.Cmd {
	if !m.cfg.Display.MarkReadOnOpen || msg.Read {
		return nil
	}
	return m.setMessageReadCmd(msg, true, false)
}

func (m *Model) focusedMessageChangedCmd(msg db.Message) tea.Cmd {
	if !m.cfg.Display.MarkReadOnFocus || msg.Read {
		return nil
	}
	return m.setMessageReadCmd(msg, true, false)
}

func (m *Model) markMailboxReadCmd(mailboxID int64) tea.Cmd {
	database := m.db
	return func() tea.Msg {
		if err := database.MarkAllRead(mailboxID); err != nil {
			return MailboxReadUpdatedMsg{Err: err}
		}
		return MailboxReadUpdatedMsg{MailboxIDs: []int64{mailboxID}}
	}
}

func (m *Model) markAccountReadCmd(accountID int64) tea.Cmd {
	mailboxIDs := make([]int64, 0)
	for _, mb := range m.mailboxes {
		if mb.AccountID == accountID {
			mailboxIDs = append(mailboxIDs, mb.ID)
		}
	}
	database := m.db
	return func() tea.Msg {
		applied := make([]int64, 0, len(mailboxIDs))
		for _, mbID := range mailboxIDs {
			if err := database.MarkAllRead(mbID); err != nil {
				return MailboxReadUpdatedMsg{MailboxIDs: applied, Err: err}
			}
			applied = append(applied, mbID)
		}
		return MailboxReadUpdatedMsg{MailboxIDs: applied}
	}
}

func renderArticleRow(prefix, title, age string, width int) string {
	prefixW := lipgloss.Width(prefix)
	ageW := lipgloss.Width(age)
	gapW := 2
	trailingW := 2
	if age == "" {
		gapW = 0
		trailingW = 0
	}
	titleW := max(0, width-prefixW-ageW-gapW-trailingW)
	row := prefix + padRight(truncate(title, titleW), titleW) + strings.Repeat(" ", gapW) + age + strings.Repeat(" ", trailingW)
	return padRight(row, width)
}

// renderArticleRowWithSender lays out a message row with a fixed-width sender
// column before the subject, keeping subjects vertically aligned across rows.
func renderArticleRowWithSender(prefix, sender, title, age string, width, senderW int) string {
	prefixW := lipgloss.Width(prefix)
	ageW := lipgloss.Width(age)
	gapW := 2
	trailingW := 2
	if age == "" {
		gapW = 0
		trailingW = 0
	}
	senderGap := 1
	titleW := max(0, width-prefixW-senderW-senderGap-ageW-gapW-trailingW)
	row := prefix +
		padRight(truncate(sender, senderW), senderW) + strings.Repeat(" ", senderGap) +
		padRight(truncate(title, titleW), titleW) + strings.Repeat(" ", gapW) +
		age + strings.Repeat(" ", trailingW)
	return padRight(row, width)
}

func (m Model) messageRowPrefix(read bool) string {
	if m.styles.PlainUI {
		if read {
			return "- "
		}
		return "* "
	}
	if !m.iconsEnabled() {
		if read {
			return "  "
		}
		return "o "
	}
	if read {
		return "· "
	}
	return "⬤ "
}
