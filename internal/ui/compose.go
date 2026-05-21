package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	"github.com/allisonhere/tide/internal/smtp"
)

type composeField int

const (
	composeFieldTo composeField = iota
	composeFieldCC
	composeFieldSubject
	composeFieldBody
	composeFieldCount
)

type ComposeModel struct {
	toInput      textinput.Model
	ccInput      textinput.Model
	subjectInput textinput.Model
	bodyInput    textarea.Model

	focusedField composeField
	inReplyTo    string
	references   string
	accountCfg   config.AccountConfig

	busy      bool
	statusMsg string
	isErr     bool
}

func NewCompose(acfg config.AccountConfig) ComposeModel {
	c := ComposeModel{accountCfg: acfg}
	c.toInput = newComposeInput("to@example.com")
	c.ccInput = newComposeInput("")
	c.ccInput.Placeholder = "cc (optional)"
	c.subjectInput = newComposeInput("Subject")
	c.bodyInput = textarea.New()
	c.bodyInput.Placeholder = "Write your message here..."
	c.bodyInput.ShowLineNumbers = false
	c.focusedField = composeFieldTo
	c.toInput.Focus()
	return c
}

func NewReply(original db.Message, acfg config.AccountConfig) ComposeModel {
	c := NewCompose(acfg)
	replyTo := original.ReplyTo
	if replyTo == "" {
		replyTo = original.From
	}
	c.toInput.SetValue(replyTo)
	subject := original.Subject
	if !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
	}
	c.subjectInput.SetValue(subject)
	c.inReplyTo = original.MessageID
	refs := original.MessageID
	if original.MessageID != "" {
		refs = original.MessageID
	}
	c.references = refs
	return c
}

func newComposeInput(placeholder string) textinput.Model {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.CharLimit = 512
	return ti
}

func (c ComposeModel) Update(msg tea.Msg, keys KeyMap) (ComposeModel, tea.Cmd, bool) {
	if c.busy {
		return c, nil, false
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		switch c.focusedField {
		case composeFieldTo:
			c.toInput, cmd = c.toInput.Update(msg)
		case composeFieldCC:
			c.ccInput, cmd = c.ccInput.Update(msg)
		case composeFieldSubject:
			c.subjectInput, cmd = c.subjectInput.Update(msg)
		case composeFieldBody:
			c.bodyInput, cmd = c.bodyInput.Update(msg)
		}
		return c, cmd, false
	}

	switch {
	case keyMatches(km, keys.Cancel):
		return c, nil, true

	case km.String() == "ctrl+s":
		return c.send()

	case keyMatches(km, keys.Tab):
		c.advanceField(1)
		return c, nil, false

	default:
		var cmd tea.Cmd
		switch c.focusedField {
		case composeFieldTo:
			if keyMatches(km, keys.Confirm) {
				c.advanceField(1)
				return c, nil, false
			}
			c.toInput, cmd = c.toInput.Update(msg)
		case composeFieldCC:
			if keyMatches(km, keys.Confirm) {
				c.advanceField(1)
				return c, nil, false
			}
			c.ccInput, cmd = c.ccInput.Update(msg)
		case composeFieldSubject:
			if keyMatches(km, keys.Confirm) {
				c.advanceField(1)
				return c, nil, false
			}
			c.subjectInput, cmd = c.subjectInput.Update(msg)
		case composeFieldBody:
			c.bodyInput, cmd = c.bodyInput.Update(msg)
		}
		return c, cmd, false
	}
}

func (c *ComposeModel) advanceField(delta int) {
	next := (int(c.focusedField) + delta + int(composeFieldCount)) % int(composeFieldCount)
	c.focusedField = composeField(next)
	c.toInput.Blur()
	c.ccInput.Blur()
	c.subjectInput.Blur()
	c.bodyInput.Blur()
	switch c.focusedField {
	case composeFieldTo:
		c.toInput.Focus()
	case composeFieldCC:
		c.ccInput.Focus()
	case composeFieldSubject:
		c.subjectInput.Focus()
	case composeFieldBody:
		c.bodyInput.Focus()
	}
}

func (c ComposeModel) send() (ComposeModel, tea.Cmd, bool) {
	to := strings.TrimSpace(c.toInput.Value())
	if to == "" {
		c.statusMsg = "TO is required"
		c.isErr = true
		return c, nil, false
	}
	acfg := c.accountCfg
	msg := smtp.OutgoingMessage{
		To:         parseAddressList(to),
		CC:         parseAddressList(c.ccInput.Value()),
		Subject:    c.subjectInput.Value(),
		Body:       c.bodyInput.Value(),
		InReplyTo:  c.inReplyTo,
		References: c.references,
	}
	c.busy = true
	c.statusMsg = "Sending..."
	c.isErr = false
	return c, sendMessageCmd(acfg, msg), false
}

func sendMessageCmd(acfg config.AccountConfig, msg smtp.OutgoingMessage) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := smtp.Send(ctx, acfg, msg)
		return MessageSentMsg{Err: err}
	}
}

func parseAddressList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (c ComposeModel) View(width, height int, styles Styles) string {
	chrome := newManagerChrome(width, styles.Theme, styles.PlainUI)
	header := renderManagerHeader("COMPOSE", width, chrome)
	if c.inReplyTo != "" {
		header = renderManagerHeader("REPLY", width, chrome)
	}

	inputW := max(10, width-4)
	styledInput := func(ti textinput.Model, focused bool) string {
		bg := chrome.baseBg
		if focused {
			bg = chrome.fieldBg
		}
		ti.Width = inputW
		ti.PromptStyle = lipgloss.NewStyle().Background(bg).Foreground(chrome.accent)
		ti.TextStyle = lipgloss.NewStyle().Background(bg).Foreground(chrome.text)
		ti.PlaceholderStyle = lipgloss.NewStyle().Background(bg).Foreground(chrome.muted)
		ti.Cursor.Style = lipgloss.NewStyle().Background(chrome.accent).Foreground(contrastFg(chrome.accent))
		return lipgloss.NewStyle().Background(bg).Width(width).Padding(0, 2).Render(ti.View())
	}
	label := func(s string) string {
		return lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted).
			Width(width).Padding(0, 2).Render(s)
	}

	bodyH := max(4, height-lipgloss.Height(header)-10)
	c.bodyInput.SetWidth(inputW)
	c.bodyInput.SetHeight(bodyH)
	c.bodyInput.FocusedStyle.Base = lipgloss.NewStyle().Background(chrome.fieldBg)
	c.bodyInput.BlurredStyle.Base = lipgloss.NewStyle().Background(chrome.baseBg)
	bodyFocused := c.focusedField == composeFieldBody
	bodyStyle := lipgloss.NewStyle().Background(chrome.baseBg)
	if bodyFocused {
		bodyStyle = lipgloss.NewStyle().Background(chrome.fieldBg)
	}
	bodyView := bodyStyle.Width(width).Padding(0, 2).Render(c.bodyInput.View())

	statusLine := ""
	if c.statusMsg != "" {
		fg := chrome.pendingFg
		if c.isErr {
			fg = chrome.errorFg
		}
		statusLine = lipgloss.NewStyle().
			Background(chrome.baseBg).
			Foreground(fg).
			Width(width).
			Padding(0, 2).
			Render(c.statusMsg)
	}

	rows := []string{
		header,
		label("To"),
		styledInput(c.toInput, c.focusedField == composeFieldTo),
		label("CC"),
		styledInput(c.ccInput, c.focusedField == composeFieldCC),
		label("Subject"),
		styledInput(c.subjectInput, c.focusedField == composeFieldSubject),
		label("Body (ctrl+s to send)"),
		bodyView,
	}
	if statusLine != "" {
		rows = append(rows, statusLine)
	}

	actionPairs := []string{"ctrl+s", "send", "tab", "next field", "esc", "cancel"}
	if c.busy {
		actionPairs = nil
	}
	actions := renderManagerActions(width, chrome, actionPairs...)
	rows = append(rows, actions)

	content := lipgloss.JoinVertical(lipgloss.Left, rows...)
	return clampView(content, width, height, chrome.baseBg)
}

// statusMsg helpers used by model.go
func (c ComposeModel) StatusMsg() string { return c.statusMsg }
func (c ComposeModel) IsErr() bool       { return c.isErr }

// clearStatus resets the status after model.go shows it in the status bar.
func (c *ComposeModel) clearStatus() {
	c.statusMsg = ""
	c.isErr = false
}

// sentStatusLine returns a brief label for the status bar.
func composeSentStatus(err error) string {
	if err != nil {
		return fmt.Sprintf("send failed: %v", err)
	}
	return "message sent"
}
