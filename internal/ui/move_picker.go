package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	imapClient "github.com/allisonhere/tide/internal/imap"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type movePicker struct {
	entries     []moveEntry
	cursor      int
	accountID   int64
	accountName string
	currentPath string
	messages    []db.Message
	sources     map[int64]bool
	creating    bool
	nameInput   textinput.Model
}

type moveEntry struct {
	label     string
	path      string
	mailboxID int64
	isDir     bool
	isConfirm bool
}

func (m *Model) openMovePicker(messages []db.Message) {
	if len(messages) == 0 {
		return
	}
	firstMailbox := m.mailboxByID(messages[0].MailboxID)
	if firstMailbox == nil {
		m.setStatus("move failed: mailbox not found", true)
		return
	}
	accountID := firstMailbox.AccountID
	for _, msg := range messages[1:] {
		mb := m.mailboxByID(msg.MailboxID)
		if mb == nil || mb.AccountID != accountID {
			m.setStatus("move failed: select messages from one account", true)
			return
		}
	}
	sources := make(map[int64]bool, len(messages))
	for _, msg := range messages {
		sources[msg.MailboxID] = true
	}
	accountName := m.accountName(accountID)
	m.movePicker = movePicker{
		accountID:   accountID,
		accountName: accountName,
		messages:    append([]db.Message(nil), messages...),
		sources:     sources,
	}
	m.refreshMovePickerEntries()
	if len(m.movePicker.entries) == 0 {
		m.setStatus("move failed: no folders available", true)
		m.movePicker = movePicker{}
		return
	}
	m.overlay = overlayMoveMessage
}

func (m *Model) movePickerMessages() []db.Message {
	if m.hasSelection() {
		return m.selectedActionMessages()
	}
	return m.currentRowMessages()
}

func (m *Model) refreshMovePickerEntries() {
	m.movePicker.entries = buildMoveEntries(m.mailboxes, m.movePicker.accountID, m.movePicker.currentPath, m.movePicker.sources)
	m.movePicker.cursor = clamp(m.movePicker.cursor, 0, max(0, len(m.movePicker.entries)-1))
}

func buildMoveEntries(mailboxes []db.Mailbox, accountID int64, currentPath string, sources map[int64]bool) []moveEntry {
	delimiter := moveDelimiter(mailboxes, accountID, currentPath)
	var entries []moveEntry
	if currentPath != "" {
		if mb, ok := exactMoveMailbox(mailboxes, accountID, currentPath); ok && !sources[mb.ID] && !hasFlag(mb.Flags, "\\Noselect") {
			entries = append(entries, moveEntry{label: "move here", mailboxID: mb.ID, isConfirm: true})
		}
		entries = append(entries, moveEntry{label: "..", path: moveParentPath(currentPath, delimiter), isDir: true})
	}

	childByName := make(map[string]moveEntry)
	childHasChildren := make(map[string]bool)
	childSelectable := make(map[string]bool)
	for _, mb := range mailboxes {
		if mb.AccountID != accountID || mb.Name == "" {
			continue
		}
		parts := splitMovePath(mb.Name, moveMailboxDelimiter(mb))
		currentParts := splitMovePath(currentPath, delimiter)
		if !sameMovePrefix(parts, currentParts) || len(parts) <= len(currentParts) {
			continue
		}
		childName := parts[len(currentParts)]
		childPath := strings.Join(parts[:len(currentParts)+1], moveMailboxDelimiter(mb))
		key := strings.ToLower(childName)
		entry := childByName[strings.ToLower(childName)]
		entry.label = childName
		entry.path = childPath
		entry.isDir = true
		if len(parts) == len(currentParts)+1 {
			entry.mailboxID = mb.ID
			if !sources[mb.ID] && !hasFlag(mb.Flags, "\\Noselect") {
				childSelectable[key] = true
			}
		} else {
			childHasChildren[key] = true
		}
		childByName[key] = entry
	}
	keys := make([]string, 0, len(childByName))
	for key := range childByName {
		if childSelectable[key] || childHasChildren[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		entries = append(entries, childByName[key])
	}
	return entries
}

func moveDelimiter(mailboxes []db.Mailbox, accountID int64, currentPath string) string {
	if currentPath != "" {
		if mb, ok := exactMoveMailbox(mailboxes, accountID, currentPath); ok {
			return moveMailboxDelimiter(mb)
		}
	}
	for _, mb := range mailboxes {
		if mb.AccountID == accountID && mb.Delimiter != "" {
			return mb.Delimiter
		}
	}
	return "/"
}

func moveMailboxDelimiter(mb db.Mailbox) string {
	if mb.Delimiter != "" {
		return mb.Delimiter
	}
	return "/"
}

func exactMoveMailbox(mailboxes []db.Mailbox, accountID int64, path string) (db.Mailbox, bool) {
	for _, mb := range mailboxes {
		if mb.AccountID == accountID && mb.Name == path {
			return mb, true
		}
	}
	return db.Mailbox{}, false
}

func splitMovePath(path, delimiter string) []string {
	if path == "" {
		return nil
	}
	if delimiter == "" {
		delimiter = "/"
	}
	parts := strings.Split(path, delimiter)
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func sameMovePrefix(parts, prefix []string) bool {
	if len(prefix) > len(parts) {
		return false
	}
	for i := range prefix {
		if parts[i] != prefix[i] {
			return false
		}
	}
	return true
}

func moveParentPath(path, delimiter string) string {
	parts := splitMovePath(path, delimiter)
	if len(parts) <= 1 {
		return ""
	}
	return strings.Join(parts[:len(parts)-1], delimiter)
}

func (m Model) handleMovePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.movePicker.creating {
		switch {
		case keyMatches(msg, m.keys.Cancel):
			m.movePicker.creating = false
			m.movePicker.nameInput.Blur()
			return m, nil
		case keyMatches(msg, m.keys.Confirm):
			name := strings.TrimSpace(m.movePicker.nameInput.Value())
			m.movePicker.creating = false
			m.movePicker.nameInput.Blur()
			if name == "" {
				return m, nil
			}
			return m, m.createFolderCmd(m.movePicker.accountID, m.movePicker.currentPath, name)
		default:
			var cmd tea.Cmd
			m.movePicker.nameInput, cmd = m.movePicker.nameInput.Update(msg)
			return m, cmd
		}
	}
	switch {
	case keyMatches(msg, m.keys.Cancel):
		m.movePicker = movePicker{}
		m.overlay = overlayNone
		return m, nil
	case keyMatches(msg, m.keys.Up):
		if m.movePicker.cursor > 0 {
			m.movePicker.cursor--
		}
		return m, nil
	case keyMatches(msg, m.keys.Down):
		if m.movePicker.cursor < len(m.movePicker.entries)-1 {
			m.movePicker.cursor++
		}
		return m, nil
	case keyMatches(msg, m.keys.Confirm):
		if len(m.movePicker.entries) == 0 {
			return m, nil
		}
		entry := m.movePicker.entries[m.movePicker.cursor]
		if entry.isConfirm {
			return m.confirmMovePicker(entry.mailboxID)
		}
		if entry.isDir {
			m.movePicker.currentPath = entry.path
			m.movePicker.cursor = 0
			m.refreshMovePickerEntries()
		}
		return m, nil
	case keyMatches(msg, m.keys.Left), keyMatches(msg, m.keys.Back):
		if m.movePicker.currentPath == "" {
			m.movePicker = movePicker{}
			m.overlay = overlayNone
			return m, nil
		}
		m.movePicker.currentPath = moveParentPath(m.movePicker.currentPath, moveDelimiter(m.mailboxes, m.movePicker.accountID, m.movePicker.currentPath))
		m.movePicker.cursor = 0
		m.refreshMovePickerEntries()
		return m, nil
	case msg.String() == "n":
		input := textinput.New()
		input.Placeholder = "folder name"
		input.Focus()
		m.movePicker.nameInput = input
		m.movePicker.creating = true
		return m, nil
	default:
		key := msg.String()
		if len(key) == 1 && ((key >= "a" && key <= "z") || (key >= "A" && key <= "Z")) {
			lower := strings.ToLower(key)
			for i, e := range m.movePicker.entries {
				if strings.HasPrefix(strings.ToLower(e.label), lower) {
					m.movePicker.cursor = i
					return m, nil
				}
			}
		}
		return m, nil
	}
}

func (m Model) confirmMovePicker(targetMailboxID int64) (tea.Model, tea.Cmd) {
	target := m.mailboxByID(targetMailboxID)
	if target == nil {
		m.setStatus("move failed: target mailbox not found", true)
		return m, m.clearStatusCmd()
	}
	msgs := append([]db.Message(nil), m.movePicker.messages...)
	m.movePicker = movePicker{}
	m.overlay = overlayNone
	m.clearSelection()
	cmds := make([]tea.Cmd, 0, len(msgs))
	for _, msg := range msgs {
		cmds = append(cmds, m.moveMessageToMailboxCmd(msg, *target))
	}
	return m, tea.Batch(cmds...)
}

func (m Model) renderMovePicker(width, height int, chrome managerChrome) string {
	header := renderManagerHeader("MOVE TO FOLDER", width, chrome)
	path := m.movePicker.accountName
	if m.movePicker.currentPath != "" {
		path += " / " + m.movePicker.currentPath
	}
	pathLine := lipgloss.NewStyle().
		Background(chrome.baseBg).
		Foreground(chrome.muted).
		Width(width).
		Padding(0, 2).
		Render(clampView(path, width-2, 1, chrome.baseBg))

	listH := max(1, height-7)

	if m.movePicker.creating {
		prompt := lipgloss.NewStyle().
			Background(chrome.baseBg).
			Foreground(chrome.text).
			Width(width).
			Padding(0, 2).
			Render(clampView("new folder: "+m.movePicker.nameInput.View(), max(1, width-4), 1, chrome.baseBg))
		rows := []string{prompt}
		for len(rows) < listH {
			rows = append(rows, lipgloss.NewStyle().Background(chrome.baseBg).Width(width).Padding(0, 2).Render(""))
		}
		actions := renderManagerActions(width, chrome,
			"enter", "create",
			"esc", "cancel",
		)
		return lipgloss.JoinVertical(lipgloss.Left, header, pathLine, lipgloss.JoinVertical(lipgloss.Left, rows...), actions)
	}

	start := 0
	if m.movePicker.cursor >= listH {
		start = m.movePicker.cursor - listH + 1
	}
	end := min(start+listH, len(m.movePicker.entries))
	visible := m.movePicker.entries[start:end]

	rows := make([]string, 0, listH)
	for i, e := range visible {
		idx := start + i
		cursor := idx == m.movePicker.cursor
		entryStyle := lipgloss.NewStyle().Background(chrome.baseBg).Width(width).Padding(0, 2)
		if cursor {
			entryStyle = entryStyle.Background(chrome.accent).Foreground(accentReadableOn(chrome.text, chrome.accent, 4.5))
		} else {
			entryStyle = entryStyle.Foreground(chrome.text)
		}
		label := e.label
		switch {
		case e.isConfirm:
			label = "move here"
		case e.label == "..":
			label = "../"
		case e.isDir:
			label = e.label + "/"
		}
		label = clampView(label, max(1, width-4), 1, chrome.baseBg)
		rows = append(rows, entryStyle.Render(label))
	}
	for len(rows) < listH {
		rows = append(rows, lipgloss.NewStyle().Background(chrome.baseBg).Width(width).Padding(0, 2).Render(""))
	}

	actions := renderManagerActions(width, chrome,
		"enter", "open/move",
		"n", "new folder",
		"h", "parent",
		"esc", "cancel",
	)
	return lipgloss.JoinVertical(lipgloss.Left, header, pathLine, lipgloss.JoinVertical(lipgloss.Left, rows...), actions)
}

func (m *Model) createFolderCmd(accountID int64, parentPath, name string) tea.Cmd {
	delimiter := moveDelimiter(m.mailboxes, accountID, parentPath)
	fullName := name
	if parentPath != "" {
		fullName = parentPath + delimiter + name
	}
	var acfg config.AccountConfig
	if len(m.movePicker.messages) > 0 {
		acfg = m.accountCfgForMailbox(m.movePicker.messages[0].MailboxID)
	}
	database := m.db
	sessions := m.sessions
	return func() tea.Msg {
		if acfg.IMAPHost != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := sessions.Do(ctx, acfg, func(client *imapClient.Client) error {
				return client.CreateMailbox(ctx, fullName)
			}); err != nil {
				return FolderCreatedMsg{AccountID: accountID, Name: fullName, Err: err}
			}
		}
		id, err := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: fullName, Delimiter: delimiter})
		if err != nil {
			return FolderCreatedMsg{AccountID: accountID, Name: fullName, Err: err}
		}
		return FolderCreatedMsg{AccountID: accountID, MailboxID: id, Name: fullName, Delimiter: delimiter}
	}
}

func (m *Model) moveMessageToMailboxCmd(msg db.Message, target db.Mailbox) tea.Cmd {
	database := m.db
	sessions := m.sessions
	source := m.mailboxByID(msg.MailboxID)
	acfg := m.accountCfgForMailbox(msg.MailboxID)
	return func() tea.Msg {
		if source == nil {
			return MessageMovedMsg{MessageID: msg.ID, FromMailboxID: msg.MailboxID, ToMailboxID: target.ID, Action: "move", Err: fmt.Errorf("mailbox not found")}
		}
		if source.AccountID != target.AccountID {
			return MessageMovedMsg{MessageID: msg.ID, FromMailboxID: msg.MailboxID, ToMailboxID: target.ID, Action: "move", Err: fmt.Errorf("target mailbox is in another account")}
		}
		if acfg.IMAPHost != "" && msg.UID != 0 {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			if err := sessions.Do(ctx, acfg, func(client *imapClient.Client) error {
				return client.MoveMessage(ctx, source.Name, msg.UID, target.Name)
			}); err != nil {
				return MessageMovedMsg{MessageID: msg.ID, FromMailboxID: msg.MailboxID, ToMailboxID: target.ID, Action: "move", Err: err}
			}
		}
		if err := database.MoveMessage(msg.ID, target.ID); err != nil {
			return MessageMovedMsg{MessageID: msg.ID, FromMailboxID: msg.MailboxID, ToMailboxID: target.ID, Action: "move", Err: err}
		}
		return MessageMovedMsg{MessageID: msg.ID, FromMailboxID: msg.MailboxID, ToMailboxID: target.ID, Action: "move"}
	}
}
