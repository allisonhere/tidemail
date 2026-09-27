package ui

// Needs You: a virtual, cross-account view of inbox messages that stored
// annotations mark as actionable (needs_reply, urgency, importance from any
// plugin; see db.ListNeedsYou). It reads only local data: opening it never
// runs a plugin or touches the network. Users can dismiss false positives; a
// dismissal is TideMail's own state and never changes mail or annotations.

import (
	"fmt"

	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// needsYouState is Needs You's cached state; rendering reads only this.
type needsYouState struct {
	count       int
	countLoaded bool
	// dismissed holds dismissed message IDs, for "Restore to Needs You".
	dismissed map[int64]bool
	// lastDismissed is the most recent dismissal (all messages of a threaded
	// row), for ctrl+z.
	lastDismissed []int64
}

type (
	needsYouCountMsg struct {
		Count     int
		Dismissed map[int64]bool
		Err       error
	}
	needsYouDismissMsg struct {
		MessageIDs []int64
		Dismissed  bool
		Err        error
	}
)

func (m Model) selectedNeedsYou() bool {
	if m.sidebarCursor < 0 || m.sidebarCursor >= len(m.sidebarRows) {
		return false
	}
	return m.sidebarRows[m.sidebarCursor].kind == rowKindNeedsYou
}

// loadNeedsYouCmd loads the view, with annotations batched in the same
// command like every other message list.
func (m *Model) loadNeedsYouCmd() tea.Cmd {
	database := m.db
	unreadOnly := m.showUnreadOnly
	return func() tea.Msg {
		if database == nil {
			return MessagesLoadedMsg{NeedsYou: true}
		}
		msgs, err := database.ListNeedsYou(unreadOnly)
		if err != nil {
			return MessagesLoadedMsg{NeedsYou: true, Err: err}
		}
		return MessagesLoadedMsg{NeedsYou: true, Messages: msgs, Annotations: loadMessageAnnotations(database, msgs)}
	}
}

// loadNeedsYouCountCmd refreshes the sidebar count and the dismissed set.
func loadNeedsYouCountCmd(database *db.DB) tea.Cmd {
	if database == nil {
		return nil
	}
	return func() tea.Msg {
		n, err := database.CountNeedsYou()
		if err != nil {
			return needsYouCountMsg{Err: err}
		}
		dismissed, err := database.DismissedFromNeedsYou()
		return needsYouCountMsg{Count: n, Dismissed: dismissed, Err: err}
	}
}

// virtualViewCmd reloads the selected cross-account view (Unified Inbox or
// Needs You), or returns nil when a real mailbox is selected.
func (m *Model) virtualViewCmd() tea.Cmd {
	switch {
	case m.selectedUnifiedInbox():
		return m.loadUnifiedInboxCmd()
	case m.selectedNeedsYou():
		return m.loadNeedsYouCmd()
	}
	return nil
}

// needsYouRefreshCmd updates the count and, when Needs You is open, the list.
// It runs after anything that can change qualification: annotation writes and
// cleanups, dismissals, and syncs.
func (m *Model) needsYouRefreshCmd() tea.Cmd {
	cmds := []tea.Cmd{loadNeedsYouCountCmd(m.db)}
	if m.selectedNeedsYou() && !m.searchActive() {
		cmds = append(cmds, m.loadNeedsYouCmd())
	}
	return tea.Batch(cmds...)
}

func (m Model) handleNeedsYouMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case needsYouCountMsg:
		if msg.Err == nil {
			m.needsYou.count = msg.Count
			m.needsYou.dismissed = msg.Dismissed
			m.needsYou.countLoaded = true
		}
		return m, nil
	case needsYouDismissMsg:
		if msg.Err != nil {
			// Nothing changed in the database, so the row stays.
			m.setStatus("Needs You: "+msg.Err.Error(), true)
			return m, m.clearStatusCmd()
		}
		if msg.Dismissed {
			m.needsYou.lastDismissed = msg.MessageIDs
			m.setStatus(fmt.Sprintf("dismissed from Needs You%sctrl+z undo", m.styles.InlineMidDot()), false)
		} else {
			m.needsYou.lastDismissed = nil
			m.setStatus("restored to Needs You", false)
		}
		return m, tea.Batch(m.needsYouRefreshCmd(), m.clearStatusCmd())
	}
	return m, nil
}

// setDismissedCmd dismisses or restores messages off the Update loop. It
// stops at the first failure, leaving the UI unchanged for it.
func setDismissedCmd(database *db.DB, ids []int64, dismissed bool) tea.Cmd {
	ids = append([]int64(nil), ids...)
	return func() tea.Msg {
		if database == nil {
			return needsYouDismissMsg{MessageIDs: ids, Dismissed: dismissed, Err: fmt.Errorf("no database")}
		}
		for _, id := range ids {
			if err := database.SetNeedsYouDismissed(id, dismissed); err != nil {
				return needsYouDismissMsg{MessageIDs: ids, Dismissed: dismissed, Err: err}
			}
		}
		return needsYouDismissMsg{MessageIDs: ids, Dismissed: dismissed}
	}
}

// dismissCurrent dismisses the selected message (every message of a threaded
// row) from Needs You.
func (m Model) dismissCurrent() (tea.Model, tea.Cmd) {
	msgs := m.currentRowMessages()
	if len(msgs) == 0 {
		return m, nil
	}
	ids := make([]int64, len(msgs))
	for i, msg := range msgs {
		ids[i] = msg.ID
	}
	return m, setDismissedCmd(m.db, ids, true)
}

// undoNeedsYouDismissal restores the latest dismissal while Needs You is open.
func (m *Model) undoNeedsYouDismissal() (tea.Cmd, bool) {
	if !m.selectedNeedsYou() || len(m.needsYou.lastDismissed) == 0 {
		return nil, false
	}
	ids := m.needsYou.lastDismissed
	m.needsYou.lastDismissed = nil
	return setDismissedCmd(m.db, ids, false), true
}

func (m Model) needsYouCommandItems() []commandItem {
	msg := m.commandMessage()
	if msg == nil {
		return nil
	}
	var items []commandItem
	if m.selectedNeedsYou() {
		items = append(items,
			commandItem{id: "needs-you-why", label: "Why is this in Needs You?", enabled: true},
			commandItem{id: "needs-you-dismiss", label: "Dismiss from Needs You", enabled: true},
		)
	}
	if m.needsYou.dismissed[msg.ID] {
		items = append(items, commandItem{id: "needs-you-restore", label: "Restore to Needs You", enabled: true})
	}
	return items
}

func (m Model) executeNeedsYouCommand(id string) (tea.Model, tea.Cmd) {
	msg := m.commandMessage()
	if msg == nil {
		return m, nil
	}
	switch id {
	case "needs-you-why":
		m.plugins.annotationsFor = msg.ID
		m.plugins.scroll = 0
		m.plugins.annCursor = 0
		m.overlay = overlayPluginAnnotations
		return m, nil
	case "needs-you-dismiss":
		return m.dismissCurrent()
	case "needs-you-restore":
		return m, setDismissedCmd(m.db, []int64{msg.ID}, false)
	}
	return m, nil
}

// needsYouReasons explains, deterministically, why a message is in Needs You.
func (m Model) needsYouReasons(messageID int64) []string {
	a := db.AttentionOf(m.plugins.annotations[messageID])
	if !a.Any() {
		return nil
	}
	icons := m.iconsEnabled() && !m.styles.PlainUI
	glyph := func(icon, ascii string) string {
		if icons {
			return icon
		}
		return ascii
	}
	var out []string
	if a.NeedsReply {
		out = append(out, glyph("↩", "R")+" needs reply")
	}
	if a.Urgent {
		out = append(out, "! urgent")
	}
	if a.Important {
		out = append(out, glyph("◆", "^")+" important")
	}
	return out
}

// needsYouExplanationLines heads the annotations overlay.
func (m Model) needsYouExplanationLines(messageID int64, chrome managerChrome) []string {
	reasons := m.needsYouReasons(messageID)
	if len(reasons) == 0 {
		return nil
	}
	base := lipgloss.NewStyle().Background(chrome.baseBg)
	head := "Needs You because:"
	if m.needsYou.dismissed[messageID] {
		head = "Would be in Needs You (dismissed) because:"
	}
	lines := []string{base.Foreground(chrome.text).Bold(true).Render(head)}
	for _, r := range reasons {
		lines = append(lines, base.Foreground(chrome.text).Render("  "+r))
	}
	return append(lines, "")
}

func (m Model) renderNeedsYouRow(selected bool, width int) string {
	badge := ""
	if m.needsYou.count > 0 {
		badge = m.accountBadgeStyle(0, selected).Render(fmt.Sprintf("(%d)", m.needsYou.count))
	}
	prefix := "◆ "
	if !m.iconsEnabled() {
		prefix = "! "
	}
	row := renderFeedRow(prefix, "Needs You", badge, width)
	style := m.styles.FeedItem
	if selected {
		style = m.sidebarSelectedStyle("")
	}
	return style.Width(width).Render(row)
}

// rowBadgeIDs are the messages whose annotations a list row's badges show:
// the representative message, or in Needs You every message of a threaded
// row, so a thread shows the signals that put it there.
func (m Model) rowBadgeIDs(msg db.Message, thread messageThread) []int64 {
	if m.selectedNeedsYou() && m.threadedMessagesEnabled() && len(thread.Messages) > 1 {
		ids := make([]int64, len(thread.Messages))
		for i, t := range thread.Messages {
			ids[i] = t.ID
		}
		return ids
	}
	return []int64{msg.ID}
}
