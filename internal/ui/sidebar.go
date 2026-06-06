package ui

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
)

func (m Model) renderAccountsPane() string {
	w := m.feedsPaneWidth()
	innerW := w - 1
	focused := m.focused == paneAccounts
	title := m.renderPaneHeader(paneAccounts, "Accounts", focused, innerW)
	rows := []string{title}

	end := min(m.sidebarOffset+m.sidebarVisibleRows(), len(m.sidebarRows))
	for i := m.sidebarOffset; i < end; i++ {
		row := m.sidebarRows[i]
		selected := i == m.sidebarCursor
		switch row.kind {
		case rowKindAccount:
			rows = append(rows, m.renderAccountHeader(row.accountID, selected, innerW))
		case rowKindUnified:
			rows = append(rows, m.renderUnifiedInboxRow(selected, innerW))
		case rowKindMailbox:
			if mb := m.mailboxByID(row.mailboxID); mb != nil {
				rows = append(rows, m.renderSidebarMailboxRow(*mb, selected, innerW))
			}
		case rowKindSysFolderHeader:
			rows = append(rows, m.renderSectionHeader("System", row.count, rowKindSysFolderHeader, row.accountID, selected, innerW))
		case rowKindPersonalFolderHeader:
			rows = append(rows, m.renderSectionHeader(row.label, row.count, rowKindPersonalFolderHeader, row.accountID, selected, innerW))
		}
	}

	if len(m.sidebarRows) == 0 {
		rows = append(rows, m.styles.FeedItem.Foreground(
			lipgloss.Color(m.styles.Theme.Dimmed),
		).Render(m.emptyAccountsHint()))
	}
	footer := fmt.Sprintf("  %d accounts", len(m.accounts))
	footer = m.styles.ArticleRead.Width(innerW).Render(footer)
	bodyHeight := max(0, m.mainHeight()-1)
	for viewLineCount(rows) < bodyHeight {
		rows = append(rows, m.styles.FeedItem.Width(innerW).Render(""))
	}
	rows = append(rows, footer)

	border := m.styles.FeedsPane
	if focused {
		border = border.BorderForeground(m.styles.Theme.BorderFocus)
	}

	content := strings.Join(rows, "\n")
	return border.Width(innerW).Height(m.mainHeight()).Render(content)
}

func buildSidebarRows(accounts []db.Account, mailboxes []db.Mailbox, collapsed map[int64]bool, collapsedSections map[string]bool) []sidebarRow {
	byAccount := make(map[int64][]db.Mailbox)
	for _, mb := range mailboxes {
		byAccount[mb.AccountID] = append(byAccount[mb.AccountID], mb)
	}
	for id := range byAccount {
		slices.SortStableFunc(byAccount[id], func(a, b db.Mailbox) int {
			ra, rb := mailboxRank(a.Name), mailboxRank(b.Name)
			if ra != rb {
				return ra - rb
			}
			return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		})
	}
	rows := make([]sidebarRow, 0, len(accounts)+len(mailboxes)+1)
	if len(accounts) > 0 {
		rows = append(rows, sidebarRow{kind: rowKindUnified})
	}
	for _, acc := range accounts {
		rows = append(rows, sidebarRow{kind: rowKindAccount, accountID: acc.ID})
		if collapsed[acc.ID] {
			continue
		}
		mbs := byAccount[acc.ID]

		// Split INBOX out — it's always first, never inside a section.
		var inbox *db.Mailbox
		sysMbs := make([]db.Mailbox, 0, len(mbs))
		personalMbs := make([]db.Mailbox, 0, len(mbs))
		for _, mb := range mbs {
			if hasFlag(mb.Flags, "\\Noselect") {
				continue // cannot be opened, no messages — skip
			}
			// Also skip children of Noselect parents (e.g. dovecot.sieve under INBOX.dovecot)
			if idx := strings.LastIndex(mb.Name, "."); idx >= 0 {
				parent := mb.Name[:idx]
				skip := false
				for _, pmb := range mbs {
					if strings.EqualFold(pmb.Name, parent) && hasFlag(pmb.Flags, "\\Noselect") {
						skip = true
						break
					}
				}
				if skip {
					continue
				}
			}
			lower := strings.ToLower(mb.Name)
			if lower == "inbox" || strings.HasSuffix(lower, "/inbox") {
				mb := mb
				inbox = &mb
				continue
			}
			if isGmailSystemFolder(mb.Name) {
				sysMbs = append(sysMbs, mb)
			} else {
				personalMbs = append(personalMbs, mb)
			}
		}

		// INBOX first, always visible.
		if inbox != nil {
			rows = append(rows, sidebarRow{kind: rowKindMailbox, mailboxID: inbox.ID})
		}

		// System folders section (Gmail system labels).
		if len(sysMbs) > 0 {
			sysKey := fmt.Sprintf("system:%d", acc.ID)
			sysCollapsed := collapsedSections[sysKey]
			rows = append(rows, sidebarRow{kind: rowKindSysFolderHeader, accountID: acc.ID, label: "System", count: len(sysMbs)})
			if !sysCollapsed {
				for _, mb := range sysMbs {
					rows = append(rows, sidebarRow{kind: rowKindMailbox, mailboxID: mb.ID})
				}
			}
		}

		// Personal folders / user labels section.
		if len(personalMbs) > 0 {
			personalKey := fmt.Sprintf("personal:%d", acc.ID)
			personalCollapsed := collapsedSections[personalKey]
			label := "Labels"
			if len(personalMbs) == 1 {
				label = "Label"
			}
			rows = append(rows, sidebarRow{kind: rowKindPersonalFolderHeader, accountID: acc.ID, label: label, count: len(personalMbs)})
			if !personalCollapsed {
				for _, mb := range personalMbs {
					rows = append(rows, sidebarRow{kind: rowKindMailbox, mailboxID: mb.ID})
				}
			}
		}

	}
	return rows
}

func (m *Model) toggleSelectedAccount() bool {
	accountID, ok := m.selectedAccountID()
	if !ok {
		return false
	}
	m.collapsedAccounts[accountID] = !m.collapsedAccounts[accountID]
	m.saveCollapseState()
	m.rebuildSidebar()
	for i, row := range m.sidebarRows {
		if row.kind == rowKindAccount && row.accountID == accountID {
			m.sidebarCursor = i
			break
		}
	}
	m.clearMessages()
	return true
}

func (m *Model) toggleSelectedSection() bool {
	if m.sidebarCursor < 0 || m.sidebarCursor >= len(m.sidebarRows) {
		return false
	}
	row := m.sidebarRows[m.sidebarCursor]
	var secKey string
	switch row.kind {
	case rowKindSysFolderHeader:
		secKey = sectionKey(row.accountID, true)
	case rowKindPersonalFolderHeader:
		secKey = sectionKey(row.accountID, false)
	default:
		return false
	}
	m.collapsedSections[secKey] = !m.collapsedSections[secKey]
	m.saveCollapseState()
	m.rebuildSidebar()
	// Keep cursor on the section header after rebuild.
	for i, r := range m.sidebarRows {
		if r.kind == row.kind && r.accountID == row.accountID {
			m.sidebarCursor = i
			break
		}
	}
	return true
}

func (m *Model) saveCollapseState() {
	if m.db == nil {
		return
	}
	accts, _ := json.Marshal(m.collapsedAccounts)
	sects, _ := json.Marshal(m.collapsedSections)
	_ = m.db.SetSetting("collapsed_accounts", string(accts))
	_ = m.db.SetSetting("collapsed_sections", string(sects))
}

func (m *Model) loadCollapseState() {
	if m.db == nil {
		return
	}
	if s, err := m.db.GetSetting("collapsed_accounts"); err == nil && s != "" {
		_ = json.Unmarshal([]byte(s), &m.collapsedAccounts)
	}
	if s, err := m.db.GetSetting("collapsed_sections"); err == nil && s != "" {
		_ = json.Unmarshal([]byte(s), &m.collapsedSections)
	}
}

func (m *Model) adjustMailboxUnreadCount(mailboxID int64, delta int64) {
	for i := range m.mailboxes {
		if m.mailboxes[i].ID == mailboxID {
			m.mailboxes[i].UnreadCount = max(0, m.mailboxes[i].UnreadCount+delta)
			return
		}
	}
}

func (m *Model) markMailboxesReadInMemory(mailboxIDs []int64) {
	if len(mailboxIDs) == 0 {
		return
	}
	mbSet := make(map[int64]struct{}, len(mailboxIDs))
	for _, mbID := range mailboxIDs {
		mbSet[mbID] = struct{}{}
		for i := range m.mailboxes {
			if m.mailboxes[i].ID == mbID {
				m.mailboxes[i].UnreadCount = 0
				break
			}
		}
	}
	changed := false
	for i := range m.messages {
		if _, ok := mbSet[m.messages[i].MailboxID]; ok && !m.messages[i].Read {
			m.messages[i].Read = true
			changed = true
		}
	}
	if !changed {
		return
	}
	m.applyFilter()
	if len(m.filteredMessages) == 0 {
		m.messageCursor = 0
		m.listOffset = 0
		m.clearViewportMessage()
		return
	}
	m.messageCursor = clamp(m.messageCursor, 0, max(0, len(m.filteredMessages)-1))
	m.listOffset = clamp(m.listOffset, 0, max(0, len(m.filteredMessages)-1))
	m.setViewportMessage(m.filteredMessages[m.messageCursor])
}

func (m *Model) removeMessageFromMemory(messageID int64) bool {
	wasUnread := false
	for i := range m.messages {
		if m.messages[i].ID == messageID {
			wasUnread = !m.messages[i].Read
			m.messages = append(m.messages[:i], m.messages[i+1:]...)
			break
		}
	}
	m.applyFilter()
	if len(m.filteredMessages) == 0 {
		m.messageCursor = 0
		m.listOffset = 0
		m.clearViewportMessage()
		return wasUnread
	}
	m.messageCursor = clamp(m.messageCursor, 0, max(0, len(m.filteredMessages)-1))
	m.listOffset = clamp(m.listOffset, 0, max(0, len(m.filteredMessages)-1))
	m.setViewportMessage(m.filteredMessages[m.messageCursor])
	return wasUnread
}

func (m Model) accountName(accountID int64) string {
	for _, acc := range m.accounts {
		if acc.ID == accountID {
			return acc.Name
		}
	}
	return "Account"
}

func (m Model) accountCfgForMailbox(mailboxID int64) config.AccountConfig {
	mb := m.mailboxByID(mailboxID)
	if mb == nil {
		if len(m.cfg.Accounts) > 0 {
			return m.cfg.Accounts[0]
		}
		return config.AccountConfig{}
	}
	acc := m.accountByID(mb.AccountID)
	if acc != nil {
		for _, acfg := range m.cfg.Accounts {
			if acfg.Name == acc.Name {
				return acfg
			}
		}
	}
	if len(m.cfg.Accounts) > 0 {
		return m.cfg.Accounts[0]
	}
	return config.AccountConfig{}
}

func (m Model) accountColor(accountID int64) lipgloss.Color {
	if accountID == 0 {
		return ""
	}
	if config.IsRetroTerminalTheme(string(m.styles.Theme.Name)) {
		return ""
	}
	if acc := m.accountByID(accountID); acc != nil && acc.Color != "" {
		return lipgloss.Color(acc.Color)
	}
	return ""
}

func (m Model) selectedMailboxAccountColor() lipgloss.Color {
	if m.selectedUnifiedInbox() {
		return ""
	}
	if mb := m.selectedMailbox(); mb != nil {
		return m.accountColor(mb.AccountID)
	}
	return ""
}

func (m Model) accountUnreadCount(accountID int64) int64 {
	var total int64
	for _, mb := range m.mailboxes {
		if mb.AccountID == accountID {
			total += mb.UnreadCount
		}
	}
	return total
}

func (m Model) unifiedUnreadCount() int64 {
	var total int64
	for _, mb := range m.mailboxes {
		if isInboxMailbox(mb) {
			total += mb.UnreadCount
		}
	}
	return total
}

func (m Model) accountHeaderStyle(accountID int64, selected bool) lipgloss.Style {
	accent := m.accountColor(accountID)
	style := m.styles.FeedItem.Foreground(lipgloss.Color(m.styles.Theme.Dimmed)).Bold(true)
	if accent != "" {
		style = style.Foreground(accentReadableOn(accent, m.styles.Theme.Bg, 3))
	}
	if selected {
		style = m.sidebarSelectedStyle(accent).Bold(true)
	}
	return style
}

func (m Model) accountBadgeStyle(accountID int64, selected bool) lipgloss.Style {
	accent := m.accountColor(accountID)
	if selected {
		return m.sidebarSelectedBadgeStyle(accent)
	}
	if accent == "" {
		return m.styles.UnreadBadge
	}
	return m.styles.UnreadBadge.Foreground(accentReadableOn(accent, m.styles.Theme.Bg, 3))
}

func (m Model) mailboxAccentStyle(mb db.Mailbox, selected bool) lipgloss.Style {
	style := m.styles.FeedItem
	accent := m.accountColor(mb.AccountID)
	if accent != "" {
		style = style.Foreground(accentReadableOn(accent, m.styles.Theme.Bg, 3))
	}
	if selected {
		style = m.sidebarSelectedStyle(accent)
	}
	return style
}

func (m Model) mailboxBadgeStyle(mb db.Mailbox, selected bool) lipgloss.Style {
	accent := m.accountColor(mb.AccountID)
	if selected {
		return m.sidebarSelectedBadgeStyle(accent)
	}
	if accent == "" {
		return m.styles.UnreadBadge
	}
	return m.styles.UnreadBadge.Foreground(accentReadableOn(accent, m.styles.Theme.Bg, 3))
}

func (m Model) sidebarSelectedStyle(accent lipgloss.Color) lipgloss.Style {
	if m.focused == paneAccounts {
		if accent != "" {
			return m.styles.FeedItemSelectedFocused.
				Background(accent).
				Foreground(readableText(m.styles.Theme.Fg, accent, 4.5))
		}
		return m.styles.FeedItemSelectedFocused
	}

	style := m.styles.FeedItemSelectedUnfocused
	if accent != "" {
		bg := terminalColorAsColor(style.GetBackground())
		style = style.Foreground(accentReadableOn(accent, bg, 3))
	}
	return style
}

func (m Model) sidebarSelectedBadgeStyle(accent lipgloss.Color) lipgloss.Style {
	style := m.sidebarSelectedStyle(accent)
	return m.styles.UnreadBadge.Foreground(terminalColorAsColor(style.GetForeground()))
}

func renderFeedRow(prefix, title, badge string, width int) string {
	prefixW := lipgloss.Width(prefix)
	badgeW := lipgloss.Width(badge)
	gapW := 0
	if badge != "" {
		gapW = 1
	}
	nameW := max(0, width-prefixW-badgeW-gapW)
	name := truncate(title, nameW)
	row := prefix + padRight(name, nameW)
	if badge != "" {
		row += " " + badge
	}
	return padRight(row, width)
}

func renderPaneHeaderRow(prefix, title, hint string, width int) string {
	base := prefix + title
	baseW := lipgloss.Width(base)
	if width <= 0 {
		return ""
	}
	if hint == "" || baseW >= width {
		return padRight(truncate(base, width), width)
	}

	hintMax := max(0, width-baseW-1)
	if hintMax == 0 {
		return padRight(truncate(base, width), width)
	}
	hint = truncate(hint, hintMax)
	spaceW := max(1, width-baseW-lipgloss.Width(hint))
	return base + strings.Repeat(" ", spaceW) + hint
}

func (m Model) renderAccountHeader(accountID int64, selected bool, width int) string {
	icon := "v "
	label := m.accountName(accountID)
	if m.iconsEnabled() {
		icon = "▾ "
	}
	if m.collapsedAccounts[accountID] {
		icon = "> "
		if m.iconsEnabled() {
			icon = "▸ "
		}
	}
	row := renderFeedRow(icon, label, "", width)
	style := m.accountHeaderStyle(accountID, selected)
	return style.Width(width).Render(row)
}

func (m Model) renderSectionHeader(label string, count int, kind sidebarRowKind, accountID int64, selected bool, width int) string {
	isSystem := kind == rowKindSysFolderHeader
	secKey := sectionKey(accountID, isSystem)
	collapsed := m.collapsedSections[secKey]

	icon := "v "
	if m.iconsEnabled() {
		icon = "▾ "
	}
	if collapsed {
		icon = "> "
		if m.iconsEnabled() {
			icon = "▸ "
		}
	}

	row := renderFeedRow("  "+icon, label, "", width)
	style := m.styles.FeedItem
	if selected {
		style = m.sidebarSelectedStyle("")
	}
	return style.Width(width).Render(row)
}

func (m Model) renderUnifiedInboxRow(selected bool, width int) string {
	badge := ""
	if unread := m.unifiedUnreadCount(); unread > 0 {
		badge = m.accountBadgeStyle(0, selected).Render(fmt.Sprintf("(%d)", unread))
	}
	prefix := "◎ "
	if !m.iconsEnabled() {
		prefix = "* "
	}
	row := renderFeedRow(prefix, "Unified Inbox", badge, width)
	style := m.styles.FeedItem
	if selected {
		style = m.sidebarSelectedStyle("")
	}
	return style.Width(width).Render(row)
}

func (m Model) renderSidebarMailboxRow(mb db.Mailbox, selected bool, width int) string {
	badge := ""
	if mb.UnreadCount > 0 {
		badge = m.mailboxBadgeStyle(mb, selected).Render(fmt.Sprintf("(%d)", mb.UnreadCount))
	}
	raw := mb.DisplayName
	if raw == "" {
		raw = mb.Name
	}
	title := cleanDisplayName(raw)
	prefix := "    "
	if !m.iconsEnabled() {
		prefix = "    " + m.mailboxRowPrefix(selected)
	}
	if m.syncing[mb.ID] {
		prefix = "    " + m.spinner.View() + " "
	}
	row := renderFeedRow(prefix, title, badge, width)
	style := m.mailboxAccentStyle(mb, selected)
	return style.Width(width).Render(row)
}

func (m Model) iconsEnabled() bool {
	return m.cfg.Display.Icons
}

func (m Model) headerLabel(label string) string {
	if !m.iconsEnabled() {
		return label
	}
	switch label {
	case "Accounts":
		return "◉ Accounts"
	case "Content":
		return "▣ Content"
	}
	if strings.HasPrefix(label, "Messages") {
		return strings.Replace(label, "Messages", "≣ Messages", 1)
	}
	return label
}

func (m Model) mailboxRowPrefix(selected bool) string {
	if m.styles.PlainUI {
		if selected {
			return "> "
		}
		return "  "
	}
	if !m.iconsEnabled() {
		if selected {
			return "> "
		}
		return "  "
	}
	if selected {
		return "▸ "
	}
	return "◦ "
}

func (m Model) emptyAccountsHint() string {
	if m.styles.PlainUI {
		return "  press M to add accounts"
	}
	if m.iconsEnabled() {
		return "  ＋ press M to add accounts"
	}
	return "  press M to add accounts"
}

func (m *Model) resetHelpVP() {
	winW := min(m.width-6, 90)
	winH := min(m.height-4, 38)
	vpW := max(1, winW-1)
	vpH := winH - 3 // inside border, minus footer row
	m.helpVP = viewport.New(vpW, vpH)
	m.helpVP.SetContent(renderHelp(vpW, m.styles, m.keys))
}

func renderChromeOverlayBox(inner string, width int, chrome managerChrome, border lipgloss.Color) string {
	return lipgloss.NewStyle().
		Background(chrome.baseBg).
		Border(lipPaneBorder(chrome.plainUI)).
		BorderForeground(border).
		BorderBackground(chrome.baseBg).
		Width(width).
		Render(inner)
}
