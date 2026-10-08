package ui

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func (m Model) renderAccountsPane() string {
	innerW := m.accountsPaneContentWidth()
	focused := m.focused == paneAccounts
	rows := []string{}
	if m.cfg.Display.ShowPaneHeaders {
		rows = append(rows, m.renderPaneHeader(paneAccounts, "Accounts", focused, innerW))
	}

	end := min(m.sidebarOffset+m.sidebarVisibleRows(), len(m.sidebarRows))
	for i := m.sidebarOffset; i < end; i++ {
		row := m.sidebarRows[i]
		selected := i == m.sidebarCursor
		switch row.kind {
		case rowKindAccount:
			rows = append(rows, m.renderAccountHeader(row.accountID, selected, innerW))
		case rowKindUnified:
			rows = append(rows, m.renderUnifiedInboxRow(selected, innerW))
		case rowKindNeedsYou:
			rows = append(rows, m.renderNeedsYouRow(selected, innerW))
		case rowKindWaiting:
			rows = append(rows, m.renderWaitingRow(selected, innerW))
		case rowKindSnoozed:
			rows = append(rows, m.renderSnoozedRow(selected, innerW))
		case rowKindOutbox:
			rows = append(rows, m.renderOutboxRow(selected, innerW))
		case rowKindMailbox:
			if mb := m.mailboxByID(row.mailboxID); mb != nil {
				if selected && m.folderPrompt.active && m.folderPrompt.rename {
					rows = append(rows, m.folderPromptRow(row.depth, innerW))
				} else {
					rows = append(rows, m.renderSidebarMailboxRow(*mb, row, selected, innerW))
				}
			}
		case rowKindSysFolderHeader:
			rows = append(rows, m.renderSectionHeader("System", row.count, rowKindSysFolderHeader, row.accountID, selected, innerW))
		case rowKindPersonalFolderHeader:
			rows = append(rows, m.renderSectionHeader(row.label, row.count, rowKindPersonalFolderHeader, row.accountID, selected, innerW))
		}
		if selected && m.folderPrompt.active && !m.folderPrompt.rename {
			rows = append(rows, m.folderPromptRow(row.depth+1, innerW))
		}
	}

	if len(m.sidebarRows) == 0 {
		rows = append(rows, m.styles.FeedItem.Foreground(
			lipgloss.Color(m.styles.Theme.Dimmed),
		).Render(m.emptyAccountsHint()))
	}
	footer := fmt.Sprintf("  %d accounts", len(m.accounts))
	footer = m.styles.ArticleRead.Width(innerW).Render(footer)
	bodyHeight := max(0, m.accountsPaneContentHeight()-m.styles.ListItemLineStride())
	for viewLineCount(rows) < bodyHeight {
		rows = append(rows, m.styles.FeedItem.Width(innerW).Render(""))
	}
	rows = append(rows, footer)

	border := m.styles.PaneFrame(focused)

	content := clampView(strings.Join(rows, "\n"), innerW, m.accountsPaneContentHeight(), m.styles.Theme.Bg)
	return border.Width(innerW).Height(m.accountsPaneContentHeight()).Render(content)
}

// folderView is the local, per-folder view state the sidebar honours: which
// folders are hidden or custom-ordered, and whether hidden ones are shown.
type folderView struct {
	prefs      map[int64]map[string]db.MailboxPref
	showHidden bool
}

func (v folderView) pref(mb db.Mailbox) db.MailboxPref { return v.prefs[mb.AccountID][mb.Name] }

// visible drops hidden folders and everything below them, unless hidden
// folders are being shown.
func (v folderView) visible(mbs []db.Mailbox) []db.Mailbox {
	if v.showHidden || len(v.prefs) == 0 {
		return mbs
	}
	out := make([]db.Mailbox, 0, len(mbs))
	for _, mb := range mbs {
		hidden := v.pref(mb).Hidden
		for _, other := range mbs {
			if !hidden && v.pref(other).Hidden && mailboxIsDescendant(other, mb) {
				hidden = true
			}
		}
		if !hidden {
			out = append(out, mb)
		}
	}
	return out
}

func buildSidebarRows(accounts []db.Account, mailboxes []db.Mailbox, collapsed map[int64]bool, collapsedSections map[string]bool, views ...folderView) []sidebarRow {
	var view folderView
	if len(views) > 0 {
		view = views[0]
	}
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
	rows := make([]sidebarRow, 0, len(accounts)+len(mailboxes)+2)
	if len(accounts) > 0 {
		rows = append(rows, sidebarRow{kind: rowKindUnified})
		rows = append(rows, sidebarRow{kind: rowKindNeedsYou})
		rows = append(rows, sidebarRow{kind: rowKindWaiting})
		rows = append(rows, sidebarRow{kind: rowKindSnoozed})
		// Beside the Unified Inbox, because outgoing mail spans accounts the
		// same way — and because a stuck message needs somewhere to be seen.
		rows = append(rows, sidebarRow{kind: rowKindOutbox})
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

		sysMbs, personalMbs = view.visible(sysMbs), view.visible(personalMbs)

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
				rows = append(rows, folderTreeRows(sysMbs, collapsedSections, view)...)
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
				rows = append(rows, folderTreeRows(personalMbs, collapsedSections, view)...)
			}
		}

	}
	return rows
}

// mailboxPath is a mailbox's hierarchy as path segments, split on its own
// delimiter. A leading INBOX segment (Dovecot/Courier "INBOX.Work") is only a
// namespace prefix and is dropped.
func mailboxPath(mb db.Mailbox) []string {
	segs := splitMovePath(mb.Name, moveMailboxDelimiter(mb))
	if len(segs) > 1 && strings.EqualFold(segs[0], "INBOX") {
		segs = segs[1:]
	}
	return segs
}

func pathKey(segs []string) string {
	return strings.ToLower(strings.Join(segs, "\x00"))
}

// folderCollapseKey is the collapsedSections key for one folder.
func folderCollapseKey(mb db.Mailbox) string {
	return fmt.Sprintf("folder:%d:%s", mb.AccountID, mb.Name)
}

// mailboxIsDescendant reports whether child sits below parent in the folder
// hierarchy of one account.
func mailboxIsDescendant(parent, child db.Mailbox) bool {
	if parent.AccountID != child.AccountID || parent.ID == child.ID {
		return false
	}
	ps, cs := mailboxPath(parent), mailboxPath(child)
	if len(ps) == 0 || len(cs) <= len(ps) {
		return false
	}
	return pathKey(cs[:len(ps)]) == pathKey(ps)
}

// folderTreeRows lays one account's folders out as a tree: each folder is
// followed by its subfolders (unless collapsed), indented by depth. A folder
// whose parent is not itself listed hangs from its nearest listed ancestor, or
// sits at the top level, and shows the path below that ancestor as its title.
func folderTreeRows(mbs []db.Mailbox, collapsed map[string]bool, view folderView) []sidebarRow {
	byPath := make(map[string]int, len(mbs))
	for i, mb := range mbs {
		byPath[pathKey(mailboxPath(mb))] = i
	}
	parent := make([]int, len(mbs))
	for i, mb := range mbs {
		parent[i] = -1
		segs := mailboxPath(mb)
		for n := len(segs) - 1; n >= 1; n-- {
			if j, ok := byPath[pathKey(segs[:n])]; ok && j != i {
				parent[i] = j
				break
			}
		}
	}
	children := make(map[int][]int)
	for i, p := range parent {
		children[p] = append(children[p], i)
	}
	for _, kids := range children {
		slices.SortStableFunc(kids, func(a, b int) int {
			oa, ob := view.pref(mbs[a]).Order, view.pref(mbs[b]).Order
			if (oa != 0 || ob != 0) && oa != ob {
				switch {
				case oa == 0:
					return 1
				case ob == 0:
					return -1
				}
				return oa - ob
			}
			ra, rb := mailboxRank(mbs[a].Name), mailboxRank(mbs[b].Name)
			if ra != rb {
				return ra - rb
			}
			return strings.Compare(strings.ToLower(mbs[a].Name), strings.ToLower(mbs[b].Name))
		})
	}
	var rows []sidebarRow
	var walk func(i, depth int)
	walk = func(i, depth int) {
		mb := mbs[i]
		row := sidebarRow{kind: rowKindMailbox, mailboxID: mb.ID, depth: depth, hasChildren: len(children[i]) > 0}
		if parent[i] >= 0 {
			segs, pseg := mailboxPath(mb), mailboxPath(mbs[parent[i]])
			row.label = strings.Join(segs[len(pseg):], moveMailboxDelimiter(mb))
		}
		row.hidden = view.pref(mb).Hidden
		row.collapsed = row.hasChildren && collapsed[folderCollapseKey(mb)]
		rows = append(rows, row)
		if row.collapsed {
			return
		}
		for _, c := range children[i] {
			walk(c, depth+1)
		}
	}
	for _, i := range children[-1] {
		walk(i, 0)
	}
	return rows
}

// toggleSelectedFolder collapses or expands the folder under the cursor when it
// has subfolders.
func (m *Model) toggleSelectedFolder() bool {
	return m.setSelectedFolderCollapsed(nil)
}

// setSelectedFolderCollapsed sets the collapsed state of the folder under the
// cursor (nil toggles). It reports false for a row that is not a parent folder.
func (m *Model) setSelectedFolderCollapsed(want *bool) bool {
	if m.sidebarCursor < 0 || m.sidebarCursor >= len(m.sidebarRows) {
		return false
	}
	row := m.sidebarRows[m.sidebarCursor]
	if row.kind != rowKindMailbox || !row.hasChildren {
		return false
	}
	mb := m.mailboxByID(row.mailboxID)
	if mb == nil {
		return false
	}
	next := !row.collapsed
	if want != nil {
		next = *want
	}
	if next == row.collapsed {
		return true
	}
	m.collapsedSections[folderCollapseKey(*mb)] = next
	m.saveCollapseState()
	m.rebuildSidebar()
	m.cursorToMailbox(mb.ID)
	return true
}

// loadSelectedFolderCmd loads whatever the sidebar cursor landed on, as moving
// the cursor with Up/Down does.
func (m *Model) loadSelectedFolderCmd() tea.Cmd {
	m.cancelFolderSettle()
	if m.searchActive() {
		return nil
	}
	if cmd := m.virtualViewCmd(); cmd != nil {
		return cmd
	}
	if selected := m.selectedMailbox(); selected != nil {
		cmd := m.loadMailboxMessagesCmd(selected.ID)
		if m.selectedDraftsMailbox() {
			cmd = tea.Batch(cmd, m.loadDraftsCmd(selected.ID))
		}
		return tea.Batch(cmd, m.scheduleFolderSettle())
	}
	m.clearMessages()
	return nil
}

func (m *Model) cursorToMailbox(id int64) {
	for i, r := range m.sidebarRows {
		if r.kind == rowKindMailbox && r.mailboxID == id {
			m.sidebarCursor = i
			return
		}
	}
}

// collapseOrJumpToParentFolder is the Left key on a folder row: collapse an open
// parent, otherwise move to the folder's parent.
func (m *Model) collapseOrJumpToParentFolder() bool {
	if m.sidebarCursor < 0 || m.sidebarCursor >= len(m.sidebarRows) {
		return false
	}
	row := m.sidebarRows[m.sidebarCursor]
	if row.kind != rowKindMailbox {
		return false
	}
	if row.hasChildren && !row.collapsed {
		collapse := true
		return m.setSelectedFolderCollapsed(&collapse)
	}
	if row.depth == 0 {
		return false
	}
	for i := m.sidebarCursor - 1; i >= 0; i-- {
		r := m.sidebarRows[i]
		if r.kind == rowKindMailbox && r.depth < row.depth {
			m.sidebarCursor = i
			return true
		}
	}
	return false
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
	if m.activeMessageRowCount() == 0 {
		m.messageCursor = 0
		m.listOffset = 0
		m.clearViewportMessage()
		return
	}
	rowCount := m.activeMessageRowCount()
	m.messageCursor = clamp(m.messageCursor, 0, max(0, rowCount-1))
	m.listOffset = clamp(m.listOffset, 0, max(0, rowCount-1))
	m.setViewportForCurrentRow()
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
	if m.activeMessageRowCount() == 0 {
		m.messageCursor = 0
		m.listOffset = 0
		m.clearViewportMessage()
		return wasUnread
	}
	rowCount := m.activeMessageRowCount()
	m.messageCursor = clamp(m.messageCursor, 0, max(0, rowCount-1))
	m.listOffset = clamp(m.listOffset, 0, max(0, rowCount-1))
	m.setViewportForCurrentRow()
	return wasUnread
}

// looksLikeMissingCredential matches connect errors that mean "we had no
// password / token to send" — as opposed to a genuinely rejected one. Paired
// with a failed keychain probe it points at a locked keychain, not the account.
func looksLikeMissingCredential(err error) bool {
	if err == nil {
		return false
	}
	m := strings.ToLower(err.Error())
	return strings.Contains(m, "empty username or password") ||
		strings.Contains(m, "no refresh token")
}

// anyAccountNeedsKeychain reports whether some account is missing the secret it
// needs to connect (empty in memory after fillSecrets) — the signal that a
// keychain read came back empty rather than the account being credential-less
// with everything in the plaintext config.
func (m Model) anyAccountNeedsKeychain() bool {
	for _, a := range m.cfg.Accounts {
		if a.AuthMethod == config.AuthOAuth2 {
			if a.RefreshToken == "" {
				return true
			}
		} else if a.Password == "" {
			return true
		}
	}
	return false
}

func (m Model) accountName(accountID int64) string {
	for _, acc := range m.accounts {
		if acc.ID == accountID {
			return acc.Name
		}
	}
	return "Account"
}

func (m Model) accountCfgForMailbox(mailboxID int64) (config.AccountConfig, error) {
	mb := m.mailboxByID(mailboxID)
	if mb == nil {
		return config.AccountConfig{}, fmt.Errorf("%w (mailbox %d)", errNoAccountConfig, mailboxID)
	}
	acc := m.accountByID(mb.AccountID)
	if acc == nil {
		return config.AccountConfig{}, fmt.Errorf("%w (account %d)", errNoAccountConfig, mb.AccountID)
	}
	return m.accountConfigFor(*acc)
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
	if m.selectedUnifiedInbox() || m.selectedNeedsYou() || m.selectedWaiting() || m.selectedSnoozed() {
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
			total += m.displayMailboxUnreadCount(mb)
		}
	}
	return total
}

func (m Model) unifiedUnreadCount() int64 {
	var total int64
	for _, mb := range m.mailboxes {
		if isInboxMailbox(mb) {
			total += m.displayMailboxUnreadCount(mb)
		}
	}
	return total
}

func (m Model) displayMailboxUnreadCount(mb db.Mailbox) int64 {
	return max(0, mb.UnreadCount-m.pendingUnreadCount(mb.ID))
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
				Foreground(accentReadableOn(m.styles.Theme.Fg, accent, 4.5))
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
	duplicates := 0
	for _, acc := range m.accounts {
		if acc.Name == label {
			duplicates++
		}
	}
	if duplicates > 1 {
		if acc := m.accountByID(accountID); acc != nil {
			if acfg, err := m.accountConfigFor(*acc); err == nil && acfg.User != "" {
				label += " · " + acfg.User
			}
		}
	}
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

// renderOutboxRow draws the standing Outbox entry. Its badge counts only what
// needs a person — a failed or unconfirmed send — in the error color, so the
// row is quiet during the seconds every normal message spends queued.
func (m Model) renderOutboxRow(selected bool, width int) string {
	badge := ""
	if t := m.summarizeOutbox(); t.needsAttention() > 0 {
		style := m.accountBadgeStyle(0, selected)
		if !selected {
			style = style.Foreground(m.styles.Theme.Error)
		}
		badge = style.Render(fmt.Sprintf("(%d)", t.needsAttention()))
	}
	prefix := "↑ "
	if !m.iconsEnabled() {
		prefix = "^ "
	}
	row := renderFeedRow(prefix, "Outbox", badge, width)
	style := m.styles.FeedItem
	if selected {
		style = m.sidebarSelectedStyle("")
	}
	return style.Width(width).Render(row)
}

func (m Model) renderSidebarMailboxRow(mb db.Mailbox, row sidebarRow, selected bool, width int) string {
	badge := ""
	if m.isDraftsMailbox(mb) {
		if count := m.draftCounts[mb.ID]; count > 0 {
			badge = m.mailboxBadgeStyle(mb, selected).Render(fmt.Sprintf("(%d)", count))
		}
	} else if unread := m.folderUnreadCount(mb, row.collapsed); unread > 0 {
		badge = m.mailboxBadgeStyle(mb, selected).Render(fmt.Sprintf("(%d)", unread))
	}
	title := row.label
	if title == "" {
		raw := mb.DisplayName
		if raw == "" {
			raw = mb.Name
		}
		title = cleanDisplayName(raw)
	}
	if row.hidden {
		title += " (hidden)"
	}
	indent := strings.Repeat("  ", row.depth)
	prefix := "    "
	if !m.iconsEnabled() {
		prefix = "    " + m.mailboxRowPrefix(selected)
	}
	if row.hasChildren {
		marker := "▾"
		if row.collapsed {
			marker = "▸"
		}
		if !m.iconsEnabled() {
			marker = "-"
			if row.collapsed {
				marker = "+"
			}
		}
		prefix = "  " + marker + " "
		if !m.iconsEnabled() {
			prefix = "    " + marker + " "
		}
	}
	if m.syncVisible[mb.ID] {
		prefix = "    " + m.spinner.View() + " "
	}
	line := renderFeedRow(indent+prefix, title, badge, width)
	style := m.mailboxAccentStyle(mb, selected)
	return style.Width(width).Render(line)
}

// folderUnreadCount is a folder's unread count; a collapsed parent also counts
// everything hidden below it.
func (m Model) folderUnreadCount(mb db.Mailbox, collapsed bool) int64 {
	total := m.displayMailboxUnreadCount(mb)
	if !collapsed {
		return total
	}
	for _, other := range m.mailboxes {
		if mailboxIsDescendant(mb, other) {
			total += m.displayMailboxUnreadCount(other)
		}
	}
	return total
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
	vpW := winW
	vpH := max(1, winH-4) // soft box: title line + bottom border + blank + footer
	m.helpVP = viewport.New(vpW, vpH)
	m.helpVP.SetContent(renderHelp(vpW, m.styles, m.keys, m.helpSearchInput.Value()))
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
