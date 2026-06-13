package ui

import (
	"path/filepath"
	"strings"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) handleCommandPaletteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := m.filteredCommandItems()
	switch {
	case keyMatches(msg, m.keys.Cancel):
		m.closeCommandPalette(false)
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
		m.closeCommandPalette(true)
		return m.executeCommand(item.id)
	default:
		var cmd tea.Cmd
		m.commandInput, cmd = m.commandInput.Update(msg)
		items = m.filteredCommandItems()
		m.commandCursor = clamp(m.commandCursor, 0, max(0, len(items)-1))
		return m, cmd
	}
}

func (m *Model) openCommandPalette(origin overlayMode, ctx commandPaletteContext) {
	m.commandPaletteOrigin = origin
	m.commandPaletteContext = ctx
	m.overlay = overlayCommandPalette
	m.commandInput.Reset()
	m.commandInput.Focus()
	m.commandCursor = 0
}

func (m *Model) closeCommandPalette(run bool) {
	m.commandInput.Blur()
	origin := m.commandPaletteOrigin
	m.commandPaletteOrigin = overlayNone
	m.commandPaletteContext = commandPaletteMain
	if run {
		m.overlay = origin
		return
	}
	if origin != overlayNone {
		m.overlay = origin
		return
	}
	m.overlay = overlayNone
}

func (m Model) commandItems() []commandItem {
	switch m.commandPaletteContext {
	case commandPaletteCompose:
		return m.composeCommandItems()
	case commandPaletteSummary:
		return m.summaryCommandItems()
	case commandPaletteSaveAttach:
		return m.saveAttachCommandItems()
	default:
		return m.mainCommandItems()
	}
}

func (m Model) mainCommandItems() []commandItem {
	hasMessage := m.activeMessageRowCount() > 0 && m.focused != paneAccounts
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
		{id: "filters", label: "Manage filters (AI rules)", enabled: true},
		{id: "settings", label: "Open settings", enabled: true},
	}
}

func (m Model) composeCommandItems() []commandItem {
	canRemove := len(m.compose.attachments) > 0
	canCycleSender := len(m.compose.accounts) > 1
	return []commandItem{
		{id: "compose-send", label: "Send message", enabled: true},
		{id: "compose-grammar", label: "Run grammar check", enabled: true},
		{id: "compose-attach", label: "Attach file", enabled: true},
		{id: "compose-remove-attach", label: "Remove last attachment", enabled: canRemove},
		{id: "compose-cycle-sender", label: "Change sender", enabled: canCycleSender},
		{id: "compose-close", label: "Close compose", enabled: true},
	}
}

func (m Model) summaryCommandItems() []commandItem {
	ready := !m.summaryGenerating && m.summaryErr == "" && m.summaryMessage.Summary != ""
	return []commandItem{
		{id: "summary-copy", label: "Copy summary", enabled: ready},
		{id: "summary-save", label: "Save summary as .md", enabled: ready},
		{id: "summary-close", label: "Close summary", enabled: true},
	}
}

func (m Model) saveAttachCommandItems() []commandItem {
	canSave := len(m.contentAttachments) > 0 && m.saveAttachPicker.currentDir != ""
	canUp := m.saveAttachPicker.currentDir != "" && filepath.Dir(m.saveAttachPicker.currentDir) != m.saveAttachPicker.currentDir
	return []commandItem{
		{id: "save-attach-save", label: "Save to current folder", enabled: canSave},
		{id: "save-attach-up", label: "Go to parent folder", enabled: canUp},
		{id: "save-attach-cancel", label: "Cancel", enabled: true},
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
			return m, m.deleteMessagesCmd([]db.Message{*msg})
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
	case "filters":
		m.filterManager = m.newFilterManager()
		m.overlay = overlayFilterManager
		return m, nil
	case "settings":
		m.settings = newSettings(m.cfg, m.settingsUpdateState())
		m.overlay = overlaySettings
		return m, nil
	case "compose-send":
		return m.handleCompose(tea.KeyMsg{Type: tea.KeyCtrlS})
	case "compose-grammar":
		return m.handleCompose(tea.KeyMsg{Type: tea.KeyCtrlG})
	case "compose-attach":
		return m.handleCompose(tea.KeyMsg{Type: tea.KeyCtrlA})
	case "compose-remove-attach":
		return m.handleCompose(tea.KeyMsg{Type: tea.KeyCtrlR})
	case "compose-cycle-sender":
		return m.handleCompose(tea.KeyMsg{Type: tea.KeyCtrlU})
	case "compose-close":
		return m.handleCompose(tea.KeyMsg{Type: tea.KeyEsc})
	case "summary-copy":
		return m.handleSummaryKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'C'}})
	case "summary-save":
		return m.handleSummaryKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}})
	case "summary-close":
		return m.handleSummaryKey(tea.KeyMsg{Type: tea.KeyEsc})
	case "save-attach-save":
		m.saveAttachPicker.active = false
		m.overlay = overlayNone
		return m, saveAttachmentsCmdTo(m.contentAttachments, m.saveAttachPicker.currentDir)
	case "save-attach-up":
		m.saveAttachPickerUpDir()
		return m, nil
	case "save-attach-cancel":
		m.saveAttachPicker.active = false
		m.overlay = overlayNone
		return m, nil
	}
	return m, nil
}
