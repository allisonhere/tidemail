package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	"github.com/allisonhere/tide/internal/imap"
)

// ── managerChrome ─────────────────────────────────────────────────────────────
// Shared visual chrome used by account manager, settings, and overlays.
// Moved here from the deleted feed_manager.go.

type managerChrome struct {
	baseBg             lipgloss.Color
	surfaceBg          lipgloss.Color
	fieldBg            lipgloss.Color
	accent             lipgloss.Color
	accentFg           lipgloss.Color
	highlight          lipgloss.Color
	highlightFg        lipgloss.Color
	border             lipgloss.Color
	text               lipgloss.Color
	muted              lipgloss.Color
	errorFg            lipgloss.Color
	successFg          lipgloss.Color
	pendingFg          lipgloss.Color
	header             lipgloss.Style
	sectionLabel       lipgloss.Style
	sectionLabelActive lipgloss.Style
	body               lipgloss.Style
	panel              lipgloss.Style
	panelSelected      lipgloss.Style
	key                lipgloss.Style
	keyLabel           lipgloss.Style
	statusBar          lipgloss.Style
	plainUI            bool
}

func (c managerChrome) pickerChevronLeft() string {
	if c.plainUI {
		return "< "
	}
	return "◀ "
}

func (c managerChrome) pickerChevronRight() string {
	if c.plainUI {
		return " >"
	}
	return " ▶"
}

func newManagerChrome(width int, t Theme, plainUI bool) managerChrome {
	baseBg := modalSurface(t)
	surfaceDelta := 0.04
	fieldDelta := 0.08
	if !isDark(baseBg) {
		surfaceDelta = -surfaceDelta
		fieldDelta = -fieldDelta
	}
	surfaceBg := adjustLightness(baseBg, surfaceDelta)
	fieldBg := adjustLightness(baseBg, fieldDelta)
	accent := t.BorderFocus
	if accent == "" {
		accent = t.OverlayBorder
	}
	if accent == "" {
		accent = t.Border
	}
	accentFg := contrastFg(accent)
	text := readableText(t.Fg, baseBg, 4.5)
	muted := mutedText(text, baseBg)
	highlight := accent
	if isDark(baseBg) {
		highlight = adjustLightness(accent, -0.16)
	} else {
		highlight = adjustLightness(accent, 0.12)
	}
	highlightFg := contrastFg(highlight)
	border := t.OverlayBorder
	if border == "" {
		border = t.Border
	}
	if border == "" {
		border = accent
	}
	errorFg := t.Error
	if errorFg == "" {
		errorFg = accent
	}
	successFg := t.Unread
	if successFg == "" {
		successFg = accent
	}
	pendingFg := t.BorderFocus
	if pendingFg == "" {
		pendingFg = accent
	}

	return managerChrome{
		baseBg:      baseBg,
		surfaceBg:   surfaceBg,
		fieldBg:     fieldBg,
		plainUI:     plainUI,
		accent:      accent,
		accentFg:    accentFg,
		highlight:   highlight,
		highlightFg: highlightFg,
		border:      border,
		text:        text,
		muted:       muted,
		errorFg:     errorFg,
		successFg:   successFg,
		pendingFg:   pendingFg,
		header: lipgloss.NewStyle().
			Width(width).
			Background(accent).
			Foreground(accentFg).
			Bold(true).
			Padding(0, 1),
		sectionLabel: lipgloss.NewStyle().
			Background(baseBg).
			Foreground(muted).
			Padding(0, 1).
			Bold(true),
		sectionLabelActive: lipgloss.NewStyle().
			Background(highlight).
			Foreground(highlightFg).
			Padding(0, 1).
			Bold(true),
		body: lipgloss.NewStyle().
			Background(baseBg).
			Foreground(text),
		panel: lipgloss.NewStyle().
			Width(max(1, width-4)).
			Background(surfaceBg).
			Foreground(text).
			Border(lipPaneBorder(plainUI)).
			BorderForeground(border).
			BorderBackground(surfaceBg).
			Padding(0, 1),
		panelSelected: lipgloss.NewStyle().
			Background(highlight).
			Foreground(highlightFg).
			Bold(true).
			Padding(0, 1),
		key: lipgloss.NewStyle().
			Background(accent).
			Foreground(accentFg).
			Bold(true).
			Padding(0, 1),
		keyLabel: lipgloss.NewStyle().
			Background(baseBg).
			Foreground(muted),
		statusBar: lipgloss.NewStyle().
			Width(width).
			Background(surfaceBg).
			Foreground(readableText(accent, surfaceBg, 3)).
			Border(lipPaneBorder(plainUI), true, false, false, false).
			BorderForeground(border).
			Padding(0, 1),
	}
}

func renderManagerHeader(title string, width int, chrome managerChrome) string {
	gap := max(0, width-lipgloss.Width(title)-2)
	return chrome.header.Render(title + strings.Repeat(" ", gap))
}

func renderManagerSection(label, body string, chrome managerChrome, labelActive bool) string {
	w := lipgloss.Width(body)
	style := chrome.sectionLabel
	if labelActive {
		style = chrome.sectionLabelActive
	}
	styledLabel := style.Width(w).Render(label)
	return lipgloss.JoinVertical(lipgloss.Left, styledLabel, body)
}

func renderManagerPaneSection(label, body string, focused bool, chrome managerChrome) string {
	w := lipgloss.Width(body)
	style := chrome.sectionLabel
	if focused {
		style = chrome.sectionLabelActive
	}
	styledLabel := style.Width(w).Render(label)
	return lipgloss.JoinVertical(lipgloss.Left, styledLabel, body)
}

func renderManagerActionGroups(width int, chrome managerChrome, primaryPairs, secondaryPairs []string) string {
	rows := []string{renderManagerActions(width, chrome, primaryPairs...)}
	if len(secondaryPairs) > 0 {
		rows = append(rows, renderManagerActions(width, chrome, secondaryPairs...))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func renderManagerActions(width int, chrome managerChrome, pairs ...string) string {
	bar := lipgloss.NewStyle().
		Width(width).
		Background(chrome.baseBg).
		Border(lipPaneBorder(chrome.plainUI), true, false, false, false).
		BorderForeground(chrome.border).
		Padding(0, 0)
	parts := make([]string, 0, len(pairs)/2)
	spacer := lipgloss.NewStyle().Background(chrome.baseBg).Render(" ")
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, lipgloss.JoinHorizontal(
			lipgloss.Left,
			chrome.key.Render(strings.ToUpper(pairs[i])),
			spacer,
			chrome.keyLabel.Render(strings.ToUpper(pairs[i+1])),
		))
	}
	if len(parts) == 0 {
		return bar.Render(clampView("", width, 1, chrome.baseBg))
	}
	bg := lipgloss.NewStyle().Background(chrome.baseBg)
	sep := bg.Render("  ")
	left := strings.Join(parts[:max(0, len(parts)-1)], sep)
	right := parts[len(parts)-1]
	gap := max(1, width-lipgloss.Width(left)-lipgloss.Width(right))
	row := clampView(left+bg.Render(strings.Repeat(" ", gap))+right, width, 1, chrome.baseBg)
	return bar.Render(row)
}

func renderManagerSelectedRow(width int, title string, chrome managerChrome, styles Styles) string {
	textW := max(1, width-2)
	row := padRight(truncate(title, textW), textW)
	bg := terminalColorAsColor(managerSelectedListStyle(styles).GetBackground())
	return clampView(managerSelectedListStyle(styles).Render(row), width, 1, bg)
}

func managerSelectedListStyle(styles Styles) lipgloss.Style {
	bg := terminalColorAsColor(styles.FeedItemSelectedFocused.GetBackground())
	if bg != "" {
		if isDark(bg) {
			bg = adjustLightness(bg, 0.08)
		} else {
			bg = adjustLightness(bg, -0.08)
		}
	}
	return lipgloss.NewStyle().
		Background(bg).
		Foreground(readableText(styles.Theme.Fg, bg, 4.5)).
		Bold(true).
		Padding(0, 1)
}

// ── AccountManager ────────────────────────────────────────────────────────────

type amMode int

const (
	amList amMode = iota
	amAdd
	amEdit
	amConfirmDelete
)

type amField int

const (
	amFieldName amField = iota
	amFieldIMAPHost
	amFieldIMAPPort
	amFieldIMAPTLS
	amFieldSMTPHost
	amFieldSMTPPort
	amFieldSMTPTLS
	amFieldUser
	amFieldPass
	amFieldFrom
	amFieldCount
)

type AccountManager struct {
	db *db.DB

	accounts  []db.Account
	mailboxes []db.Mailbox
	cursor    int

	mode amMode

	nameInput     textinput.Model
	imapHostInput textinput.Model
	imapPortInput textinput.Model
	imapTLS       bool
	smtpHostInput textinput.Model
	smtpPortInput textinput.Model
	smtpTLS       bool
	userInput     textinput.Model
	passInput     textinput.Model
	fromInput     textinput.Model

	focusedField  amField
	editAccountID int64

	busy      bool
	busyMsg   string
	statusMsg string
}

func NewAccountManager(database *db.DB) AccountManager {
	am := AccountManager{db: database, imapTLS: true, smtpTLS: true}
	am.nameInput = newAMInput("display name", false)
	am.imapHostInput = newAMInput("imap.example.com", false)
	am.imapPortInput = newAMInput("993", false)
	am.smtpHostInput = newAMInput("smtp.example.com", false)
	am.smtpPortInput = newAMInput("587", false)
	am.userInput = newAMInput("you@example.com", false)
	am.passInput = newAMInput("password", true)
	am.fromInput = newAMInput("Your Name <you@example.com>", false)
	return am
}

func newAMInput(placeholder string, password bool) textinput.Model {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.CharLimit = 256
	if password {
		ti.EchoMode = textinput.EchoPassword
	}
	return ti
}

func (am *AccountManager) setData(accounts []db.Account, mailboxes []db.Mailbox) {
	am.accounts = accounts
	am.mailboxes = mailboxes
	am.cursor = clamp(am.cursor, 0, max(0, len(accounts)-1))
}

func (am *AccountManager) syncInputWidths(width int) {
	inputW := max(10, width-6)
	for _, ti := range []*textinput.Model{
		&am.nameInput, &am.imapHostInput, &am.imapPortInput,
		&am.smtpHostInput, &am.smtpPortInput,
		&am.userInput, &am.passInput, &am.fromInput,
	} {
		ti.Width = inputW
	}
}

func (am *AccountManager) focusField(f amField) {
	am.focusedField = f
	am.nameInput.Blur()
	am.imapHostInput.Blur()
	am.imapPortInput.Blur()
	am.smtpHostInput.Blur()
	am.smtpPortInput.Blur()
	am.userInput.Blur()
	am.passInput.Blur()
	am.fromInput.Blur()
	switch f {
	case amFieldName:
		am.nameInput.Focus()
	case amFieldIMAPHost:
		am.imapHostInput.Focus()
	case amFieldIMAPPort:
		am.imapPortInput.Focus()
	case amFieldSMTPHost:
		am.smtpHostInput.Focus()
	case amFieldSMTPPort:
		am.smtpPortInput.Focus()
	case amFieldUser:
		am.userInput.Focus()
	case amFieldPass:
		am.passInput.Focus()
	case amFieldFrom:
		am.fromInput.Focus()
	}
}

func (am *AccountManager) populateFormFrom(acfg config.AccountConfig) {
	am.nameInput.SetValue(acfg.Name)
	am.imapHostInput.SetValue(acfg.IMAPHost)
	am.imapPortInput.SetValue(strconv.Itoa(acfg.IMAPPort))
	am.imapTLS = acfg.IMAPTLS
	am.smtpHostInput.SetValue(acfg.SMTPHost)
	am.smtpPortInput.SetValue(strconv.Itoa(acfg.SMTPPort))
	am.smtpTLS = acfg.SMTPTLS
	am.userInput.SetValue(acfg.User)
	am.passInput.SetValue(acfg.Password)
	am.fromInput.SetValue(acfg.From)
}

func (am AccountManager) buildCfg() config.AccountConfig {
	imapPort, _ := strconv.Atoi(am.imapPortInput.Value())
	if imapPort == 0 {
		imapPort = 993
	}
	smtpPort, _ := strconv.Atoi(am.smtpPortInput.Value())
	if smtpPort == 0 {
		smtpPort = 587
	}
	return config.AccountConfig{
		Name:     strings.TrimSpace(am.nameInput.Value()),
		IMAPHost: strings.TrimSpace(am.imapHostInput.Value()),
		IMAPPort: imapPort,
		IMAPTLS:  am.imapTLS,
		SMTPHost: strings.TrimSpace(am.smtpHostInput.Value()),
		SMTPPort: smtpPort,
		SMTPTLS:  am.smtpTLS,
		User:     strings.TrimSpace(am.userInput.Value()),
		Password: am.passInput.Value(),
		From:     strings.TrimSpace(am.fromInput.Value()),
	}
}

func (am AccountManager) selectedAccount() *db.Account {
	if am.cursor < 0 || am.cursor >= len(am.accounts) {
		return nil
	}
	return &am.accounts[am.cursor]
}

func (am AccountManager) Update(msg tea.Msg, keys KeyMap) (AccountManager, tea.Cmd, bool) {
	switch am.mode {
	case amList:
		return am.updateList(msg, keys)
	case amAdd, amEdit:
		return am.updateForm(msg, keys)
	case amConfirmDelete:
		return am.updateConfirmDelete(msg, keys)
	}
	return am, nil, false
}

func (am AccountManager) updateList(msg tea.Msg, keys KeyMap) (AccountManager, tea.Cmd, bool) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return am, nil, false
	}
	switch {
	case keyMatches(km, keys.Cancel), keyMatches(km, keys.Back):
		return am, nil, true
	case keyMatches(km, keys.Up):
		if am.cursor > 0 {
			am.cursor--
		}
	case keyMatches(km, keys.Down):
		if am.cursor < len(am.accounts)-1 {
			am.cursor++
		}
	case keyMatches(km, keys.Add):
		am.mode = amAdd
		am.editAccountID = 0
		am.resetForm()
		am.focusField(amFieldName)
	case keyMatches(km, keys.Edit):
		if acc := am.selectedAccount(); acc != nil {
			am.mode = amEdit
			am.editAccountID = acc.ID
			am.resetForm()
			am.focusField(amFieldName)
			am.nameInput.SetValue(acc.Name)
		}
	case keyMatches(km, keys.Delete):
		if am.selectedAccount() != nil {
			am.mode = amConfirmDelete
		}
	}
	return am, nil, false
}

func (am AccountManager) updateForm(msg tea.Msg, keys KeyMap) (AccountManager, tea.Cmd, bool) {
	if am.busy {
		return am, nil, false
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		// Forward to focused input
		var cmd tea.Cmd
		switch am.focusedField {
		case amFieldName:
			am.nameInput, cmd = am.nameInput.Update(msg)
		case amFieldIMAPHost:
			am.imapHostInput, cmd = am.imapHostInput.Update(msg)
		case amFieldIMAPPort:
			am.imapPortInput, cmd = am.imapPortInput.Update(msg)
		case amFieldSMTPHost:
			am.smtpHostInput, cmd = am.smtpHostInput.Update(msg)
		case amFieldSMTPPort:
			am.smtpPortInput, cmd = am.smtpPortInput.Update(msg)
		case amFieldUser:
			am.userInput, cmd = am.userInput.Update(msg)
		case amFieldPass:
			am.passInput, cmd = am.passInput.Update(msg)
		case amFieldFrom:
			am.fromInput, cmd = am.fromInput.Update(msg)
		}
		return am, cmd, false
	}
	switch {
	case keyMatches(km, keys.Cancel):
		am.mode = amList
		am.statusMsg = ""
	case keyMatches(km, keys.Tab):
		am.advanceField(1)
	case keyMatches(km, keys.Backspace):
		// let input handle it
		fallthrough
	default:
		// toggle TLS fields
		if km.String() == " " {
			switch am.focusedField {
			case amFieldIMAPTLS:
				am.imapTLS = !am.imapTLS
				return am, nil, false
			case amFieldSMTPTLS:
				am.smtpTLS = !am.smtpTLS
				return am, nil, false
			}
		}
		if keyMatches(km, keys.Confirm) {
			if am.focusedField == amFieldFrom {
				// submit
				return am.submitForm()
			}
			am.advanceField(1)
			return am, nil, false
		}
		// Forward keystroke to focused input
		var cmd tea.Cmd
		switch am.focusedField {
		case amFieldName:
			am.nameInput, cmd = am.nameInput.Update(msg)
		case amFieldIMAPHost:
			am.imapHostInput, cmd = am.imapHostInput.Update(msg)
		case amFieldIMAPPort:
			am.imapPortInput, cmd = am.imapPortInput.Update(msg)
		case amFieldSMTPHost:
			am.smtpHostInput, cmd = am.smtpHostInput.Update(msg)
		case amFieldSMTPPort:
			am.smtpPortInput, cmd = am.smtpPortInput.Update(msg)
		case amFieldUser:
			am.userInput, cmd = am.userInput.Update(msg)
		case amFieldPass:
			am.passInput, cmd = am.passInput.Update(msg)
		case amFieldFrom:
			am.fromInput, cmd = am.fromInput.Update(msg)
		}
		return am, cmd, false
	}
	return am, nil, false
}

func (am AccountManager) submitForm() (AccountManager, tea.Cmd, bool) {
	acfg := am.buildCfg()
	if acfg.Name == "" {
		am.statusMsg = "NAME IS REQUIRED"
		return am, nil, false
	}
	if acfg.IMAPHost == "" {
		am.statusMsg = "IMAP HOST IS REQUIRED"
		return am, nil, false
	}
	if acfg.User == "" {
		am.statusMsg = "USERNAME IS REQUIRED"
		return am, nil, false
	}
	am.busy = true
	am.busyMsg = "CONNECTING TO IMAP..."
	am.statusMsg = ""
	return am, saveAccountCmd(am.db, acfg, am.editAccountID), false
}

func (am AccountManager) updateConfirmDelete(msg tea.Msg, keys KeyMap) (AccountManager, tea.Cmd, bool) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return am, nil, false
	}
	switch {
	case keyMatches(km, keys.Yes), keyMatches(km, keys.Confirm):
		if acc := am.selectedAccount(); acc != nil {
			am.busy = true
			am.busyMsg = "DELETING..."
			return am, deleteAccountCmd(am.db, acc.ID), false
		}
		am.mode = amList
	case keyMatches(km, keys.No), keyMatches(km, keys.Cancel):
		am.mode = amList
	}
	return am, nil, false
}

func (am *AccountManager) advanceField(delta int) {
	next := int(am.focusedField) + delta
	// Skip TLS toggles from tab navigation — they're toggled with space
	for {
		if next < 0 {
			next = int(amFieldCount) - 1
		}
		if next >= int(amFieldCount) {
			next = 0
		}
		if amField(next) != amFieldIMAPTLS && amField(next) != amFieldSMTPTLS {
			break
		}
		next += delta
	}
	am.focusField(amField(next))
}

func (am *AccountManager) resetForm() {
	am.nameInput.Reset()
	am.imapHostInput.Reset()
	am.imapPortInput.SetValue("993")
	am.imapTLS = true
	am.smtpHostInput.Reset()
	am.smtpPortInput.SetValue("587")
	am.smtpTLS = true
	am.userInput.Reset()
	am.passInput.Reset()
	am.fromInput.Reset()
	am.statusMsg = ""
	am.busy = false
	am.busyMsg = ""
}

func (am AccountManager) View(width, height int, styles Styles) string {
	chrome := newManagerChrome(width, styles.Theme, styles.PlainUI)
	switch am.mode {
	case amAdd:
		return am.viewForm(width, height, chrome, "ADD ACCOUNT")
	case amEdit:
		return am.viewForm(width, height, chrome, "EDIT ACCOUNT")
	case amConfirmDelete:
		return am.viewConfirmDelete(width, chrome)
	default:
		return am.viewList(width, height, chrome, styles)
	}
}

func (am AccountManager) viewList(width, height int, chrome managerChrome, styles Styles) string {
	header := renderManagerHeader("ACCOUNTS", width, chrome)

	rows := []string{}
	for i, acc := range am.accounts {
		selected := i == am.cursor
		label := acc.Name
		if label == "" {
			label = fmt.Sprintf("Account %d", acc.ID)
		}
		// Count mailboxes for this account
		count := 0
		for _, mb := range am.mailboxes {
			if mb.AccountID == acc.ID {
				count++
			}
		}
		if count > 0 {
			label += fmt.Sprintf("  (%d mailboxes)", count)
		}
		if selected {
			rows = append(rows, renderManagerSelectedRow(width, label, chrome, styles))
		} else {
			rows = append(rows, lipgloss.NewStyle().
				Background(chrome.baseBg).
				Foreground(chrome.text).
				Width(width).
				Padding(0, 1).
				Render(label))
		}
	}

	if len(am.accounts) == 0 {
		rows = append(rows, lipgloss.NewStyle().
			Background(chrome.baseBg).
			Foreground(chrome.muted).
			Width(width).
			Padding(1, 2).
			Render("No accounts. Press a to add one."))
	}

	bodyH := max(1, height-lipgloss.Height(header)-3)
	body := clampView(
		lipgloss.NewStyle().Background(chrome.baseBg).Width(width).Render(
			strings.Join(rows, "\n"),
		),
		width, bodyH, chrome.baseBg,
	)

	statusLine := ""
	if am.statusMsg != "" {
		statusLine = lipgloss.NewStyle().
			Background(chrome.baseBg).
			Foreground(chrome.errorFg).
			Width(width).
			Padding(0, 1).
			Render(am.statusMsg)
	}

	var primaryActions []string
	if len(am.accounts) > 0 {
		primaryActions = []string{"e", "edit", "d", "delete", "esc", "close"}
	} else {
		primaryActions = []string{"esc", "close"}
	}
	actions := renderManagerActionGroups(width, chrome,
		[]string{"a", "add account"},
		primaryActions,
	)

	parts := []string{header, body}
	if statusLine != "" {
		parts = append(parts, statusLine)
	}
	parts = append(parts, actions)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (am AccountManager) viewForm(width, height int, chrome managerChrome, title string) string {
	header := renderManagerHeader(title, width, chrome)

	fieldW := max(10, width-4)
	label := func(s string) string {
		return lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted).
			Width(fieldW).Padding(0, 2).Render(s)
	}
	field := func(ti textinput.Model, focused bool) string {
		bg := chrome.baseBg
		if focused {
			bg = chrome.fieldBg
		}
		ti.PromptStyle = lipgloss.NewStyle().Background(bg).Foreground(chrome.accent)
		ti.TextStyle = lipgloss.NewStyle().Background(bg).Foreground(chrome.text)
		ti.PlaceholderStyle = lipgloss.NewStyle().Background(bg).Foreground(chrome.muted)
		ti.Cursor.Style = lipgloss.NewStyle().Background(chrome.accent).Foreground(contrastFg(chrome.accent))
		return lipgloss.NewStyle().Background(bg).Width(fieldW).Padding(0, 2).Render(ti.View())
	}
	tlsLabel := func(on bool, focused bool) string {
		val := "off"
		if on {
			val = "on"
		}
		style := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted).Width(fieldW).Padding(0, 2)
		if focused {
			style = style.Background(chrome.fieldBg).Foreground(chrome.text)
		}
		return style.Render(fmt.Sprintf("[space] TLS: %s", val))
	}

	rows := []string{
		label("Name"),
		field(am.nameInput, am.focusedField == amFieldName),
		label("IMAP Host"),
		field(am.imapHostInput, am.focusedField == amFieldIMAPHost),
		label("IMAP Port"),
		field(am.imapPortInput, am.focusedField == amFieldIMAPPort),
		tlsLabel(am.imapTLS, am.focusedField == amFieldIMAPTLS),
		label("SMTP Host"),
		field(am.smtpHostInput, am.focusedField == amFieldSMTPHost),
		label("SMTP Port"),
		field(am.smtpPortInput, am.focusedField == amFieldSMTPPort),
		tlsLabel(am.smtpTLS, am.focusedField == amFieldSMTPTLS),
		label("Username"),
		field(am.userInput, am.focusedField == amFieldUser),
		label("Password"),
		field(am.passInput, am.focusedField == amFieldPass),
		label("From (optional)"),
		field(am.fromInput, am.focusedField == amFieldFrom),
	}

	statusLine := ""
	if am.statusMsg != "" {
		statusLine = lipgloss.NewStyle().
			Background(chrome.baseBg).
			Foreground(chrome.errorFg).
			Width(width).
			Padding(0, 1).
			Render(am.statusMsg)
	}
	if am.busyMsg != "" {
		statusLine = lipgloss.NewStyle().
			Background(chrome.baseBg).
			Foreground(chrome.pendingFg).
			Width(width).
			Padding(0, 1).
			Render(am.busyMsg)
	}

	bodyH := max(1, height-lipgloss.Height(header)-4)
	body := clampView(
		lipgloss.NewStyle().Background(chrome.baseBg).Width(width).Render(
			strings.Join(rows, "\n"),
		),
		width, bodyH, chrome.baseBg,
	)

	var actionPairs []string
	if am.busy {
		actionPairs = []string{}
	} else {
		actionPairs = []string{"enter", "next / save", "esc", "cancel"}
	}
	actions := renderManagerActions(width, chrome, actionPairs...)

	parts := []string{header, body}
	if statusLine != "" {
		parts = append(parts, statusLine)
	}
	parts = append(parts, actions)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (am AccountManager) viewConfirmDelete(width int, chrome managerChrome) string {
	header := renderManagerHeader("DELETE ACCOUNT?", width, chrome)
	acc := am.selectedAccount()
	name := "this account"
	if acc != nil {
		name = acc.Name
	}
	body := lipgloss.NewStyle().
		Background(chrome.baseBg).
		Foreground(chrome.text).
		Width(width).
		Padding(1, 2).
		Render(fmt.Sprintf("Delete %q? All mailboxes and messages will be removed.", name))
	actions := renderManagerActions(width, chrome, "y", "delete", "esc", "cancel")
	return lipgloss.JoinVertical(lipgloss.Left, header, body, actions)
}

// ── Async commands ────────────────────────────────────────────────────────────

func saveAccountCmd(database *db.DB, acfg config.AccountConfig, editID int64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		client := imap.New(acfg)
		if err := client.Connect(ctx); err != nil {
			return AccountSavedMsg{AccountCfg: acfg, Err: fmt.Errorf("IMAP connect failed: %w", err)}
		}
		defer client.Close()

		var accountID int64
		var err error
		if editID != 0 {
			if err = database.UpdateAccount(editID, acfg.Name, ""); err != nil {
				return AccountSavedMsg{AccountCfg: acfg, Err: fmt.Errorf("update account: %w", err)}
			}
			accountID = editID
		} else {
			accountID, err = database.AddAccount(acfg.Name, "")
			if err != nil {
				return AccountSavedMsg{AccountCfg: acfg, Err: fmt.Errorf("add account: %w", err)}
			}
		}

		account, err := database.GetAccount(accountID)
		if err != nil {
			return AccountSavedMsg{AccountCfg: acfg, Err: fmt.Errorf("get account: %w", err)}
		}

		infos, err := client.ListMailboxes(ctx)
		if err != nil {
			return AccountSavedMsg{Account: account, AccountCfg: acfg,
				Err: fmt.Errorf("list mailboxes: %w", err)}
		}

		var mailboxes []db.Mailbox
		for _, info := range infos {
			mb := db.Mailbox{
				AccountID: accountID,
				Name:      info.Name,
				Delimiter: info.Delimiter,
				Flags:     info.Flags,
			}
			id, upsertErr := database.UpsertMailbox(mb)
			if upsertErr != nil {
				continue
			}
			mb.ID = id
			mailboxes = append(mailboxes, mb)
		}

		return AccountSavedMsg{Account: account, Mailboxes: mailboxes, AccountCfg: acfg}
	}
}

func deleteAccountCmd(database *db.DB, accountID int64) tea.Cmd {
	return func() tea.Msg {
		err := database.DeleteAccount(accountID)
		return AccountDeletedMsg{AccountID: accountID, Err: err}
	}
}
