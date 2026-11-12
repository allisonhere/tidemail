package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Right-click context menu. A right-click selects the row under the pointer
// (the same selection a left-click makes) and opens a small menu at the
// pointer holding only the actions that make sense for what was clicked.
// Every item is a command-palette id, so choosing one runs executeCommand and
// behaves exactly like the palette entry.

type contextMenu struct {
	x, y   int // top-left of the drawn box, already clamped to the screen
	w      int // box width including borders
	items  []commandItem
	cursor int
}

const contextMenuMinInner = 18

// contextMenuItems returns the menu entries for the row the right-click just
// selected in the given pane, or nil when there is nothing useful to offer.
func (m Model) contextMenuItems(target pane) []commandItem {
	switch target {
	case paneAccounts:
		return m.sidebarContextItems()
	case paneMessages:
		return m.messageContextItems()
	default:
		if m.currentContentMessage() == nil {
			return nil
		}
		return []commandItem{
			{id: "reply", label: "Reply", enabled: true},
			{id: "reply-all", label: "Reply all", enabled: true},
			{id: "forward", label: "Forward", enabled: true},
			{id: "archive", label: "Archive", enabled: true},
			{id: "delete", label: "Delete", enabled: true},
		}
	}
}

func (m Model) sidebarContextItems() []commandItem {
	if m.sidebarCursor < 0 || m.sidebarCursor >= len(m.sidebarRows) {
		return nil
	}
	switch row := m.sidebarRows[m.sidebarCursor]; row.kind {
	case rowKindMailbox:
		hide := "Hide folder"
		if mb := m.mailboxByID(row.mailboxID); mb != nil && m.mailboxPrefs[mb.AccountID][mb.Name].Hidden {
			hide = "Unhide folder"
		}
		return []commandItem{
			{id: "sync", label: "Sync folder", enabled: true},
			{id: "new-folder", label: "New folder", enabled: true},
			{id: "rename-folder", label: "Rename folder", enabled: true},
			{id: "hide-folder", label: hide, enabled: true},
			{id: "delete-folder", label: "Delete folder", enabled: true},
		}
	case rowKindAccount:
		return []commandItem{
			{id: "new-folder", label: "New folder", enabled: true},
			{id: "sync-all", label: "Sync all mailboxes", enabled: len(m.mailboxes) > 0},
			{id: "accounts", label: "Manage accounts", enabled: true},
		}
	case rowKindOutbox:
		return []commandItem{{id: "outbox", label: "Open Outbox", enabled: true}}
	default:
		return []commandItem{{id: "sync-all", label: "Sync all mailboxes", enabled: len(m.mailboxes) > 0}}
	}
}

func (m Model) messageContextItems() []commandItem {
	msg := m.commandMessage()
	if msg == nil {
		return nil
	}
	read := "Mark as read"
	if msg.Read {
		read = "Mark as unread"
	}
	items := []commandItem{
		{id: "reply", label: "Reply", enabled: true},
		{id: "reply-all", label: "Reply all", enabled: true},
		{id: "forward", label: "Forward", enabled: true},
		{id: "archive", label: "Archive", enabled: true},
		{id: "move", label: "Move…", enabled: true},
		{id: "delete", label: "Delete", enabled: true},
		{id: "toggle-read", label: read, enabled: true},
	}
	return append(items, m.snoozeCommandItems()...)
}

// openContextMenu shows items with its top-left corner at (x, y), shifted
// left/up so the whole box stays on screen. It reports false when there are
// no items to show.
func (m *Model) openContextMenu(x, y int, items []commandItem) bool {
	if len(items) == 0 {
		return false
	}
	inner := contextMenuMinInner
	for _, it := range items {
		inner = max(inner, lipgloss.Width(it.label)+4)
	}
	w, h := inner+2, len(items)+2
	cm := contextMenu{
		x:     clamp(x, 0, max(0, m.width-w)),
		y:     clamp(y, 0, max(0, m.height-h)),
		w:     w,
		items: items,
	}
	for i, it := range items {
		if it.enabled {
			cm.cursor = i
			break
		}
	}
	m.ctxMenu = cm
	m.overlay = overlayContextMenu
	return true
}

func (m *Model) closeContextMenu() {
	m.ctxMenu = contextMenu{}
	m.overlay = overlayNone
}

// runContextMenuItem closes the menu and runs the item at idx, ignoring
// disabled entries.
func (m Model) runContextMenuItem(idx int) (tea.Model, tea.Cmd) {
	if idx < 0 || idx >= len(m.ctxMenu.items) || !m.ctxMenu.items[idx].enabled {
		return m, nil
	}
	id := m.ctxMenu.items[idx].id
	m.closeContextMenu()
	return m.executeCommand(id)
}

func (m *Model) stepContextMenu(delta int) {
	n := len(m.ctxMenu.items)
	for i := 1; i <= n; i++ {
		next := ((m.ctxMenu.cursor+delta*i)%n + n) % n
		if m.ctxMenu.items[next].enabled {
			m.ctxMenu.cursor = next
			return
		}
	}
}

func (m Model) handleContextMenuKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case keyMatches(msg, m.keys.Cancel):
		m.closeContextMenu()
	case keyMatches(msg, m.keys.Up):
		m.stepContextMenu(-1)
	case keyMatches(msg, m.keys.Down), keyMatches(msg, m.keys.Tab):
		m.stepContextMenu(1)
	case keyMatches(msg, m.keys.Confirm):
		return m.runContextMenuItem(m.ctxMenu.cursor)
	}
	return m, nil
}

// handleContextMenuClick runs the item under a left-click, or dismisses the
// menu when the click lands anywhere else (the click is swallowed).
func (m Model) handleContextMenuClick(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	cm := m.ctxMenu
	row := msg.Y - cm.y - 1 // the box's top border is the first row
	inside := msg.X > cm.x && msg.X < cm.x+cm.w-1
	if inside && row >= 0 && row < len(cm.items) {
		return m.runContextMenuItem(row)
	}
	m.closeContextMenu()
	return m, nil
}

func (m Model) renderContextMenu(base string) string {
	cm := m.ctxMenu
	inner := cm.w - 2
	chrome := newManagerChrome(inner, m.styles.Theme, m.styles.PlainUI)
	rows := make([]string, 0, len(cm.items))
	for i, it := range cm.items {
		selected := i == cm.cursor
		fg := chrome.text
		if !it.enabled {
			fg = chrome.muted
		}
		label := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(fg).Render(" " + truncate(it.label, max(1, inner-3)))
		rows = append(rows, softRail(chrome, selected, chrome.baseBg)+padStyled(label, inner-2, chrome.baseBg))
	}
	border := lipgloss.RoundedBorder()
	if chrome.plainUI {
		border = lipgloss.ASCIIBorder()
	}
	box := lipgloss.NewStyle().
		Background(chrome.baseBg).
		Border(border).
		BorderForeground(chrome.border).
		BorderBackground(chrome.baseBg).
		Render(strings.Join(rows, "\n"))
	return overlayAt(base, box, cm.x, cm.y, m.width, m.height, m.styles.Theme.Bg, m.cfg.Display.Shadow)
}
