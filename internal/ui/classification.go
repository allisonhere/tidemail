package ui

import (
	"fmt"
	"strings"

	"github.com/allisonhere/tidemail/internal/attention"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/mailtag"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type classificationField int

const (
	classificationNeedsReply classificationField = iota
	classificationUrgency
	classificationImportance
	classificationCategory
)

type classificationEditor struct {
	messageID int64
	field     classificationField
	choice    int
	choices   []string
	tags      []mailtag.Definition
	custom    bool
	input     textinput.Model
}

var classificationFieldNames = []string{"Needs reply", "Urgency", "Importance", "Category"}
var categoryChoices = []string{"plugin", "none", "github", "receipt", "shipping", "security", "calendar", "newsletter", "support", "social", "billing", "notification", "personal", "important", "work", "to-do", "later", "custom"}

func (m Model) openClassificationEditor() (tea.Model, tea.Cmd) {
	msg := m.commandMessage()
	if msg == nil {
		return m, nil
	}
	return m.openClassificationEditorFor(msg.ID)
}

func (m Model) openClassificationEditorFor(messageID int64) (tea.Model, tea.Cmd) {
	m.classification = classificationEditor{messageID: messageID}
	m.prepareMessageMailTags(messageID)
	m.classification.tags = m.accountMailTags(m.mailTags.editor.accountID)
	m.classification.setChoices(m.effectiveClassification(messageID))
	m.overlay = overlayClassification
	return m, nil
}

func (e *classificationEditor) setChoices(c attention.EffectiveClassification) {
	switch e.field {
	case classificationNeedsReply:
		e.choices = []string{"plugin", "true", "false"}
		if c.NeedsReplySource == attention.SourceUser && c.NeedsReply {
			e.choice = 1
		}
		if c.NeedsReplySource == attention.SourceUser && !c.NeedsReply {
			e.choice = 2
		}
	case classificationUrgency:
		e.choices = []string{"plugin", "high", "normal"}
		if c.UrgencySource == attention.SourceUser && c.Urgent {
			e.choice = 1
		}
		if c.UrgencySource == attention.SourceUser && !c.Urgent {
			e.choice = 2
		}
	case classificationImportance:
		e.choices = []string{"plugin", "high", "normal"}
		if c.ImportanceSource == attention.SourceUser && c.Important {
			e.choice = 1
		}
		if c.ImportanceSource == attention.SourceUser && !c.Important {
			e.choice = 2
		}
	case classificationCategory:
		e.choices = append([]string(nil), categoryChoices[:len(categoryChoices)-1]...)
		for _, tag := range e.tags {
			if !isDefaultTag(tag.Keyword) {
				e.choices = append(e.choices, "tag:"+tag.Keyword)
			}
		}
		e.choices = append(e.choices, "custom")
		value := c.Category
		if c.CategorySource == attention.SourceUser && value == "" {
			value = db.ClassificationNone
		}
		for i, choice := range e.choices {
			if choice == value {
				e.choice = i
			}
		}
		if value != "" && value != db.ClassificationPlugin && value != db.ClassificationNone {
			known := false
			for _, choice := range categoryChoices {
				if choice == value {
					known = true
					break
				}
			}
			if !known {
				e.choice = len(e.choices) - 1
			}
		}
	}
}

func (e *classificationEditor) key() string {
	switch e.field {
	case classificationNeedsReply:
		return db.ClassificationNeedsReply
	case classificationUrgency:
		return db.ClassificationUrgency
	case classificationImportance:
		return db.ClassificationImportance
	default:
		return db.ClassificationCategory
	}
}

func (e *classificationEditor) displayChoice(choice string) string {
	for _, tag := range e.tags {
		if keyword := categoryKeyword(choice); keyword != "" && strings.EqualFold(keyword, tag.Keyword) {
			return sanitizePluginLine(tag.Name)
		}
	}
	switch choice {
	case "plugin":
		return "Use plugin decision"
	case "true":
		return "Yes"
	case "false":
		return "No"
	case "normal":
		return "Normal"
	case "none":
		return "No category"
	case "to-do":
		return "To Do"
	case "custom":
		return "Custom…"
	}
	return strings.ToUpper(choice[:1]) + choice[1:]
}

type classificationOverrideMsg struct {
	MessageID int64
	Overrides map[string]db.ClassificationOverride
	Err       error
}

func classificationOverrideCmd(database *db.DB, messageID int64, key, value string, resetAll bool) tea.Cmd {
	return func() tea.Msg {
		if database == nil {
			return classificationOverrideMsg{Err: fmt.Errorf("no database")}
		}
		var err error
		if resetAll {
			err = database.DeleteClassificationOverrides(messageID)
		} else if value == db.ClassificationPlugin {
			err = database.DeleteClassificationOverride(messageID, key)
		} else {
			err = database.SetClassificationOverride(messageID, key, value)
		}
		if err != nil {
			return classificationOverrideMsg{MessageID: messageID, Err: err}
		}
		overrides, err := database.GetClassificationOverrides(messageID)
		return classificationOverrideMsg{MessageID: messageID, Overrides: overrides, Err: err}
	}
}

func (m Model) handleClassificationMsg(msg classificationOverrideMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.setStatus("classification correction: "+msg.Err.Error(), true)
		return m, m.clearStatusCmd()
	}
	if m.plugins.overrides == nil {
		m.plugins.overrides = map[int64]map[string]db.ClassificationOverride{}
	}
	if len(msg.Overrides) == 0 {
		delete(m.plugins.overrides, msg.MessageID)
	} else {
		m.plugins.overrides[msg.MessageID] = msg.Overrides
	}
	m.overlay = overlayNone
	m.setStatus("classification correction saved locally", false)
	return m, tea.Batch(m.clearStatusCmd(), m.needsYouRefreshCmd())
}

func (m Model) handleClassificationKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	e := &m.classification
	if m.mailTags.busy && !keyMatches(msg, m.keys.Cancel, m.keys.Back) {
		return m, nil
	}
	if e.custom {
		switch {
		case keyMatches(msg, m.keys.Cancel):
			e.custom = false
			return m, nil
		case keyMatches(msg, m.keys.Confirm):
			value := strings.ToLower(strings.TrimSpace(e.input.Value()))
			if _, err := db.ValidateClassificationOverride(db.ClassificationCategory, value); err != nil || value == db.ClassificationPlugin || value == db.ClassificationNone {
				m.setStatus("invalid custom category", true)
				return m, nil
			}
			return m.saveClassificationChoice(value)
		}
		e.input, _ = e.input.Update(msg)
		return m, nil
	}
	switch {
	case keyMatches(msg, m.keys.Cancel, m.keys.Back):
		m.overlay = overlayNone
	case msg.String() == "r":
		return m, classificationOverrideCmd(m.db, e.messageID, "", "", true)
	case keyMatches(msg, m.keys.Up):
		e.field = max(0, e.field-1)
		e.choice = 0
		e.setChoices(m.effectiveClassification(e.messageID))
	case keyMatches(msg, m.keys.Down):
		e.field = min(classificationCategory, e.field+1)
		e.choice = 0
		e.setChoices(m.effectiveClassification(e.messageID))
	case keyMatches(msg, m.keys.Left):
		e.choice = max(0, e.choice-1)
	case keyMatches(msg, m.keys.Right):
		e.choice = min(len(e.choices)-1, e.choice+1)
	case keyMatches(msg, m.keys.Space) && e.field == classificationCategory:
		if keyword := categoryKeyword(e.choices[e.choice]); keyword != "" {
			return m.saveCategoryTag(keyword, !mailtag.Has(m.mailTags.editor.message.Flags, keyword), "")
		}
	case keyMatches(msg, m.keys.Confirm):
		value := e.choices[e.choice]
		if e.field == classificationCategory && value == "custom" {
			e.custom = true
			e.input = textinput.New()
			e.input.Placeholder = "short category"
			e.input.Focus()
			return m, textinput.Blink
		}
		return m.saveClassificationChoice(value)
	}
	return m, nil
}

func (m Model) renderClassificationOverlay() string {
	width := max(1, min(m.width-4, 58))
	chrome := newManagerChrome(width, m.styles.Theme, m.styles.PlainUI)
	base := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.text)
	if m.classification.custom {
		body := base.Padding(1, 2).Width(width).Render("Custom category\n\n" + m.classification.input.View() + "\n\nUse lowercase letters, numbers, - or _ (max 12 characters)")
		return renderSoftPanelBox(body, width, "tidemail", "category", chrome)
	}
	e := m.classification
	lines := []string{base.Bold(true).Render("Correct classification"), ""}
	for i, name := range classificationFieldNames {
		marker := "  "
		if classificationField(i) == e.field {
			marker = "› "
		}
		lines = append(lines, base.Render(marker+name))
	}
	lines = append(lines, "", base.Bold(true).Render(nameForClassificationField(e.field)+":"))
	visible := max(1, m.height-23)
	start := max(0, e.choice-visible+1)
	end := min(len(e.choices), start+visible)
	if start > 0 {
		lines = append(lines, base.Render("… more above"))
	}
	for i := start; i < end; i++ {
		choice := e.choices[i]
		marker := "  "
		if i == e.choice {
			marker = "● "
		}
		label := e.displayChoice(choice)
		if e.field == classificationCategory && categoryKeyword(choice) != "" {
			checked := "[ ] "
			if mailtag.Has(m.mailTags.editor.message.Flags, categoryKeyword(choice)) {
				checked = "[x] "
			}
			label = checked + label
		}
		lines = append(lines, base.Render(truncate(marker+label, max(1, width-4))))
	}
	if end < len(e.choices) {
		lines = append(lines, base.Render("… more below"))
	}
	assigned := m.mailTagsForMessage(m.mailTags.editor.message)
	if len(assigned) > 0 {
		names := make([]string, len(assigned))
		for i, tag := range assigned {
			names[i] = tag.category
		}
		lines = append(lines, base.Render(truncate("Saved tags: "+strings.Join(names, ", "), width-4)))
	}
	if e.field == classificationCategory {
		lines = append(lines, base.Render("[x] saved on server; space toggles selected tag"))
	}
	if m.mailTags.busy {
		lines = append(lines, base.Render("Saving tag to server…"))
	}
	if m.mailTags.editor.error != "" {
		lines = append(lines, base.Render(truncate(m.mailTags.editor.error, width-4)))
	}
	lines = append(lines, "", base.Render("↑↓ field  ←→ choice  enter save  r reset all  esc cancel"))
	body := base.Padding(1, 2).Width(width).Render(strings.Join(lines, "\n"))
	return renderSoftPanelBox(body, width, "tidemail", "correct classification", chrome)
}

func nameForClassificationField(field classificationField) string {
	return classificationFieldNames[field]
}

func loadMessageClassificationOverrides(database *db.DB, msgs []db.Message) map[int64]map[string]db.ClassificationOverride {
	if database == nil || len(msgs) == 0 {
		return nil
	}
	ids := make([]int64, len(msgs))
	for i, msg := range msgs {
		ids[i] = msg.ID
	}
	overrides, err := database.GetClassificationOverridesForMessages(ids)
	if err != nil {
		return nil
	}
	return overrides
}

func (m Model) effectiveClassification(messageID int64) attention.EffectiveClassification {
	return attention.FromDB(m.plugins.annotations[messageID], m.plugins.overrides[messageID])
}

func (m Model) effectiveClassificationFor(messageIDs ...int64) attention.EffectiveClassification {
	var out attention.EffectiveClassification
	for _, id := range messageIDs {
		one := m.effectiveClassification(id)
		if one.NeedsReply && !out.NeedsReply {
			out.NeedsReplySource = one.NeedsReplySource
		}
		if one.Urgent && !out.Urgent {
			out.UrgencySource = one.UrgencySource
		}
		if one.Important && !out.Important {
			out.ImportanceSource = one.ImportanceSource
		}
		out.NeedsReply = out.NeedsReply || one.NeedsReply
		out.Urgent = out.Urgent || one.Urgent
		out.Important = out.Important || one.Important
		if out.Category == "" && one.Category != "" {
			out.Category, out.CategorySource = one.Category, one.CategorySource
		}
	}
	return out
}

func categoryKeyword(category string) string {
	if strings.HasPrefix(category, "tag:") {
		return strings.TrimPrefix(category, "tag:")
	}
	switch category {
	case "important":
		return "$label1"
	case "work":
		return "$label2"
	case "personal":
		return "$label3"
	case "to-do":
		return "$label4"
	case "later":
		return "$label5"
	}
	return ""
}

func (m Model) saveCategoryTag(keyword string, enabled bool, category string) (tea.Model, tea.Cmd) {
	m.mailTags.busy = true
	m.mailTags.editor.error = ""
	m.mailTags.operation++
	m.mailTags.searchVersion++
	change := m.changeMailTagCmd(m.mailTags.editor.message, keyword, enabled, m.mailTags.operation)
	database, messageID := m.db, m.classification.messageID
	return m, func() tea.Msg {
		result := change().(mailTagChangedMsg)
		if result.err == nil && category != "" {
			correction := classificationOverrideCmd(database, messageID, db.ClassificationCategory, category, false)().(classificationOverrideMsg)
			result.classification = &correction
			if correction.Err != nil {
				result.err = fmt.Errorf("tag saved on server; local category failed: %w", correction.Err)
			}
		}
		return result
	}
}

func isDefaultTag(keyword string) bool {
	for _, category := range categoryChoices {
		if mapped := categoryKeyword(category); mapped != "" && strings.EqualFold(mapped, keyword) {
			return true
		}
	}
	return false
}

func (m Model) saveClassificationChoice(value string) (tea.Model, tea.Cmd) {
	m.classification.custom = false
	if m.classification.field == classificationCategory {
		if keyword := categoryKeyword(value); keyword != "" {
			category := value
			if strings.HasPrefix(value, "tag:") {
				category = ""
			}
			return m.saveCategoryTag(keyword, true, category)
		}
	}
	return m, classificationOverrideCmd(m.db, m.classification.messageID, m.classification.key(), value, false)
}
