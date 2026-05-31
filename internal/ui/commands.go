package ui

import (
	"strings"

	"github.com/allisonhere/tide/internal/config"
	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) handleCommandPaletteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := m.filteredCommandItems()
	switch {
	case keyMatches(msg, m.keys.Cancel):
		m.overlay = overlayNone
		m.commandInput.Blur()
		return m, nil
	case keyMatches(msg, m.keys.Up):
		if len(items) > 0 {
			m.commandCursor = (m.commandCursor - 1 + len(items)) % len(items)
		}
		return m, nil
	case keyMatches(msg, m.keys.Down), keyMatches(msg, m.keys.Tab):
		if len(items) > 0 {
			m.commandCursor = (m.commandCursor + 1) % len(items)
		}
		return m, nil
	case keyMatches(msg, m.keys.Confirm):
		if len(items) == 0 {
			return m, nil
		}
		item := items[clamp(m.commandCursor, 0, len(items)-1)]
		if !item.enabled {
			return m, nil
		}
		m.overlay = overlayNone
		m.commandInput.Blur()
		return m.executeCommand(item.id)
	default:
		var cmd tea.Cmd
		m.commandInput, cmd = m.commandInput.Update(msg)
		items = m.filteredCommandItems()
		m.commandCursor = clamp(m.commandCursor, 0, max(0, len(items)-1))
		return m, cmd
	}
}

func (m Model) commandItems() []commandItem {
	hasMessage := len(m.filteredMessages) > 0 && m.focused != paneAccounts
	hasMailbox := m.selectedMailbox() != nil
	return []commandItem{
		{id: "compose", label: "Compose new message", enabled: len(m.cfg.Accounts) > 0},
		{id: "reply", label: "Reply to current message", enabled: m.contentMessageID != 0 || hasMessage},
		{id: "forward", label: "Forward current message", enabled: m.contentMessageID != 0 || hasMessage},
		{id: "archive", label: "Archive current message", enabled: hasMessage},
		{id: "move", label: "Move current message", enabled: hasMessage},
		{id: "delete", label: "Delete current message", enabled: hasMessage},
		{id: "toggle-read", label: "Toggle read/unread", enabled: hasMessage},
		{id: "sync", label: "Sync current mailbox", enabled: hasMailbox},
		{id: "sync-all", label: "Sync all mailboxes", enabled: len(m.mailboxes) > 0},
		{id: "accounts", label: "Manage accounts", enabled: true},
		{id: "settings", label: "Open settings", enabled: true},
	}
}

func (m Model) filteredCommandItems() []commandItem {
	q := strings.ToLower(strings.TrimSpace(m.commandInput.Value()))
	items := m.commandItems()
	if q == "" {
		return items
	}
	filtered := make([]commandItem, 0, len(items))
	for _, item := range items {
		if strings.Contains(strings.ToLower(item.label), q) || strings.Contains(strings.ToLower(item.id), q) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func (m Model) executeCommand(id string) (tea.Model, tea.Cmd) {
	switch id {
	case "compose":
		var acfg config.AccountConfig
		if len(m.cfg.Accounts) > 0 {
			acfg = m.cfg.Accounts[0]
		}
		m.compose = NewCompose(acfg, m.cfg.Accounts, m.addressBook)
		m.overlay = overlayCompose
		return m, nil
	case "reply":
		msg := m.commandMessage()
		if msg == nil {
			return m, nil
		}
		acfg := m.accountCfgForMailbox(msg.MailboxID)
		m.compose = NewReply(*msg, acfg, m.cfg.Accounts)
		m.overlay = overlayCompose
		return m, nil
	case "forward":
		msg := m.commandMessage()
		if msg == nil {
			return m, nil
		}
		acfg := m.accountCfgForMailbox(msg.MailboxID)
		m.compose = NewForward(*msg, acfg, m.cfg.Accounts)
		m.overlay = overlayCompose
		return m, nil
	case "archive":
		if msg := m.commandMessage(); msg != nil {
			return m, m.archiveMessageCmd(*msg)
		}
	case "move":
		m.openMovePicker(m.movePickerMessages())
		return m, nil
	case "delete":
		if msg := m.commandMessage(); msg != nil {
			return m, m.deleteMessageCmd(*msg)
		}
	case "toggle-read":
		if msg := m.commandMessage(); msg != nil {
			return m, m.setMessageReadCmd(*msg, !msg.Read, !msg.Read)
		}
	case "sync":
		if selected := m.selectedMailbox(); selected != nil {
			return m, m.syncMailboxCmd(selected.ID, true)
		}
	case "sync-all":
		var cmds []tea.Cmd
		for _, mb := range m.mailboxes {
			cmds = append(cmds, m.syncMailboxCmd(mb.ID, false))
		}
		return m, tea.Batch(cmds...)
	case "accounts":
		m.overlay = overlayAccountManager
		m.accountManager = m.newAccountManager()
		return m, nil
	case "settings":
		m.settings = newSettings(m.cfg, m.settingsUpdateState())
		m.overlay = overlaySettings
		return m, nil
	}
	return m, nil
}
