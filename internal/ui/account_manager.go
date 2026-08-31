package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"golang.org/x/oauth2"

	"github.com/allisonhere/tidemail/internal/auth"
	"github.com/allisonhere/tidemail/internal/clipboard"
	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/imap"
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
	accentFg := accentReadableOn(t.Fg, accent, 4.5)
	text := readableText(t.Fg, baseBg, 4.5)
	muted := mutedText(text, baseBg)
	var highlight lipgloss.Color
	if isDark(baseBg) {
		highlight = adjustLightness(accent, -0.16)
	} else {
		highlight = adjustLightness(accent, 0.12)
	}
	highlightFg := accentReadableOn(t.Fg, highlight, 4.5)
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
	if w := lipgloss.Width(row); w < width {
		row += lipgloss.NewStyle().Background(chrome.baseBg).Render(strings.Repeat(" ", width-w))
	}
	return bar.Render(row)
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
	amFieldAuthMethod
	amFieldPass
	amFieldOAuthSignIn
	amFieldOAuthCode
	amFieldFrom
	amFieldSignature
	amFieldSyncInterval
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
	{"Crimson", "#ff3366"},
	{"Electric Blue", "#3b82ff"},
	{"Vivid Violet", "#8b5cf6"},
	{"Emerald", "#00d084"},
	{"Hot Pink", "#ff4fd8"},
	{"Aqua", "#00e5ff"},
	{"Amber", "#ffb000"},
	{"Lime", "#7CFC00"},
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
	syncInput     textinput.Model
	sigInput      textinput.Model

	provider      string
	focusedField  amField
	editAccountID int64
	colorIdx      int

	// OAuth sign-in state for the Gmail and Outlook providers. useOAuth is the
	// Auth-method selector (App password ⇄ OAuth). Gmail (and custom Microsoft
	// clients) use the device-code flow — SSH-safe: show a URL + short code,
	// poll. The bundled Thunderbird Microsoft client, and any device-code
	// rejection, fall back to the authorization-code paste-back flow.
	oauthCfg          config.OAuthConfig
	useOAuth          bool
	oauthSignedIn     bool
	oauthRefreshToken string
	oauthActive       bool
	oauthAwaitingCode bool // paste-back flow: waiting for the pasted code/URL
	oauthFlow         authCodeExchanger
	oauthFlowURL      string
	oauthCodeInput    textinput.Model
	oauthCtx          context.Context
	oauthCancel       context.CancelFunc

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
	am.syncInput = newAMInput("0 = push", false)
	am.sigInput = newAMInput(`signature (\n for a new line)`, false)
	am.oauthCodeInput = newAMInput("code, or full http://localhost/?code=... URL", false)
	am.oauthCodeInput.CharLimit = 2048 // a pasted redirect URL is long
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

func (am *AccountManager) setData(accounts []db.Account, mailboxes []db.Mailbox, configs []config.AccountConfig, oauthCfg config.OAuthConfig) {
	am.accounts = accounts
	am.mailboxes = mailboxes
	am.configs = configs
	am.oauthCfg = oauthCfg
	am.cursor = clamp(am.cursor, 0, max(0, len(accounts)-1))
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
	am.syncInput.Blur()
	am.sigInput.Blur()
	am.oauthCodeInput.Blur()
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
	case amFieldOAuthCode:
		am.oauthCodeInput.Focus()
	case amFieldFrom:
		am.fromInput.Focus()
	case amFieldSyncInterval:
		am.syncInput.Focus()
	case amFieldSignature:
		am.sigInput.Focus()
	}
}

func (am *AccountManager) populateFormFrom(acfg config.AccountConfig) {
	am.provider = acfg.Provider
	// Unknown providers (empty, or presets that no longer exist, e.g. the
	// removed Outlook preset) edit as Custom so the saved hosts stay visible.
	if _, ok := providerPresets[am.provider]; !ok {
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
	am.syncInput.SetValue(strconv.Itoa(acfg.SyncMinutes))
	// The form is single-line; real newlines round-trip as literal \n escapes.
	am.sigInput.SetValue(strings.ReplaceAll(acfg.Signature, "\n", `\n`))
	// Carry the existing refresh token so buildCfg preserves OAuth on save.
	am.oauthRefreshToken = acfg.RefreshToken
	am.oauthSignedIn = acfg.RefreshToken != ""
	am.useOAuth = (acfg.Provider == "Gmail" || acfg.Provider == "Outlook") && acfg.RefreshToken != ""
	am.oauthAwaitingCode = false
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
		Provider:    am.provider,
		Name:        strings.TrimSpace(am.nameInput.Value()),
		IMAPHost:    strings.TrimSpace(am.imapHostInput.Value()),
		IMAPPort:    imapPort,
		IMAPTLS:     am.imapTLS,
		SMTPHost:    strings.TrimSpace(am.smtpHostInput.Value()),
		SMTPPort:    smtpPort,
		SMTPTLS:     am.smtpTLS,
		User:        strings.TrimSpace(am.userInput.Value()),
		Password:    am.passInput.Value(),
		From:        strings.TrimSpace(am.fromInput.Value()),
		SyncMinutes: func() int { n, _ := parseSyncMinutes(am.syncInput.Value()); return n }(),
		Signature:   strings.ReplaceAll(strings.TrimSpace(am.sigInput.Value()), `\n`, "\n"),
	}
	if preset, ok := providerPresets[am.provider]; ok {
		cfg.IMAPHost = preset.IMAPHost
		cfg.IMAPPort = preset.IMAPPort
		cfg.IMAPTLS = preset.IMAPTLS
		cfg.SMTPHost = preset.SMTPHost
		cfg.SMTPPort = preset.SMTPPort
		cfg.SMTPTLS = preset.SMTPTLS
	}
	// A signed-in Gmail/Outlook account with the OAuth auth method authenticates
	// with XOAUTH2: the refresh token replaces the password. ClientID/Secret are
	// toml:"-" — they only feed the live connect on save/test; fillSecrets
	// re-fills them on later loads from the app-level [oauth] config.
	if am.providerSupportsOAuth() && am.useOAuth && am.oauthRefreshToken != "" {
		cfg.RefreshToken = am.oauthRefreshToken
		cfg.Password = ""
		if am.provider == "Outlook" {
			cfg.ClientID = am.oauthCfg.MSClientID // public client — no secret
		} else {
			cfg.ClientID = am.oauthCfg.GoogleClientID
			cfg.ClientSecret = am.oauthCfg.GoogleClientSecret
		}
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
	case strings.HasPrefix(msg, "CONNECTED:"), strings.HasPrefix(msg, "SAVED:"), strings.HasPrefix(msg, "DELETED"),
		strings.HasPrefix(msg, "SIGNED IN"), strings.HasPrefix(msg, "SIGN-IN PAGE"):
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
	if tok := strings.TrimSpace(am.oauthRefreshToken); tok != "" {
		cfg.Accounts = append(cfg.Accounts, config.AccountConfig{RefreshToken: tok})
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
		am.focusField(amFieldProvider)
	case keyMatches(km, keys.Edit):
		if acc := am.selectedAccount(); acc != nil {
			am.mode = amEdit
			am.editAccountID = acc.ID
			am.resetForm()
			am.focusField(amFieldProvider)
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
	case amFieldOAuthCode:
		am.oauthCodeInput, cmd = am.oauthCodeInput.Update(msg)
	case amFieldFrom:
		am.fromInput, cmd = am.fromInput.Update(msg)
	case amFieldSyncInterval:
		am.syncInput, cmd = am.syncInput.Update(msg)
	case amFieldSignature:
		am.sigInput, cmd = am.sigInput.Update(msg)
	}
	return cmd
}

func (am AccountManager) updateForm(msg tea.Msg, keys KeyMap) (AccountManager, tea.Cmd, bool) {
	// Sign-in results arrive async while the form is busy — handle them before
	// the busy guard.
	switch dm := msg.(type) {
	case DeviceCodeMsg:
		if !am.oauthActive {
			return am, nil, false // stale result from a cancelled flow
		}
		if dm.Err != nil {
			// The device endpoint rejected the request (commonly: the mail
			// scope isn't allowed for device clients). Fall back to the
			// authorization-code paste-back flow.
			return am.startAuthCodeFlow()
		}
		am.busyMsg = fmt.Sprintf("GO TO %s AND ENTER CODE %s (ESC CANCELS)", dm.VerificationURL, dm.UserCode)
		return am, am.pollDeviceTokenCmd(dm.da), false
	case OAuth2DoneMsg:
		if !am.oauthActive {
			return am, nil, false // stale result from a cancelled flow
		}
		if dm.Err != nil && am.oauthAwaitingCode {
			// A bad paste doesn't consume the PKCE verifier — keep the flow
			// alive so the user can paste again without restarting.
			am.busy = false
			am.busyMsg = ""
			am.statusMsg = fmt.Sprintf("SIGN-IN FAILED: %v", dm.Err)
			am.focusField(amFieldOAuthCode)
			return am, nil, false
		}
		provider := am.provider
		am.cancelOAuth()
		if dm.Err != nil {
			am.statusMsg = fmt.Sprintf("SIGN-IN FAILED: %v", dm.Err)
			return am, nil, false
		}
		if dm.RefreshToken == "" {
			am.statusMsg = "SIGN-IN FAILED: NO REFRESH TOKEN RETURNED (RE-CONSENT NEEDED)"
			return am, nil, false
		}
		am.oauthSignedIn = true
		am.oauthRefreshToken = dm.RefreshToken
		// Drop any cached tokens for this account so the next connect seeds
		// from the freshly issued refresh token.
		forgetOAuthToken(provider, strings.TrimSpace(am.nameInput.Value()))
		am.statusMsg = "SIGNED IN: " + strings.ToUpper(oauthVendor(provider)) + " ACCOUNT LINKED"
		am.focusField(amFieldFrom)
		return am, nil, false
	}
	if am.busy {
		// Allow bailing out of a pending device-code approval (it can take
		// many minutes to expire on its own).
		if km, ok := msg.(tea.KeyMsg); ok && am.oauthActive && keyMatches(km, keys.Cancel) {
			am.cancelOAuth()
			am.statusMsg = "SIGN-IN CANCELLED"
		}
		return am, nil, false
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return am, am.updateFocusedInput(msg), false
	}
	switch {
	case keyMatches(km, keys.Cancel):
		if am.oauthAwaitingCode {
			// Esc during code entry abandons the sign-in, not the form.
			am.cancelOAuth()
			am.statusMsg = "SIGN-IN CANCELLED"
			am.focusField(amFieldOAuthSignIn)
			return am, nil, false
		}
		am.mode = amList
		am.statusMsg = ""
	case keyMatches(km, keys.TestAccount):
		return am.testForm()
	case keyMatches(km, keys.Save):
		return am.submitForm()
	case keyMatches(km, keys.OAuthSignIn):
		if am.providerSupportsOAuth() {
			return am.startOAuthSignIn()
		}
	case keyMatches(km, keys.Tab):
		am.advanceField(1)
	case am.formNavMatches(km, keys.Down):
		am.advanceField(1)
	case am.formNavMatches(km, keys.Up):
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
		// Reset the auth method to the new provider's default and drop any
		// half-finished sign-in from the previous provider.
		am.cancelOAuth()
		am.oauthSignedIn = false
		am.oauthRefreshToken = ""
		am.useOAuth = am.defaultUseOAuth()
	case am.focusedField == amFieldColor && (keyMatches(km, keys.Left) || keyMatches(km, keys.Right)):
		if keyMatches(km, keys.Right) {
			am.colorIdx = (am.colorIdx + 1) % len(accountColorList)
		} else {
			am.colorIdx = (am.colorIdx - 1 + len(accountColorList)) % len(accountColorList)
		}
	case am.focusedField == amFieldAuthMethod && (keyMatches(km, keys.Left) || keyMatches(km, keys.Right)):
		am.useOAuth = !am.useOAuth
		if !am.useOAuth {
			am.cancelOAuth() // abandon any pending sign-in; a completed one is kept
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
			case amFieldOAuthSignIn:
				return am.startOAuthSignIn()
			}
		}
		if keyMatches(km, keys.Confirm) {
			if am.focusedField == amFieldOAuthSignIn {
				return am.startOAuthSignIn()
			}
			if am.focusedField == amFieldOAuthCode {
				return am.submitOAuthCode()
			}
			if am.focusedField == amFieldSyncInterval {
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
	if status := am.validateForm(acfg); status != "" {
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
	if status := am.validateForm(acfg); status != "" {
		am.statusMsg = status
		return am, nil, false
	}
	am.busy = true
	am.busyMsg = "TESTING ACCOUNT..."
	am.statusMsg = ""
	return am, testAccountCmd(acfg), false
}

// clipboardCopy is a seam so tests don't touch the real clipboard.
var clipboardCopy = clipboard.Copy

// authCodeExchanger is the common surface of the Google and Microsoft
// authorization-code (paste-back) flows.
type authCodeExchanger interface {
	Exchange(ctx context.Context, pasted string) (*oauth2.Token, error)
}

// providerSupportsOAuth reports whether the current provider has an OAuth path.
func (am AccountManager) providerSupportsOAuth() bool {
	return am.provider == "Gmail" || am.provider == "Outlook"
}

// oauthClientID returns the app-level client ID for the current provider, and
// whether one is configured (Gmail is bring-your-own; Outlook always has the
// bundled Thunderbird default).
func (am AccountManager) oauthClientID() (string, bool) {
	switch am.provider {
	case "Gmail":
		return am.oauthCfg.GoogleClientID, am.oauthCfg.GoogleClientID != ""
	case "Outlook":
		return am.oauthCfg.MSClientID, am.oauthCfg.MSClientID != ""
	default:
		return "", false
	}
}

// defaultUseOAuth picks the Auth-method default when the provider changes: OAuth
// whenever a client is configured for it.
func (am AccountManager) defaultUseOAuth() bool {
	_, ok := am.oauthClientID()
	return ok
}

// oauthDeviceCapable reports whether the current provider+client can use the
// device-code flow. Microsoft forbids it for the bundled Thunderbird client, so
// that path goes straight to paste-back.
func (am AccountManager) oauthDeviceCapable() bool {
	if am.provider == "Outlook" {
		return am.oauthCfg.MSClientID != config.ThunderbirdMSClientID
	}
	return am.provider == "Gmail"
}

func oauthVendor(provider string) string {
	if provider == "Outlook" {
		return "Microsoft"
	}
	return "Google"
}

func forgetOAuthToken(provider, account string) {
	if provider == "Outlook" {
		auth.ForgetMSToken(account)
		return
	}
	auth.ForgetGoogleToken(account)
}

// startOAuthSignIn kicks off a sign-in for the current provider. Device-capable
// clients request a short code and poll; otherwise (or on a device-code
// rejection) updateForm falls back to startAuthCodeFlow.
func (am AccountManager) startOAuthSignIn() (AccountManager, tea.Cmd, bool) {
	clientID, ok := am.oauthClientID()
	if !ok {
		am.statusMsg = "GMAIL OAUTH NOT CONFIGURED — SET TIDEMAIL_GOOGLE_CLIENT_ID / _SECRET"
		return am, nil, false
	}
	am.cancelOAuth() // drop any previous attempt
	am.statusMsg = ""
	if !am.oauthDeviceCapable() {
		return am.startAuthCodeFlow()
	}
	ctx, cancel := context.WithCancel(context.Background())
	am.oauthCtx = ctx
	am.oauthCancel = cancel
	am.oauthActive = true
	am.busy = true
	am.busyMsg = "REQUESTING SIGN-IN CODE..."
	provider := am.provider
	gSecret := am.oauthCfg.GoogleClientSecret
	return am, func() tea.Msg {
		var (
			da  *oauth2.DeviceAuthResponse
			err error
		)
		if provider == "Outlook" {
			da, err = auth.StartMSDeviceFlow(ctx, clientID)
		} else {
			da, err = auth.StartGoogleDeviceFlow(ctx, clientID, gSecret)
		}
		if err != nil {
			return DeviceCodeMsg{Err: err}
		}
		return DeviceCodeMsg{VerificationURL: da.VerificationURI, UserCode: da.UserCode, da: da}
	}, false
}

// pollDeviceTokenCmd polls the current provider's token endpoint until the user
// approves the device code.
func (am AccountManager) pollDeviceTokenCmd(da *oauth2.DeviceAuthResponse) tea.Cmd {
	ctx := am.oauthCtx
	provider := am.provider
	clientID, _ := am.oauthClientID()
	gSecret := am.oauthCfg.GoogleClientSecret
	return func() tea.Msg {
		var (
			tok *oauth2.Token
			err error
		)
		if provider == "Outlook" {
			tok, err = auth.PollMSDeviceToken(ctx, clientID, da)
		} else {
			tok, err = auth.PollGoogleDeviceToken(ctx, clientID, gSecret, da)
		}
		if err != nil {
			return OAuth2DoneMsg{Err: err}
		}
		return OAuth2DoneMsg{RefreshToken: tok.RefreshToken}
	}
}

// startAuthCodeFlow is the paste-back path: build the consent URL (copied to the
// clipboard), the user approves and lands on an unreachable localhost page, then
// pastes that URL (or the bare code) into the Code field.
func (am AccountManager) startAuthCodeFlow() (AccountManager, tea.Cmd, bool) {
	clientID, _ := am.oauthClientID()
	if am.provider == "Outlook" {
		f := auth.NewMSAuthCodeFlow(clientID)
		am.oauthFlow, am.oauthFlowURL = f, f.AuthURL
	} else {
		f := auth.NewGoogleAuthCodeFlow(am.oauthCfg.GoogleClientID, am.oauthCfg.GoogleClientSecret)
		am.oauthFlow, am.oauthFlowURL = f, f.AuthURL
	}
	am.oauthActive = true
	am.oauthAwaitingCode = true
	am.busy = false
	am.busyMsg = ""
	am.oauthCodeInput.Reset()
	am.focusField(amFieldOAuthCode)
	note := "SIGN-IN PAGE: OPEN THE URL BELOW, APPROVE, PASTE THE RESULT"
	if err := clipboardCopy(am.oauthFlowURL); err == nil {
		note = "SIGN-IN PAGE URL COPIED TO CLIPBOARD — OPEN IT, APPROVE, PASTE THE RESULT"
	}
	am.statusMsg = note
	return am, nil, false
}

// submitOAuthCode exchanges the pasted authorization code (or redirect URL) for
// tokens.
func (am AccountManager) submitOAuthCode() (AccountManager, tea.Cmd, bool) {
	pasted := strings.TrimSpace(am.oauthCodeInput.Value())
	if pasted == "" {
		am.statusMsg = "PASTE THE CODE OR REDIRECT URL FROM THE BROWSER FIRST"
		return am, nil, false
	}
	if am.oauthFlow == nil {
		am.statusMsg = "SIGN-IN EXPIRED — PRESS CTRL+O TO START AGAIN"
		return am, nil, false
	}
	am.busy = true
	am.busyMsg = "EXCHANGING SIGN-IN CODE..."
	am.statusMsg = ""
	flow := am.oauthFlow
	return am, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		tok, err := flow.Exchange(ctx, pasted)
		if err != nil {
			return OAuth2DoneMsg{Err: err}
		}
		return OAuth2DoneMsg{RefreshToken: tok.RefreshToken}
	}, false
}

// cancelOAuth stops any in-flight sign-in (either flow) and clears its state;
// safe to call when no flow is active.
func (am *AccountManager) cancelOAuth() {
	if am.oauthCancel != nil {
		am.oauthCancel()
	}
	am.oauthCtx = nil
	am.oauthCancel = nil
	am.oauthActive = false
	am.oauthAwaitingCode = false
	am.oauthFlow = nil
	am.oauthFlowURL = ""
	am.oauthCodeInput.Reset()
	am.busy = false
	am.busyMsg = ""
}

// formNavMatches treats the vim navigation runes (j/k on Up/Down) as navigation
// only while a non-text row is focused. While a text input is focused only the
// arrow keys navigate — typing "imap.gmail.com" must insert the runes, not jump
// fields.
func (am AccountManager) formNavMatches(km tea.KeyMsg, b key.Binding) bool {
	if km.Type == tea.KeyRunes && am.focusedIsTextInput() {
		return false
	}
	return keyMatches(km, b)
}

// focusedIsTextInput reports whether the focused form row is a free-text input
// (as opposed to a picker/toggle/button row).
func (am AccountManager) focusedIsTextInput() bool {
	switch am.focusedField {
	case amFieldProvider, amFieldColor, amFieldAuthMethod, amFieldIMAPTLS, amFieldSMTPTLS, amFieldOAuthSignIn:
		return false
	}
	return true
}

// parseSyncMinutes reads the refresh field, reporting whether the text was a
// value the user could have meant. The port fields above can quietly fall back
// to a default because a bad port fails loudly on connect, but sync_minutes
// cannot: every out-of-range string parses to 0, which now means push rather
// than off, so a typo would silently opt an account into a persistent
// connection. An empty field is the exception — that is a new account taking
// the push default, not a mistake.
func parseSyncMinutes(raw string) (int, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, true
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < -1 {
		return 0, false
	}
	return n, true
}

// validateForm checks the raw form text that buildCfg has to collapse into
// typed fields, then defers to the connection-level checks.
func (am AccountManager) validateForm(acfg config.AccountConfig) string {
	if _, ok := parseSyncMinutes(am.syncInput.Value()); !ok {
		return "REFRESH MUST BE -1 (MANUAL), 0 (PUSH), OR MINUTES"
	}
	return validateAccountForConnect(acfg)
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
	if acfg.IMAPPort < 1 || acfg.IMAPPort > 65535 {
		return "IMAP PORT MUST BE 1-65535"
	}
	if acfg.SMTPPort < 1 || acfg.SMTPPort > 65535 {
		return "SMTP PORT MUST BE 1-65535"
	}
	if (acfg.Provider == "Gmail" || acfg.Provider == "Outlook") && acfg.Password == "" && acfg.RefreshToken == "" {
		return "SIGN IN WITH " + strings.ToUpper(oauthVendor(acfg.Provider)) + " (CTRL+O) OR ENTER AN APP PASSWORD"
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
	if next < 0 || next >= int(amFieldCount) {
		return // don't wrap — stay on current field
	}
	// Skip hidden per-provider fields (IMAP/SMTP) when a preset is active.
	if am.provider != "Custom" {
		f := amField(next)
		for f >= amFieldIMAPHost && f <= amFieldSMTPTLS {
			next += delta
			if next < 0 || next >= int(amFieldCount) {
				return
			}
			f = amField(next)
		}
	}
	// The Auth-method selector and its OAuth sub-rows only exist for a provider
	// that supports OAuth (Gmail, Outlook); within that, the password row and
	// the OAuth rows are mutually exclusive per the selector, and the paste-back
	// Code row shows only while a sign-in awaits its code.
	oauthMode := am.providerSupportsOAuth() && am.useOAuth
	for {
		f := amField(next)
		skip := (f == amFieldAuthMethod && !am.providerSupportsOAuth()) ||
			(f == amFieldPass && oauthMode) ||
			(f == amFieldOAuthSignIn && !oauthMode) ||
			(f == amFieldOAuthCode && (!oauthMode || !am.oauthAwaitingCode))
		if skip {
			next += delta
			if next < 0 || next >= int(amFieldCount) {
				return
			}
			continue
		}
		break
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
	am.cancelOAuth()
	am.oauthSignedIn = false
	am.oauthRefreshToken = ""
	// resetForm sets provider to Custom, which has no OAuth path; the provider
	// cycle and populateFormFrom set the real default afterwards.
	am.useOAuth = am.defaultUseOAuth()
	am.statusMsg = ""
	am.busy = false
	am.busyMsg = ""
}

func (am AccountManager) View(width, height int, styles Styles) string {
	chrome := newManagerChrome(width, styles.Theme, styles.PlainUI)
	switch am.mode {
	case amAdd, amEdit:
		return am.viewForm(width, height, chrome)
	case amConfirmDelete:
		return am.viewConfirmDelete(width, chrome)
	default:
		return am.viewList(width, height, chrome, styles)
	}
}

// softTitle names the account manager's current mode for the soft-panel border.
func (am AccountManager) softTitle() string {
	switch am.mode {
	case amAdd:
		return "add account"
	case amEdit:
		return "edit account"
	case amConfirmDelete:
		return "delete account?"
	default:
		return "accounts"
	}
}

func (am AccountManager) viewList(width, height int, chrome managerChrome, styles Styles) string {
	mailboxCounts := make(map[int64]int, len(am.accounts))
	for _, mb := range am.mailboxes {
		mailboxCounts[mb.AccountID]++
	}
	cfgByName := make(map[string]config.AccountConfig, len(am.configs))
	for _, c := range am.configs {
		cfgByName[c.Name] = c
	}

	blank := lipgloss.NewStyle().Background(chrome.baseBg).Width(width).Render("")
	rows := []string{blank}
	for i, acc := range am.accounts {
		selected := i == am.cursor
		rows = append(rows, am.renderAccountCard(width, acc, selected, chrome, styles, mailboxCounts[acc.ID], cfgByName[acc.Name]), blank)
	}

	if len(am.accounts) == 0 {
		rows = append(rows, lipgloss.NewStyle().
			Background(chrome.baseBg).
			Foreground(chrome.muted).
			Width(width).
			Padding(1, 2).
			Render("No accounts. Press a to add one."))
	}

	bodyH := max(1, height-2)
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

	var actionPairs []string
	if len(am.accounts) > 0 {
		actionPairs = []string{"a", "add", "e", "edit", "d", "delete", "esc", "close"}
	} else {
		actionPairs = []string{"a", "add", "esc", "close"}
	}
	actions := renderSoftHints(width, chrome, actionPairs...)

	parts := []string{body}
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
	nameFg := chrome.text
	serverFg := chrome.muted
	emailFg := chrome.muted
	if selected {
		bg = chrome.surfaceBg
		emailFg = chrome.text
	}
	gutter := lipgloss.NewStyle().Background(bg).Width(2).Render("")
	rail := func() string { return gutter + softRail(chrome, selected, bg) }

	dot := ""
	if acc.Color != "" {
		dotGlyph := "●"
		if chrome.plainUI {
			dotGlyph = "*"
		}
		dot = lipgloss.NewStyle().Background(bg).Render(" ") +
			lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(acc.Color)).Render(dotGlyph)
	}
	nameCell := lipgloss.NewStyle().Background(bg).Foreground(nameFg).Bold(true).Render(name) + dot
	emailCell := lipgloss.NewStyle().Background(bg).Foreground(emailFg).Render(email)
	nameLine := rail() + nameCell
	gap := width - lipgloss.Width(nameLine) - lipgloss.Width(emailCell) - 2
	if gap >= 1 {
		nameLine += lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", gap)) + emailCell
	}

	servers := imapServer
	if smtpServer != "" && smtpServer != "not configured" {
		sep := " · "
		if chrome.plainUI {
			sep = " | "
		}
		servers += sep + smtpServer
	}
	serverLine := rail() + lipgloss.NewStyle().Background(bg).Foreground(serverFg).Render(truncate(servers, max(1, width-6)))
	statusLine := rail() + lipgloss.NewStyle().Background(bg).Foreground(chrome.successFg).Render(status)

	rows := []string{
		padStyled(nameLine, width, bg),
		padStyled(serverLine, width, bg),
		padStyled(statusLine, width, bg),
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

func (am AccountManager) viewForm(width, height int, chrome managerChrome) string {
	fieldW := max(12, width-20)
	labelW := max(10, width-fieldW-2)
	rail := func(focused bool) string { return softRail(chrome, focused, chrome.baseBg) }
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
			ti.Cursor.Style = lipgloss.NewStyle().Background(chrome.accent).Foreground(accentReadableOn(chrome.text, chrome.accent, 4.5))
		} else {
			_ = ti.Cursor.SetMode(cursor.CursorHide)
		}
		ti.Width = fieldW
		labelFg := chrome.muted
		if focused {
			labelFg = chrome.text
		}
		left := rail(focused) + lipgloss.NewStyle().Background(chrome.baseBg).Foreground(labelFg).Width(max(1, labelW-2)).Padding(0, 1).Render(label)
		var fieldView string
		if focused {
			cursorTi := ti
			if cursorTi.Value() == "" {
				// bubbles' placeholderView pads to Width with unstyled spaces when
				// Width is set, leaking the terminal's default background. Leave
				// padding to our own Width(fieldW) wrap below, which styles it.
				cursorTi.Width = 0
			}
			fieldView = inputViewWithCursor(cursorTi, true)
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
		right := lipgloss.NewStyle().Background(bg).Width(fieldW).Render(truncateStyled(fieldView, fieldW, bg))
		return lipgloss.JoinHorizontal(lipgloss.Left, left, right)
	}
	labelCell := func(label string, focused bool) string {
		labelFg := chrome.muted
		if focused {
			labelFg = chrome.text
		}
		return rail(focused) + lipgloss.NewStyle().Background(chrome.baseBg).Foreground(labelFg).Width(max(1, labelW-2)).Padding(0, 1).Render(label)
	}
	tlsRow := func(label string, on bool, focused bool) string {
		right := lipgloss.NewStyle().Background(chrome.baseBg).Render(" ") + renderSoftToggle(on, focused, chrome)
		return padStyled(labelCell(label, focused)+right, width, chrome.baseBg)
	}

	pickerVal := func(value, swatch string, focused bool) string {
		valueFg := chrome.muted
		chevronFg := chrome.muted
		if focused {
			valueFg = chrome.text
			chevronFg = chrome.accent
		}
		val := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(valueFg).Render(value) + swatch +
			lipgloss.NewStyle().Background(chrome.baseBg).Render("  ") +
			lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chevronFg).Bold(focused).Render(chrome.softChevrons())
		return val
	}

	providerRow := func(focused bool) string {
		idx := 0
		for i, p := range providerList {
			if p == am.provider {
				idx = i
				break
			}
		}
		return padStyled(labelCell("Provider", focused)+pickerVal(providerList[idx], "", focused), width, chrome.baseBg)
	}

	colorRow := func(focused bool) string {
		c := accountColorList[am.colorIdx]
		swatch := ""
		if c.Hex != "" {
			dotGlyph := " ●"
			if chrome.plainUI {
				dotGlyph = " *"
			}
			swatch = lipgloss.NewStyle().
				Background(chrome.baseBg).
				Foreground(lipgloss.Color(c.Hex)).
				Render(dotGlyph)
		}
		return padStyled(labelCell("Color", focused)+pickerVal(c.Name, swatch, focused), width, chrome.baseBg)
	}

	blank := lipgloss.NewStyle().Background(chrome.baseBg).Width(width).Render("")
	rows := []string{blank}
	anchors := make(map[amField]int)
	addLine := func(line string) {
		rows = append(rows, line)
	}
	addBlank := func() {
		addLine(blank)
	}
	addControl := func(field amField, line string) {
		anchors[field] = viewLineCount(rows)
		addLine(line)
	}
	addSection := func(title string) {
		if len(rows) > 1 {
			addBlank()
		}
		heading := rail(false) + lipgloss.NewStyle().
			Background(chrome.baseBg).
			Foreground(chrome.accent).
			Bold(true).
			Padding(0, 1).
			Render(title)
		addLine(padStyled(heading, width, chrome.baseBg))
	}

	addSection("Account")
	addControl(amFieldProvider, providerRow(am.focusedField == amFieldProvider))
	addBlank()
	addControl(amFieldName, row("Name", am.nameInput, am.focusedField == amFieldName))
	addControl(amFieldColor, colorRow(am.focusedField == amFieldColor))
	if am.provider == "Custom" {
		addSection("Incoming mail")
		addControl(amFieldIMAPHost, row("IMAP Host", am.imapHostInput, am.focusedField == amFieldIMAPHost))
		addControl(amFieldIMAPPort, row("IMAP Port", am.imapPortInput, am.focusedField == amFieldIMAPPort))
		addControl(amFieldIMAPTLS, tlsRow("IMAP TLS", am.imapTLS, am.focusedField == amFieldIMAPTLS))
		addSection("Outgoing mail")
		addControl(amFieldSMTPHost, row("SMTP Host", am.smtpHostInput, am.focusedField == amFieldSMTPHost))
		addControl(amFieldSMTPPort, row("SMTP Port", am.smtpPortInput, am.focusedField == amFieldSMTPPort))
		addControl(amFieldSMTPTLS, tlsRow("SMTP TLS", am.smtpTLS, am.focusedField == amFieldSMTPTLS))
	}
	userLabel := "Username"
	passInput := am.passInput
	if preset, ok := providerPresets[am.provider]; ok {
		userLabel = "Email"
		passInput.Placeholder = preset.PassHint
	}
	// A muted continuation line under a field, aligned to the field column.
	addHint := func(hint string) {
		hintLeft := lipgloss.NewStyle().Background(chrome.baseBg).Width(max(1, labelW)).Render("")
		hintRight := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted).Width(max(1, fieldW-2)).Padding(0, 1).Render(hint)
		addLine(lipgloss.JoinHorizontal(lipgloss.Left, hintLeft, hintRight))
	}

	addSection("Credentials")
	addControl(amFieldUser, row(userLabel, am.userInput, am.focusedField == amFieldUser))

	if !am.providerSupportsOAuth() {
		// Providers without an OAuth path: app password only.
		addControl(amFieldPass, row("Password", passInput, am.focusedField == amFieldPass))
	} else {
		vendor := oauthVendor(am.provider) // "Google" / "Microsoft"
		// A left/right selector chooses the auth method. The verbose guidance
		// only appears while the selector or its sub-controls are focused, so
		// the form stays quiet the rest of the time.
		methodVal := "App password"
		if am.useOAuth {
			methodVal = "OAuth · sign in with " + vendor
		}
		authRow := padStyled(
			labelCell("Auth", am.focusedField == amFieldAuthMethod)+
				pickerVal(methodVal, "", am.focusedField == amFieldAuthMethod),
			width, chrome.baseBg)
		addControl(amFieldAuthMethod, authRow)
		addBlank()

		authFocused := am.focusedField == amFieldAuthMethod ||
			am.focusedField == amFieldPass ||
			am.focusedField == amFieldOAuthSignIn ||
			am.focusedField == amFieldOAuthCode

		if !am.useOAuth {
			addControl(amFieldPass, row("Password", passInput, am.focusedField == amFieldPass))
			if authFocused {
				if am.provider == "Outlook" {
					addHint("Outlook.com no longer accepts passwords — use OAuth (‹ / ›).")
					addHint("Only M365 tenants that still allow app passwords work here.")
				} else {
					addHint("Gmail needs an App Password, not your normal password:")
					addHint("2-Step Verification → myaccount.google.com/apppasswords")
					addHint("‹ / › switches to OAuth sign-in instead.")
				}
			}
		} else {
			signFg := chrome.text
			if am.focusedField == amFieldOAuthSignIn {
				signFg = chrome.accent
			}
			signVal := "[ Sign in with " + vendor + " ]  (ctrl+o)"
			switch {
			case am.oauthAwaitingCode:
				signVal = "… waiting for pasted code (esc cancels)"
			case am.oauthSignedIn:
				signVal = "✓ signed in with " + vendor
				signFg = chrome.successFg
				if am.focusedField == amFieldOAuthSignIn {
					signVal += "  (enter re-links)"
				}
			}
			signRight := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(signFg).Width(max(1, fieldW-2)).Padding(0, 1).Render(signVal)
			addControl(amFieldOAuthSignIn, padStyled(labelCell("", am.focusedField == amFieldOAuthSignIn)+signRight, width, chrome.baseBg))
			if am.oauthAwaitingCode {
				addControl(amFieldOAuthCode, row("Code", am.oauthCodeInput, am.focusedField == amFieldOAuthCode))
			}
			if authFocused {
				var hints []string
				switch {
				case am.oauthAwaitingCode:
					hints = []string{
						"Approve TideMail in the browser; you'll land on an unreachable",
						"localhost page — paste that page's URL (or the code= value",
						"from it) below and press enter.",
					}
				case am.provider == "Outlook":
					hints = []string{
						"Press enter (or ctrl+o), sign in, and paste the localhost URL",
						"the browser lands on. Uses Thunderbird's shared client unless",
						"TIDEMAIL_MS_CLIENT_ID is set (which enables the device-code flow).",
						"‹ / › switches back to an App Password.",
					}
				default:
					hints = []string{
						"Press enter (or ctrl+o) to sign in — a short code + URL appears;",
						"open it on any device and approve.",
						"Needs TIDEMAIL_GOOGLE_CLIENT_ID / _SECRET (your own OAuth client).",
						"‹ / › switches back to an App Password.",
					}
				}
				for _, hint := range hints {
					addHint(hint)
				}
			}
		}
	}
	addSection("Sending identity")
	addControl(amFieldFrom, row("From", am.fromInput, am.focusedField == amFieldFrom))
	addControl(amFieldSignature, row("Signature", am.sigInput, am.focusedField == amFieldSignature))
	addSection("Sync")
	addControl(amFieldSyncInterval, row("Refresh", am.syncInput, am.focusedField == amFieldSyncInterval))
	addHint("0 = push (IMAP IDLE) · -1 = manual only")
	addHint("N = poll every N minutes")

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

	bodyH := max(1, height-3)
	offset := 0
	if anchor, ok := anchors[am.focusedField]; ok {
		offset = settingsScrollOffset(viewLineCount(rows), anchor, bodyH)
	}
	end := min(len(rows), offset+bodyH)
	body := clampView(
		lipgloss.NewStyle().Background(chrome.baseBg).Width(width).Render(
			strings.Join(rows[offset:end], "\n"),
		),
		width, bodyH, chrome.baseBg,
	)

	var actionPairs []string
	if !am.busy {
		actionPairs = []string{"↑↓", "move", "^s", "save", "^t", "test", "space", "toggle", "esc", "cancel"}
	}
	actions := renderSoftHints(width, chrome, actionPairs...)

	parts := []string{body}
	if statusLine != "" {
		parts = append(parts, statusLine)
	}
	parts = append(parts, actions)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (am AccountManager) viewConfirmDelete(width int, chrome managerChrome) string {
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
	actions := renderSoftHints(width, chrome, "y", "delete", "esc", "cancel")
	return lipgloss.JoinVertical(lipgloss.Left, body, actions)
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
