package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	"github.com/allisonhere/tide/internal/imap"
)

// ── managerChrome ─────────────────────────────────────────────────────────────

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
	amFieldProvider amField = iota
	amFieldName
	amFieldColor
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

var accountColorList = []struct {
	Name string
	Hex  string
}{
	{"None", ""},
	{"Blue", "#89b4fa"},
	{"Green", "#a6e3a1"},
	{"Red", "#f38ba8"},
	{"Purple", "#cba6f7"},
	{"Orange", "#fab387"},
	{"Teal", "#94e2d5"},
	{"Pink", "#f5c2e7"},
	{"Yellow", "#f9e2af"},
	{"Cyan", "#89dceb"},
	{"Lavender", "#b4befe"},
	{"Coral", "#eba0ac"},
	{"Mint", "#a6e3a1"},
	{"Rosewater", "#f5e0dc"},
	{"Flamingo", "#f2cdcd"},
	{"Peach", "#fab387"},
	{"Maroon", "#eba0ac"},
	{"Mauve", "#cba6f7"},
	{"Sapphire", "#74c7ec"},
	{"Sky", "#89dceb"},
}

type AccountManager struct {
	db *db.DB

	accounts  []db.Account
	mailboxes []db.Mailbox
	configs   []config.AccountConfig
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

	provider      string
	focusedField  amField
	editAccountID int64
	colorIdx      int

	busy      bool
	busyMsg   string
	statusMsg string
}

func NewAccountManager(database *db.DB) AccountManager {
	am := AccountManager{db: database, provider: "Custom", imapTLS: true, smtpTLS: true}
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

func (am *AccountManager) setData(accounts []db.Account, mailboxes []db.Mailbox, configs []config.AccountConfig) {
	am.accounts = accounts
	am.mailboxes = mailboxes
	am.configs = configs
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
	case amFieldColor:
		// no input to focus; handled in updateForm
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
	am.provider = acfg.Provider
	if am.provider == "" {
		am.provider = "Custom"
	}
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
	cfg := config.AccountConfig{
		Provider: am.provider,
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
	if preset, ok := providerPresets[am.provider]; ok {
		cfg.IMAPHost = preset.IMAPHost
		cfg.IMAPPort = preset.IMAPPort
		cfg.IMAPTLS = preset.IMAPTLS
		cfg.SMTPHost = preset.SMTPHost
		cfg.SMTPPort = preset.SMTPPort
		cfg.SMTPTLS = preset.SMTPTLS
	}
	return cfg
}

func (am AccountManager) selectedAccount() *db.Account {
	if am.cursor < 0 || am.cursor >= len(am.accounts) {
		return nil
	}
	return &am.accounts[am.cursor]
}

func colorIndexForHex(hex string) int {
	for i, c := range accountColorList {
		if c.Hex == hex {
			return i
		}
	}
	return 0
}

func colorSuffix(hex string, bg lipgloss.Color) string {
	if hex == "" {
		return ""
	}
	return " " + lipgloss.NewStyle().
		Background(lipgloss.Color(hex)).
		Foreground(contrastFg(lipgloss.Color(hex))).
		Padding(0, 1).
		Render("  ")
}

func (am AccountManager) configForAccount(acc db.Account) (config.AccountConfig, bool) {
	for _, acfg := range am.configs {
		if acfg.Name == acc.Name {
			return acfg, true
		}
	}
	return config.AccountConfig{}, false
}

func (am AccountManager) statusForeground(chrome managerChrome) lipgloss.Color {
	msg := strings.ToUpper(strings.TrimSpace(am.statusMsg))
	switch {
	case strings.HasPrefix(msg, "CONNECTED:"), strings.HasPrefix(msg, "SAVED:"), strings.HasPrefix(msg, "DELETED"):
		return chrome.successFg
	default:
		return chrome.errorFg
	}
}

func (am AccountManager) redactSensitive(s string) string {
	return am.redactSensitiveWithAccounts(s, nil)
}

func (am AccountManager) redactSensitiveWithAccounts(s string, extraAccounts []config.AccountConfig) string {
	cfg := config.DefaultConfig()
	cfg.Accounts = append(cfg.Accounts, am.configs...)
	cfg.Accounts = append(cfg.Accounts, extraAccounts...)
	if pass := am.passInput.Value(); strings.TrimSpace(pass) != "" {
		cfg.Accounts = append(cfg.Accounts, config.AccountConfig{Password: pass})
	}
	return config.RedactSecrets(s, cfg)
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
			if acfg, ok := am.configForAccount(*acc); ok {
				am.populateFormFrom(acfg)
			} else {
				am.nameInput.SetValue(acc.Name)
			}
			am.colorIdx = colorIndexForHex(acc.Color)
		}
	case keyMatches(km, keys.Delete):
		if am.selectedAccount() != nil {
			am.mode = amConfirmDelete
		}
	}
	return am, nil, false
}

func (am *AccountManager) updateFocusedInput(msg tea.Msg) tea.Cmd {
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
	return cmd
}

func (am AccountManager) updateForm(msg tea.Msg, keys KeyMap) (AccountManager, tea.Cmd, bool) {
	if am.busy {
		return am, nil, false
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return am, am.updateFocusedInput(msg), false
	}
	switch {
	case keyMatches(km, keys.Cancel):
		am.mode = amList
		am.statusMsg = ""
	case keyMatches(km, keys.TestAccount):
		return am.testForm()
	case keyMatches(km, keys.SaveAccount):
		return am.submitForm()
	case keyMatches(km, keys.Tab):
		am.advanceField(1)
	case keyMatches(km, keys.Down):
		am.advanceField(1)
	case keyMatches(km, keys.Up):
		am.advanceField(-1)
	case am.focusedField == amFieldProvider && (keyMatches(km, keys.Left) || keyMatches(km, keys.Right)):
		for i, p := range providerList {
			if p == am.provider {
				if keyMatches(km, keys.Right) {
					am.provider = providerList[(i+1)%len(providerList)]
				} else {
					am.provider = providerList[(i-1+len(providerList))%len(providerList)]
				}
				break
			}
		}
	case am.focusedField == amFieldColor && (keyMatches(km, keys.Left) || keyMatches(km, keys.Right)):
		if keyMatches(km, keys.Right) {
			am.colorIdx = (am.colorIdx + 1) % len(accountColorList)
		} else {
			am.colorIdx = (am.colorIdx - 1 + len(accountColorList)) % len(accountColorList)
		}
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
		return am, am.updateFocusedInput(msg), false
	}
	return am, nil, false
}

func (am AccountManager) submitForm() (AccountManager, tea.Cmd, bool) {
	acfg := am.buildCfg()
	color := accountColorList[am.colorIdx].Hex
	if status := validateAccountForConnect(acfg); status != "" {
		am.statusMsg = status
		return am, nil, false
	}
	am.busy = true
	am.busyMsg = "CONNECTING TO IMAP..."
	am.statusMsg = ""
	return am, saveAccountCmd(am.db, acfg, am.editAccountID, color), false
}

func (am AccountManager) testForm() (AccountManager, tea.Cmd, bool) {
	acfg := am.buildCfg()
	if status := validateAccountForConnect(acfg); status != "" {
		am.statusMsg = status
		return am, nil, false
	}
	am.busy = true
	am.busyMsg = "TESTING ACCOUNT..."
	am.statusMsg = ""
	return am, testAccountCmd(acfg), false
}

func validateAccountForConnect(acfg config.AccountConfig) string {
	if acfg.Name == "" {
		return "NAME IS REQUIRED"
	}
	if acfg.IMAPHost == "" {
		return "IMAP HOST IS REQUIRED"
	}
	if acfg.User == "" {
		return "USERNAME IS REQUIRED"
	}
	return ""
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
	for i := 0; i < int(amFieldCount); i++ {
		next = ((next % int(amFieldCount)) + int(amFieldCount)) % int(amFieldCount)
		if am.provider == "Custom" {
			break
		}
		f := amField(next)
		if f < amFieldIMAPHost || f > amFieldSMTPTLS {
			break
		}
		next += delta
	}
	am.focusField(amField(next))
}

func (am *AccountManager) resetForm() {
	am.provider = "Custom"
	am.colorIdx = 0
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

	mailboxCounts := make(map[int64]int, len(am.accounts))
	for _, mb := range am.mailboxes {
		mailboxCounts[mb.AccountID]++
	}
	cfgByName := make(map[string]config.AccountConfig, len(am.configs))
	for _, c := range am.configs {
		cfgByName[c.Name] = c
	}

	rows := []string{}
	for i, acc := range am.accounts {
		selected := i == am.cursor
		rows = append(rows, am.renderAccountCard(width, acc, selected, chrome, styles, mailboxCounts[acc.ID], cfgByName[acc.Name]))
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
			Foreground(am.statusForeground(chrome)).
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

func (am AccountManager) renderAccountCard(width int, acc db.Account, selected bool, chrome managerChrome, styles Styles, mailboxCount int, acfg config.AccountConfig) string {
	name := acc.Name
	if name == "" {
		name = fmt.Sprintf("Account %d", acc.ID)
	}
	email := acfg.User
	if email == "" {
		email = "not configured"
	}
	imapServer := compactServer(acfg.IMAPHost, acfg.IMAPPort, acfg.IMAPTLS, "TLS")
	smtpServer := compactServer(acfg.SMTPHost, acfg.SMTPPort, acfg.SMTPTLS, "STARTTLS")
	status := fmt.Sprintf("%d mailboxes", mailboxCount)
	if mailboxCount == 1 {
		status = "1 mailbox"
	}

	bg := chrome.baseBg
	fg := chrome.text
	if selected {
		bg = terminalColorAsColor(managerSelectedListStyle(styles).GetBackground())
		fg = terminalColorAsColor(managerSelectedListStyle(styles).GetForeground())
		if fg == "" {
			fg = chrome.text
		}
	}
	row := func(label, value string) string {
		labelW := clamp(width/5, 8, 12)
		valueW := max(8, width-labelW)
		left := lipgloss.NewStyle().Background(bg).Foreground(chrome.muted).Width(labelW).Padding(0, 1).Render(label)
		right := lipgloss.NewStyle().Background(bg).Foreground(fg).Width(valueW).Padding(0, 1).Render(truncate(value, max(1, valueW-2)))
		return lipgloss.JoinHorizontal(lipgloss.Left, left, right)
	}

	rows := []string{
		row("Name", name+colorSuffix(acc.Color, bg)),
		row("Email", email),
		row("IMAP", imapServer),
		row("SMTP", smtpServer),
		lipgloss.NewStyle().Background(bg).Foreground(chrome.successFg).Width(width).Padding(0, 1).Render(status),
	}
	return clampView(strings.Join(rows, "\n"), width, len(rows), bg)
}

func compactServer(host string, port int, tls bool, tlsLabel string) string {
	if host == "" {
		return "not configured"
	}
	if port > 0 {
		host = fmt.Sprintf("%s:%d", host, port)
	}
	if tls {
		host += "  " + tlsLabel + " on"
	}
	return host
}

func (am AccountManager) viewForm(width, height int, chrome managerChrome, title string) string {
	header := renderManagerHeader(title, width, chrome)

	fieldW := max(12, width-18)
	labelW := max(10, width-fieldW)
	row := func(label string, ti textinput.Model, focused bool) string {
		bg := chrome.surfaceBg
		if focused {
			bg = chrome.fieldBg
		}
		ti.Prompt = ""
		ti.PromptStyle = lipgloss.NewStyle().Background(bg).Foreground(chrome.accent)
		ti.TextStyle = lipgloss.NewStyle().Background(bg).Foreground(chrome.text)
		ti.PlaceholderStyle = lipgloss.NewStyle().Background(bg).Foreground(chrome.muted)
		if focused {
			ti.Cursor.Style = lipgloss.NewStyle().Background(chrome.accent).Foreground(contrastFg(chrome.accent))
		} else {
			_ = ti.Cursor.SetMode(cursor.CursorHide)
		}
		ti.Width = fieldW
		left := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted).Width(max(1, labelW-2)).Padding(0, 1).Render(label)
		var fieldView string
		if focused {
			fieldView = ti.View()
		} else {
			val := ti.Value()
			if val == "" {
				fieldView = lipgloss.NewStyle().Background(bg).Foreground(chrome.muted).Render(ti.Placeholder)
			} else if ti.EchoMode == textinput.EchoPassword {
				fieldView = lipgloss.NewStyle().Background(bg).Foreground(chrome.text).Render(strings.Repeat("*", len([]rune(val))))
			} else {
				fieldView = lipgloss.NewStyle().Background(bg).Foreground(chrome.text).Render(val)
			}
		}
		right := lipgloss.NewStyle().Background(bg).Render(fieldView)
		return lipgloss.JoinHorizontal(lipgloss.Left, left, right)
	}
	tlsRow := func(label string, on bool, focused bool) string {
		val := "off"
		if on {
			val = "on"
		}
		fg := chrome.text
		if focused {
			fg = chrome.accent
		}
		left := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted).Width(max(1, labelW-2)).Padding(0, 1).Render(label)
		right := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(fg).Width(max(1, fieldW-2)).Padding(0, 1).Render("[space] " + val)
		return lipgloss.JoinHorizontal(lipgloss.Left, left, right)
	}

	providerRow := func(focused bool) string {
		fg := chrome.text
		if focused {
			fg = chrome.accent
		}
		idx := 0
		for i, p := range providerList {
			if p == am.provider {
				idx = i
				break
			}
		}
		val := "◀ " + providerList[idx] + " ▶"
		left := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted).Width(max(1, labelW-2)).Padding(0, 1).Render("Provider")
		right := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(fg).Width(max(1, fieldW-2)).Padding(0, 1).Render(val)
		return lipgloss.JoinHorizontal(lipgloss.Left, left, right)
	}

	colorRow := func(focused bool) string {
		fg := chrome.text
		if focused {
			fg = chrome.accent
		}
		c := accountColorList[am.colorIdx]
		swatch := ""
		if c.Hex != "" {
			swatch = lipgloss.NewStyle().
				Background(lipgloss.Color(c.Hex)).
				Foreground(contrastFg(lipgloss.Color(c.Hex))).
				Padding(0, 1).
				Render("  ")
		}
		val := "◀ " + c.Name + " ▶ " + swatch
		left := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted).Width(max(1, labelW-2)).Padding(0, 1).Render("Color")
		right := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(fg).Width(max(1, fieldW-2)).Padding(0, 1).Render(val)
		return lipgloss.JoinHorizontal(lipgloss.Left, left, right)
	}

	blank := lipgloss.NewStyle().Background(chrome.baseBg).Width(width).Render("")
	rows := []string{
		providerRow(am.focusedField == amFieldProvider), blank,
		row("Name", am.nameInput, am.focusedField == amFieldName), blank,
		colorRow(am.focusedField == amFieldColor), blank,
	}
	if am.provider == "Custom" {
		rows = append(rows,
			row("IMAP Host", am.imapHostInput, am.focusedField == amFieldIMAPHost), blank,
			row("IMAP Port", am.imapPortInput, am.focusedField == amFieldIMAPPort), blank,
			tlsRow("IMAP TLS", am.imapTLS, am.focusedField == amFieldIMAPTLS), blank,
			row("SMTP Host", am.smtpHostInput, am.focusedField == amFieldSMTPHost), blank,
			row("SMTP Port", am.smtpPortInput, am.focusedField == amFieldSMTPPort), blank,
			tlsRow("SMTP TLS", am.smtpTLS, am.focusedField == amFieldSMTPTLS), blank,
		)
	}
	userLabel := "Username"
	passInput := am.passInput
	if preset, ok := providerPresets[am.provider]; ok {
		userLabel = "Email"
		passInput.Placeholder = preset.PassHint
	}
	rows = append(rows,
		row(userLabel, am.userInput, am.focusedField == amFieldUser), blank,
		row("Password", passInput, am.focusedField == amFieldPass), blank,
	)
	if am.provider == "Gmail" {
		hintLeft := lipgloss.NewStyle().Background(chrome.baseBg).Width(max(1, labelW-2)).Padding(0, 1).Render("")
		hintRight := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted).Width(max(1, fieldW-2)).Padding(0, 1).Render("Google account → Security → App passwords")
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Left, hintLeft, hintRight), blank)
	}
	rows = append(rows, row("From", am.fromInput, am.focusedField == amFieldFrom))

	statusLine := ""
	if am.statusMsg != "" {
		statusLine = lipgloss.NewStyle().
			Background(chrome.baseBg).
			Foreground(am.statusForeground(chrome)).
			Width(width).
			Padding(0, 1).
			Render(am.redactSensitive(am.statusMsg))
	}
	if am.busyMsg != "" {
		statusLine = lipgloss.NewStyle().
			Background(chrome.baseBg).
			Foreground(chrome.pendingFg).
			Width(width).
			Padding(0, 1).
			Render(am.redactSensitive(am.busyMsg))
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
		actionPairs = []string{"ctrl+s", "save", "ctrl+t", "test", "esc", "cancel"}
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

func saveAccountCmd(database *db.DB, acfg config.AccountConfig, editID int64, color string) tea.Cmd {
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
			if err = database.UpdateAccount(editID, acfg.Name, color); err != nil {
				return AccountSavedMsg{AccountCfg: acfg, Err: fmt.Errorf("update account: %w", err)}
			}
			accountID = editID
		} else {
			accountID, err = database.AddAccount(acfg.Name, color)
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
				AccountID:   accountID,
				Name:        info.Name,
				DisplayName: cleanDisplayName(info.Name),
				Delimiter:   info.Delimiter,
				Flags:       info.Flags,
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

func testAccountCmd(acfg config.AccountConfig) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		client := imap.New(acfg)
		if err := client.Connect(ctx); err != nil {
			return AccountTestedMsg{Err: fmt.Errorf("IMAP connect failed: %w", err)}
		}
		defer client.Close()

		infos, err := client.ListMailboxes(ctx)
		if err != nil {
			return AccountTestedMsg{Err: fmt.Errorf("list mailboxes: %w", err)}
		}
		return AccountTestedMsg{MailboxCount: len(infos)}
	}
}

func deleteAccountCmd(database *db.DB, accountID int64) tea.Cmd {
	return func() tea.Msg {
		err := database.DeleteAccount(accountID)
		return AccountDeletedMsg{AccountID: accountID, Err: err}
	}
}
