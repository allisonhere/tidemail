package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/allisonhere/tidemail/internal/db"
	imapClient "github.com/allisonhere/tidemail/internal/imap"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type remoteDeleteAction int

const (
	remoteDeleteExpunge remoteDeleteAction = iota
	remoteDeleteMoveToTrash
)

func (m Model) renderMessagesPane() string {
	w := m.messagesPaneContentWidth()
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
			rows = append(rows, style.Width(w).Render(renderArticleRow(prefix, m.messageRowStar(false), unescapeDisplayText(draftSubject(draft)), age, w)))
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
				// When selected but not the cursor, tint the row with the theme's
				// accent (kept readable on the pane background) rather than a fixed
				// green that clashes on light themes.
				if i != m.messageCursor {
					selFg := accentReadableOn(m.styles.Theme.Unread, m.styles.Theme.Bg, 4.5)
					style = style.Foreground(selFg)
				}
			}
			subject := m.messageRowTitle(msg2)
			if threadCount > 1 {
				subject = fmt.Sprintf("%s (%d)", subject, threadCount)
			}
			cursor := i == m.messageCursor
			style = applyMessageRowState(style, msgSelected, msg2.Starred, cursor, m.styles.Theme)
			star := m.messageRowStar(msg2.Starred)
			var line string
			if m.cfg.Display.ShowSender {
				senderW := min(22, max(0, w/3))
				line = style.Width(w).Render(renderArticleRowWithSender(dot, star, senderDisplay(msg2.From), subject, age, w, senderW))
			} else {
				line = style.Width(w).Render(renderArticleRow(dot, star, subject, age, w))
			}
			rows = append(rows, line)
		}

		if len(m.filteredMessages) == 0 {
			switch {
			case m.initialLoading:
				rows = append(rows, msgRead.Render("  "+m.spinner.View()+" Loading messages…"))
			case m.searchMode:
				rows = append(rows, msgRead.Render("  no results"))
			default:
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
	if m.starredFirst {
		title += " (starred first)"
	}
	// Surface an active multi-selection so it's clear a bulk action (d/a/m/x)
	// will apply to those messages, not just the focused row.
	if n := len(m.selectedMessages); n > 0 {
		title += fmt.Sprintf("  ✓ %d selected", n)
	}

	var headerLine string
	if m.searchMode {
		// Badge fills from text through the gap to the hint area, using the
		// theme's focus accent so it stays on-palette (not a black block on
		// light themes). Mirrors the PaneHeaderActive treatment.
		badgeBg := m.styles.Theme.BorderFocus
		badgeStyle := lipgloss.NewStyle().
			Background(badgeBg).
			Foreground(accentReadableOn(m.styles.Theme.Fg, badgeBg, 4.5)).
			Bold(true)
		badgeText := "> " + m.headerLabel(title)
		badgeW := lipgloss.Width(badgeText)
		if badgeW >= w {
			badgeText = truncate(badgeText, w)
			badgeW = lipgloss.Width(badgeText)
		}
		hint := m.renderPaneHint(paneMessages)
		hintMax := max(0, w-badgeW)
		hint = truncate(hint, hintMax)
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
		Border(paneFrameBorder(m.styles.PlainUI, m.styles.RoundedCorners)).
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
			Foreground(accentReadableOn(m.styles.Theme.Fg, accent, 4.5))
		borderFocus = accent
	}
	borderFocus = accentReadableOn(borderFocus, m.styles.Theme.Bg, paneFocusMinContrast)
	return unread, read, selected, headerActive, border, borderFocus
}

func (m *Model) clearSelection() {
	m.selectedMessages = make(map[int64]bool)
}

func (m Model) hasSelection() bool {
	return len(m.selectedMessages) > 0
}

func (m *Model) applyFilter() {
	visible := make([]db.Message, 0, len(m.messages))
	for _, msg := range m.messages {
		if !m.messagePendingDestructive(msg.ID) {
			visible = append(visible, msg)
		}
	}
	// m.messages is already the match set when a search is running — the FTS
	// index covers subject, sender, recipients, and body. Narrowing it again
	// here would have to re-implement that matching locally, and the subject-only
	// substring test this used to do silently dropped every message that matched
	// on sender or body. The unread toggle is the only filter left to apply.
	filtered := visible
	if m.showUnreadOnly {
		filtered = make([]db.Message, 0, len(visible))
		for _, msg := range visible {
			if !msg.Read {
				filtered = append(filtered, msg)
			}
		}
	}
	m.filteredMessages = filtered
	m.sortFilteredStarred()
	m.rebuildMessageThreads()
}

// sortFilteredStarred stably floats starred messages to the top when the
// starred-first sort (key `t`) is active. It copies before sorting so the
// shared m.messages slice (which m.filteredMessages may alias) is never mutated.
func (m *Model) sortFilteredStarred() {
	if !m.starredFirst || len(m.filteredMessages) == 0 {
		return
	}
	sorted := make([]db.Message, len(m.filteredMessages))
	copy(sorted, m.filteredMessages)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Starred && !sorted[j].Starred
	})
	m.filteredMessages = sorted
}

func (m Model) messageRowTitle(msg db.Message) string {
	subject := messageListDisplayText(unescapeDisplayText(msg.Subject))
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

func (m *Model) setMessageStarredCmd(msg db.Message, starred bool) tea.Cmd {
	database := m.db
	sessions := m.sessions
	mailbox := m.mailboxByID(msg.MailboxID)
	acfg := m.accountCfgForMailbox(msg.MailboxID)
	return func() tea.Msg {
		if mailbox != nil && acfg.IMAPHost != "" && msg.UID != 0 {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := sessions.Do(ctx, acfg, func(client *imapClient.Client) error {
				return client.MarkFlagged(ctx, mailbox.Name, msg.UID, starred)
			}); err != nil {
				return MessageStarredUpdatedMsg{MessageID: msg.ID, MailboxID: msg.MailboxID, Starred: starred, Err: err}
			}
		}
		if err := database.MarkStarred(msg.ID, starred); err != nil {
			return MessageStarredUpdatedMsg{MessageID: msg.ID, MailboxID: msg.MailboxID, Starred: starred, Err: err}
		}
		return MessageStarredUpdatedMsg{MessageID: msg.ID, MailboxID: msg.MailboxID, Starred: starred}
	}
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

func renderArticleRow(prefix, star, title, age string, width int) string {
	prefixW := lipgloss.Width(prefix)
	starW := lipgloss.Width(star)
	ageW := lipgloss.Width(age)
	gapW := 2
	trailingW := 2
	if age == "" {
		gapW = 0
		trailingW = 0
	}
	titleW := max(0, width-prefixW-starW-ageW-gapW-trailingW)
	row := prefix + star + padRight(truncate(title, titleW), titleW) + strings.Repeat(" ", gapW) + age + strings.Repeat(" ", trailingW)
	return padRight(row, width)
}

// renderArticleRowWithSender lays out a message row with a fixed-width sender
// column before the subject, keeping subjects vertically aligned across rows.
func renderArticleRowWithSender(prefix, star, sender, title, age string, width, senderW int) string {
	prefixW := lipgloss.Width(prefix)
	starW := lipgloss.Width(star)
	ageW := lipgloss.Width(age)
	gapW := 2
	trailingW := 2
	if age == "" {
		gapW = 0
		trailingW = 0
	}
	senderGap := 1
	titleW := max(0, width-prefixW-starW-senderW-senderGap-ageW-gapW-trailingW)
	row := prefix + star +
		padRight(truncate(sender, senderW), senderW) + strings.Repeat(" ", senderGap) +
		padRight(truncate(title, titleW), titleW) + strings.Repeat(" ", gapW) +
		age + strings.Repeat(" ", trailingW)
	return padRight(row, width)
}

// starColor is the warm accent used to derive the subtle starred-row tint.
func starColor(t Theme) lipgloss.Color {
	return accentReadableOn(lipgloss.Color("#f9c74f"), t.Bg, 3)
}

func starredRowBackground(t Theme) lipgloss.Color {
	return mixColors(t.Bg, starColor(t), 0.12)
}

func applyMessageRowState(style, selected lipgloss.Style, starred, cursor bool, t Theme) lipgloss.Style {
	if starred {
		style = style.Background(starredRowBackground(t))
	}
	if cursor {
		return selected
	}
	return style
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

// messageRowStar returns the fixed-width star column that precedes the sender.
// It deliberately contains no ANSI styling: an inline reset would interrupt the
// enclosing row background, leaving the star cells (and sometimes the remainder
// of the row) with the terminal's default background.
func (m Model) messageRowStar(starred bool) string {
	if !starred {
		return "  "
	}
	if m.styles.PlainUI {
		return "* " // vt52 ASCII fallback
	}
	return "★ "
}
