package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/allisonhere/tide/internal/db"
	imapClient "github.com/allisonhere/tide/internal/imap"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func (m Model) renderMessagesPane() string {
	w := m.articlesPaneWidth()
	h := m.articlesPaneContentHeight()
	msgUnread, msgRead, msgSelected, headerActive, borderColor, borderFocus := m.messageRowStyles()

	rows := []string{}
	visible := m.filteredMessages
	end := min(m.listOffset+m.articleRowsVisible(), len(visible))
	for i := m.listOffset; i < end; i++ {
		msg2 := visible[i]
		age := m.formatTime(msg2.Date)
		style := msgRead
		if !msg2.Read {
			style = msgUnread
		}
		dot := m.messageRowPrefix(msg2.Read)
		if m.selectedMessages[msg2.ID] {
			dot = "✓ "
			// When selected but not the cursor, use green checkmark + text
			if i != m.messageCursor {
				selFg := lipgloss.Color("#a6e3a1")
				style = style.Copy().Foreground(selFg)
			}
		}
		if i == m.messageCursor {
			style = msgSelected
		}
		rows = append(rows, style.Width(w-2).Render(renderArticleRow(dot, unescapeDisplayText(msg2.Subject), age, w-2)))
	}

	if len(m.filteredMessages) == 0 {
		if m.searchQuery != "" {
			rows = append(rows, msgRead.Render("  no results"))
		} else {
			rows = append(rows, msgRead.Render("  no messages"))
		}
	}

	focused := m.focused == paneMessages
	border := m.styles.ArticlesPane
	if focused {
		border = border.BorderForeground(borderFocus)
	}
	title := "Messages"
	if m.selectedUnifiedInbox() {
		title = "Unified Inbox"
	}
	if m.searchQuery != "" {
		title = fmt.Sprintf("%s [/%s]", title, m.searchQuery)
	}
	if m.showUnreadOnly {
		title += " (unread)"
	}

	contentRows := append([]string{m.renderPaneHeaderWithAccent(paneMessages, title, focused, w, headerActive)}, rows...)
	for viewLineCount(contentRows) < h {
		contentRows = append(contentRows, msgRead.Width(w-2).Render(""))
	}

	bg := m.styles.Theme.Bg
	return lipgloss.NewStyle().
		Background(bg).
		Border(lipPaneBorder(m.styles.PlainUI), false, false, true, false).
		BorderForeground(lipgloss.Color(func() string {
			if focused {
				return string(borderFocus)
			}
			return string(borderColor)
		}())).
		BorderBackground(bg).
		Width(w).Height(h).
		Render(strings.Join(contentRows, "\n"))
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
		unread = unread.Copy().Foreground(leg)
		selected = selected.Copy().Foreground(leg)
		headerActive = headerActive.Copy().
			Background(accent).
			Foreground(readableText(m.styles.Theme.Fg, accent, 4.5))
		borderFocus = accent
	}
	return unread, read, selected, headerActive, border, borderFocus
}

func (m *Model) clearSelection() {
	m.selectedMessages = make(map[int64]bool)
}

func (m *Model) toggleMessageSelection(id int64) {
	if m.selectedMessages[id] {
		delete(m.selectedMessages, id)
	} else {
		m.selectedMessages[id] = true
	}
}

func (m Model) hasSelection() bool {
	return len(m.selectedMessages) > 0
}

func (m Model) selectedMessageCount() int {
	return len(m.selectedMessages)
}

func (m *Model) applyFilter() {
	q := strings.ToLower(m.searchQuery)
	if q == "" && !m.showUnreadOnly {
		m.filteredMessages = m.messages
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
}

func (m *Model) setMessageReadCmd(msg db.Message, read, advance bool) tea.Cmd {
	database := m.db
	mailbox := m.mailboxByID(msg.MailboxID)
	acfg := m.accountCfgForMailbox(msg.MailboxID)
	return func() tea.Msg {
		if mailbox != nil && acfg.IMAPHost != "" && msg.UID != 0 {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			client := imapClient.New(acfg)
			if err := client.Connect(ctx); err != nil {
				return MessageReadUpdatedMsg{MessageID: msg.ID, MailboxID: msg.MailboxID, WasRead: msg.Read, Read: read, Advance: advance, Err: err}
			}
			defer client.Close()
			if err := client.MarkSeen(ctx, mailbox.Name, msg.UID, read); err != nil {
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
			client := imapClient.New(acfg)
			if err := client.Connect(ctx); err != nil {
				return MessageMovedMsg{MessageID: msg.ID, FromMailboxID: msg.MailboxID, ToMailboxID: archive.ID, Action: "archive", Err: err}
			}
			defer client.Close()
			if err := client.MoveMessage(ctx, mailbox.Name, msg.UID, archive.Name); err != nil {
				return MessageMovedMsg{MessageID: msg.ID, FromMailboxID: msg.MailboxID, ToMailboxID: archive.ID, Action: "archive", Err: err}
			}
		}
		if err := database.MoveMessage(msg.ID, archive.ID); err != nil {
			return MessageMovedMsg{MessageID: msg.ID, FromMailboxID: msg.MailboxID, ToMailboxID: archive.ID, Action: "archive", Err: err}
		}
		return MessageMovedMsg{MessageID: msg.ID, FromMailboxID: msg.MailboxID, ToMailboxID: archive.ID, Action: "archive"}
	}
}

func (m *Model) deleteMessageCmd(msg db.Message) tea.Cmd {
	database := m.db
	mailbox := m.mailboxByID(msg.MailboxID)
	acfg := m.accountCfgForMailbox(msg.MailboxID)
	return func() tea.Msg {
		if mailbox == nil {
			return MessageDeletedMsg{MessageID: msg.ID, MailboxID: msg.MailboxID, Err: fmt.Errorf("mailbox not found")}
		}
		if acfg.IMAPHost != "" && msg.UID != 0 {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			client := imapClient.New(acfg)
			if err := client.Connect(ctx); err != nil {
				return MessageDeletedMsg{MessageID: msg.ID, MailboxID: msg.MailboxID, Err: err}
			}
			defer client.Close()
			if err := client.DeleteMessage(ctx, mailbox.Name, msg.UID); err != nil {
				return MessageDeletedMsg{MessageID: msg.ID, MailboxID: msg.MailboxID, Err: err}
			}
		}
		if err := database.DeleteMessage(msg.ID); err != nil {
			return MessageDeletedMsg{MessageID: msg.ID, MailboxID: msg.MailboxID, Err: err}
		}
		return MessageDeletedMsg{MessageID: msg.ID, MailboxID: msg.MailboxID}
	}
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
	if age == "" {
		gapW = 0
	}
	titleW := max(0, width-prefixW-ageW-gapW)
	row := prefix + padRight(truncate(title, titleW), titleW) + strings.Repeat(" ", gapW) + age
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
