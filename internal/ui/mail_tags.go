package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	imapClient "github.com/allisonhere/tidemail/internal/imap"
	"github.com/allisonhere/tidemail/internal/mailtag"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type mailTagUI struct {
	definitions   []mailtag.Definition
	editor        mailTagEditor
	busy          bool
	operation     uint64
	searchVersion uint64
}

type mailTagEditor struct {
	manager      bool
	accountIndex int
	accountID    int64
	message      db.Message
	cursor       int
	form         bool
	keyword      string
	input        textinput.Model
	error        string
}

type mailTagChangedMsg struct {
	operation            uint64
	messageID, mailboxID int64
	err                  error
	classification       *classificationOverrideMsg
}

func (m *Model) reloadMailTags() {
	if m.db == nil {
		return
	}
	definitions, err := m.db.ListMailTags()
	if err != nil {
		m.setStatus("mail tags: "+err.Error(), true)
		return
	}
	m.mailTags.definitions = definitions
}

func (m Model) accountMailTags(accountID int64) []mailtag.Definition {
	var out []mailtag.Definition
	for _, d := range m.mailTags.definitions {
		if d.AccountID == accountID {
			out = append(out, d)
		}
	}
	return out
}

func (m Model) mailTagsForMessage(msg db.Message) []annotationTag {
	mailbox := m.mailboxByID(msg.MailboxID)
	var accountID int64
	if mailbox != nil {
		accountID = mailbox.AccountID
	}
	definitions := m.accountMailTags(accountID)
	var out []annotationTag
	for _, keyword := range mailtag.Keywords(msg.Flags) {
		d := mailtag.Default(accountID, keyword)
		for _, known := range definitions {
			if strings.EqualFold(keyword, known.Keyword) {
				d = known
				break
			}
		}
		out = append(out, annotationTag{kind: tagMail, category: sanitizePluginLine(d.Name), color: d.Color})
	}
	return out
}

func (m *Model) prepareMessageMailTags(messageID int64) {
	m.reloadMailTags()
	e := mailTagEditor{}
	for _, msg := range m.messages {
		if msg.ID == messageID {
			e.message = msg
			break
		}
	}
	if m.db != nil {
		if msg, err := m.db.GetMessage(messageID); err == nil {
			e.message = msg
		}
	}
	if mailbox := m.mailboxByID(e.message.MailboxID); mailbox != nil {
		e.accountID = mailbox.AccountID
	}
	m.mailTags.editor = e
}

func (m Model) openMailTagSettings() (tea.Model, tea.Cmd) {
	m.reloadMailTags()
	e := mailTagEditor{manager: true}
	if len(m.accounts) > 0 {
		e.accountID = m.accounts[0].ID
	}
	m.mailTags.editor = e
	m.overlay = overlayMailTags
	return m, nil
}

func (m Model) handleMailTagKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	e := &m.mailTags.editor
	e.error = ""
	if e.form {
		switch key.Type {
		case tea.KeyEsc:
			e.form = false
		case tea.KeyEnter:
			value := strings.TrimSpace(e.input.Value())
			d := mailtag.Default(e.accountID, e.keyword)
			for _, known := range m.accountMailTags(e.accountID) {
				if strings.EqualFold(known.Keyword, e.keyword) {
					d = known
					break
				}
			}
			d.Name = value
			if m.db == nil {
				e.error = "No database is open."
				return m, nil
			}
			if err := m.db.SaveMailTag(d); err != nil {
				e.error = err.Error()
				return m, nil
			}
			e.form = false
			m.mailTags.searchVersion++
			m.reloadMailTags()
			m.setStatus("Tag mapping saved locally", false)
			return m, tea.Batch(m.clearStatusCmd(), m.refreshTagSearchCmd())
		default:
			var cmd tea.Cmd
			e.input, cmd = e.input.Update(key)
			return m, cmd
		}
		return m, nil
	}
	definitions := m.accountMailTags(e.accountID)
	switch {
	case keyMatches(key, m.keys.Cancel, m.keys.Back):
		m.overlay = overlaySettings
	case key.Type == tea.KeyLeft || key.Type == tea.KeyRight:
		if len(m.accounts) > 0 {
			delta := 1
			if key.Type == tea.KeyLeft {
				delta = -1
			}
			e.accountIndex = (e.accountIndex + len(m.accounts) + delta) % len(m.accounts)
			e.accountID = m.accounts[e.accountIndex].ID
			e.cursor = 0
		}
	case keyMatches(key, m.keys.Up):
		e.cursor = max(0, e.cursor-1)
	case keyMatches(key, m.keys.Down):
		e.cursor = min(max(0, len(definitions)-1), e.cursor+1)
	case key.String() == "e":
		if e.accountID == 0 {
			e.error = "Select an account first."
			return m, nil
		}
		if len(definitions) == 0 {
			return m, nil
		}
		e.form = true
		e.input = textinput.New()
		e.input.CharLimit = 80
		e.input.Width = max(1, min(m.width-12, 52))
		d := definitions[clamp(e.cursor, 0, len(definitions)-1)]
		e.keyword = d.Keyword
		e.input.SetValue(d.Name)
		e.input.Focus()
		return m, textinput.Blink
	case key.String() == "c" && len(definitions) > 0:
		d := definitions[clamp(e.cursor, 0, len(definitions)-1)]
		color := d.Color
		if color == "" {
			color = string(m.tagColorFor(tagKeyCategory).bg)
		}
		r, g, b, _ := parseHexRGB(color)
		return m.openColorPicker(m.overlay, d.Keyword, "bg", r, g, b)
	case key.String() == "r" && len(definitions) > 0:
		d := definitions[clamp(e.cursor, 0, len(definitions)-1)]
		d.Color = ""
		if m.db == nil {
			return m, nil
		}
		if err := m.db.SaveMailTag(d); err != nil {
			e.error = err.Error()
			return m, nil
		}
		m.reloadMailTags()

	}
	return m, nil
}

func (m *Model) changeMailTagCmd(message db.Message, keyword string, enabled bool, operation uint64) tea.Cmd {
	database, sessions := m.db, m.sessions
	account, cfgErr := m.accountCfgForMailbox(message.MailboxID)
	mailbox := m.mailboxByID(message.MailboxID)
	mailboxName := ""
	if mailbox != nil {
		mailboxName = mailbox.Name
	}
	return func() tea.Msg {
		result := mailTagChangedMsg{operation: operation, messageID: message.ID, mailboxID: message.MailboxID}
		if cfgErr != nil {
			result.err = cfgErr
			return result
		}
		if database == nil || sessions == nil || mailboxName == "" || account.IMAPHost == "" || message.UID == 0 {
			result.err = fmt.Errorf("a connected IMAP message is required to change tags")
			return result
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		result.err = sessions.Do(ctx, account, func(client *imapClient.Client) error {
			// Reload the row after acquiring the same lock as sync. A mailbox reset
			// or a move must not turn a stale editor target into a different message.
			current, err := database.GetMessage(message.ID)
			if err != nil {
				return err
			}
			if current.MailboxID != message.MailboxID || current.UID != message.UID {
				return fmt.Errorf("message changed; reopen its tag editor")
			}
			validity, err := database.MailboxUIDValidity(message.MailboxID)
			if err != nil {
				return err
			}
			flags, err := client.SetKeyword(ctx, mailboxName, message.UID, validity, keyword, enabled)
			if err != nil {
				return err
			}
			if err := database.ApplyServerFlags(message.MailboxID, map[uint32][]string{message.UID: flags}); err != nil {
				return fmt.Errorf("tag changed on server, but its local cache could not be saved: %w", err)
			}
			return nil
		})
		return result
	}
}

func (m Model) handleMailTagChanged(msg mailTagChangedMsg) (tea.Model, tea.Cmd) {
	if msg.operation != m.mailTags.operation {
		return m, nil
	}
	m.mailTags.busy = false
	m.mailTags.searchVersion++
	if msg.err != nil {
		m.setStatus("Mail tag: "+sanitizePluginLine(msg.err.Error()), true)
		if m.mailTags.editor.message.ID == msg.messageID && !m.mailTags.editor.manager {
			m.mailTags.editor.error = sanitizePluginLine(msg.err.Error())
		}
		return m, tea.Batch(m.clearStatusCmd(), m.syncMailboxCmd(msg.mailboxID, false))
	}
	m.reloadMailTags()
	if m.db != nil && m.mailTags.editor.message.ID == msg.messageID {
		if current, err := m.db.GetMessage(msg.messageID); err == nil {
			m.mailTags.editor.message = current
		}
	}
	if correction := msg.classification; correction != nil {
		if m.plugins.overrides == nil {
			m.plugins.overrides = map[int64]map[string]db.ClassificationOverride{}
		}
		m.plugins.overrides[correction.MessageID] = correction.Overrides
		if m.overlay == overlayClassification && m.classification.messageID == msg.messageID {
			m.classification.choice = 0
			m.classification.setChoices(m.effectiveClassification(msg.messageID))
		}
	}
	m.setStatus("Mail tag saved to server", false)
	return m, tea.Batch(m.clearStatusCmd(), m.visibleTagMessagesCmd(), m.needsYouRefreshCmd())
}

func (m *Model) refreshTagSearchCmd() tea.Cmd {
	if m.searchActive() {
		return m.searchAllMessagesCmd(m.searchQuery)
	}
	return nil
}

func (m *Model) saveMailTagColor(keyword, color string) error {
	for _, d := range m.accountMailTags(m.mailTags.editor.accountID) {
		if strings.EqualFold(d.Keyword, keyword) {
			d.Color = color
			if err := m.db.SaveMailTag(d); err != nil {
				return err
			}
			m.reloadMailTags()
			return nil
		}
	}
	return fmt.Errorf("tag mapping no longer exists")
}

func (m Model) renderMailTagsOverlay() string {
	width := max(1, min(m.width-4, 70))
	chrome := newManagerChrome(width, m.styles.Theme, m.styles.PlainUI)
	base := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.text)
	e := m.mailTags.editor
	lines := []string{"Tag names and colors"}
	accountName := "No account"
	if len(m.accounts) > 0 {
		accountName = sanitizePluginLine(m.accounts[e.accountIndex].Name)
	}
	lines = append(lines, "Account: "+accountName+"   (←→ changes account)")
	lines = append(lines, "Names and colors are local; assignments sync with IMAP.")
	if e.form {
		prompt := "Display name"
		lines = append(lines, "", prompt, e.input.View(), "", "enter save   esc cancel")
	} else {
		definitions := m.accountMailTags(e.accountID)
		visible := max(1, m.height-13)
		start := max(0, e.cursor-visible+1)
		end := min(len(definitions), start+visible)
		if len(definitions) == 0 {
			lines = append(lines, "", "No tags found. Sync your account to discover tags.")
		}
		for i := start; i < end; i++ {
			d := definitions[i]
			marker := "  "
			if i == e.cursor {
				marker = "> "
			}
			label := sanitizePluginLine(d.Name)
			style := m.tagStyle()
			if style == config.TagStyleGlyphPills || style == config.TagStyleIcons {
				style = config.TagStylePills
			}
			chip := chipFor(annotationTag{kind: tagMail, category: label, color: d.Color}, style, tagWide, m.iconsEnabled())
			chip.ends = m.tagEnds()
			chip.text = truncate(chip.text, max(1, width-12))
			lines = append(lines, marker+m.renderChips([]tagChip{chip}, base))
		}
		if end < len(definitions) {
			lines = append(lines, fmt.Sprintf("%d more below", len(definitions)-end))
		}
		lines = append(lines, "", "↑↓ select   e rename   c color   r theme color")
		lines = append(lines, "esc back to settings")
	}
	if m.mailTags.busy {
		lines = append(lines, "Saving tag to server…")
	}
	if e.error != "" {
		lines = append(lines, "", "Error: "+sanitizePluginLine(e.error))
	}
	for i, line := range lines {
		lines[i] = base.Render(truncate(line, max(1, width-4)))
	}
	body := base.Padding(1, 2).Width(width).Render(strings.Join(lines, "\n"))
	return renderSoftPanelBox(body, width, "tidemail", "mail tags", chrome)
}

func (m *Model) visibleTagMessagesCmd() tea.Cmd {
	if m.searchActive() {
		return m.searchAllMessagesCmd(m.searchQuery)
	}
	return m.visibleMessagesCmd()
}

func (m *Model) refreshMailTagTarget(messages []db.Message) {
	for _, message := range messages {
		if message.ID == m.mailTags.editor.message.ID {
			m.mailTags.editor.message = message
			e := &m.classification
			if m.overlay == overlayClassification && e.messageID == message.ID {
				selected := ""
				if e.choice >= 0 && e.choice < len(e.choices) {
					selected = e.choices[e.choice]
				}
				e.tags = m.accountMailTags(m.mailTags.editor.accountID)
				e.choice = 0
				e.setChoices(m.effectiveClassification(message.ID))
				for i, choice := range e.choices {
					if choice == selected {
						e.choice = i
						break
					}
				}
			}
			return
		}
	}
}

func (m Model) mergeMessageTags(tags []annotationTag, message db.Message) []annotationTag {
	merged := make([]annotationTag, 0, len(tags))
	for _, tag := range tags {
		if tag.kind == tagCategory && categoryKeyword(tag.category) != "" && mailtag.Has(message.Flags, categoryKeyword(tag.category)) {
			continue
		}
		merged = append(merged, tag)
	}
	return append(merged, m.mailTagsForMessage(message)...)
}
