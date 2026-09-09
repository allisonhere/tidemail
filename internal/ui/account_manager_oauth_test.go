package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tidemail/internal/config"
)

func gmailFormManager() AccountManager {
	am := NewAccountManager(nil)
	am.mode = amAdd
	am.oauthCfg = config.OAuthConfig{GoogleClientID: "cid", GoogleClientSecret: "secret"}
	am.provider = "Gmail"
	am.useOAuth = true
	am.nameInput.SetValue("Gmail")
	am.userInput.SetValue("me@gmail.com")
	am.focusField(amFieldOAuthSignIn)
	return am
}

func TestBuildCfgPreservesGoogleOAuthOnEdit(t *testing.T) {
	am := NewAccountManager(nil)
	am.oauthCfg = config.OAuthConfig{GoogleClientID: "cid", GoogleClientSecret: "secret"}
	am.populateFormFrom(config.AccountConfig{
		Name:         "Gmail",
		Provider:     "Gmail",
		AuthMethod:   config.AuthOAuth2,
		IMAPHost:     "imap.gmail.com",
		IMAPPort:     993,
		SMTPPort:     587,
		User:         "me@gmail.com",
		RefreshToken: "refresh-xyz",
		SyncMinutes:  5,
	})
	am.syncInput.SetValue("10") // change an unrelated field

	cfg := am.buildCfg()
	if cfg.AuthMethod != config.AuthOAuth2 || !cfg.UsesGoogleOAuth2() {
		t.Fatalf("edited Gmail account lost its OAuth status: auth_method=%q", cfg.AuthMethod)
	}
	if cfg.RefreshToken != "refresh-xyz" {
		t.Fatalf("refresh token = %q, want refresh-xyz", cfg.RefreshToken)
	}
	if cfg.Password != "" {
		t.Fatalf("OAuth account should have no password, got %q", cfg.Password)
	}
	if cfg.ClientID != "cid" || cfg.ClientSecret != "secret" {
		t.Fatalf("client creds not filled from oauthCfg: %+v", cfg)
	}
	if cfg.SyncMinutes != 10 {
		t.Fatalf("sync minutes = %d, want 10", cfg.SyncMinutes)
	}
}

func TestStartGoogleOAuthNeedsClientID(t *testing.T) {
	am := gmailFormManager()
	am.oauthCfg = config.OAuthConfig{} // no client id
	am, _, _ = am.startOAuthSignIn()
	if !strings.Contains(am.statusMsg, "GOOGLE SIGN-IN UNAVAILABLE IN THIS BUILD") {
		t.Fatalf("expected a not-configured hint, got %q", am.statusMsg)
	}
	if am.oauthActive {
		t.Fatal("flow should not start without a client id")
	}
}

func TestGoogleSignInStartsBrowserFlow(t *testing.T) {
	am := gmailFormManager()
	am, cmd, _ := am.startOAuthSignIn()
	defer am.cancelOAuth()
	if !am.oauthActive || !am.busy || cmd == nil || !strings.Contains(am.busyMsg, "OPENING GOOGLE") {
		t.Fatal("Gmail should start browser sign-in directly")
	}
}

func TestGoogleOAuthDoneSuccess(t *testing.T) {
	am := gmailFormManager()
	am, _, _ = am.startOAuthSignIn()
	am, _, _ = am.updateForm(OAuth2DoneMsg{attempt: am.oauthCtx, RefreshToken: "1//new-refresh"}, DefaultKeys)
	if !am.oauthSignedIn || am.oauthRefreshToken != "1//new-refresh" {
		t.Fatalf("sign-in not recorded: signed=%v tok=%q", am.oauthSignedIn, am.oauthRefreshToken)
	}
	if am.oauthActive {
		t.Fatal("flow should be cleared after success")
	}
	if am.focusedField != amFieldFrom {
		t.Fatalf("focus = %v, want amFieldFrom", am.focusedField)
	}
}

func TestGoogleOAuthDoneErrorAllowsRestart(t *testing.T) {
	am := gmailFormManager()
	am, _, _ = am.startAuthCodeFlow()
	am, _, _ = am.updateForm(OAuth2DoneMsg{attempt: am.oauthCtx, Err: errStub("bad code")}, DefaultKeys)
	if am.oauthActive || am.oauthFlow != nil || am.busy {
		t.Fatal("failed Google exchange should close the attempt")
	}
	if !strings.Contains(am.statusMsg, "SIGN-IN FAILED") || !strings.Contains(am.statusMsg, "CTRL+O") {
		t.Fatalf("status = %q", am.statusMsg)
	}
}

func TestGoogleCodeRowHiddenUntilAwaiting(t *testing.T) {
	am := gmailFormManager() // OAuth method selected
	am.focusField(amFieldAuthMethod)
	am.advanceField(1)
	if am.focusedField != amFieldOAuthSignIn {
		t.Fatalf("focus = %v, want amFieldOAuthSignIn (code row skipped until awaiting)", am.focusedField)
	}
	am.advanceField(1)
	if am.focusedField == amFieldOAuthCode {
		t.Fatal("code row should be skipped when not awaiting a pasted code")
	}
}

func TestAuthMethodSelectorTogglesRows(t *testing.T) {
	am := gmailFormManager()
	am.useOAuth = false // App password
	am.focusField(amFieldAuthMethod)
	am.advanceField(1)
	if am.focusedField != amFieldPass {
		t.Fatalf("App-password mode: after selector expected amFieldPass, got %v", am.focusedField)
	}

	am.useOAuth = true // OAuth
	am.focusField(amFieldAuthMethod)
	am.advanceField(1)
	if am.focusedField != amFieldOAuthSignIn {
		t.Fatalf("OAuth mode: after selector expected amFieldOAuthSignIn, got %v", am.focusedField)
	}
	if am.focusedField == amFieldPass {
		t.Fatal("password row must be skipped in OAuth mode")
	}
}

func TestAuthMethodLeftRightFlipsMode(t *testing.T) {
	am := gmailFormManager()
	am.useOAuth = true
	am.focusField(amFieldAuthMethod)
	am, _, _ = am.updateForm(tea.KeyMsg{Type: tea.KeyLeft}, DefaultKeys)
	if am.useOAuth {
		t.Fatal("left arrow on the selector should flip to App password")
	}
	am, _, _ = am.updateForm(tea.KeyMsg{Type: tea.KeyRight}, DefaultKeys)
	if !am.useOAuth {
		t.Fatal("right arrow on the selector should flip back to OAuth")
	}
}

func TestAuthMethodSelectorAbsentForNonGmail(t *testing.T) {
	am := NewAccountManager(nil)
	am.mode = amAdd
	am.provider = "Custom"
	am.focusField(amFieldUser)
	am.advanceField(1)
	if am.focusedField == amFieldAuthMethod || am.focusedField == amFieldOAuthSignIn || am.focusedField == amFieldOAuthCode {
		t.Fatalf("non-Gmail form landed on a Gmail-only row: %v", am.focusedField)
	}
	if am.focusedField != amFieldPass {
		t.Fatalf("non-Gmail form should go User → Password, got %v", am.focusedField)
	}
}

func TestDisabledGoogleOAuthHidesSelectorAndShowsUserMessage(t *testing.T) {
	am := gmailFormManager()
	am.oauthCfg.GoogleDisabled = true
	am.mode = amAdd
	am.useOAuth = false
	am.focusField(amFieldUser)
	am.advanceField(1)
	if am.focusedField != amFieldPass {
		t.Fatalf("disabled Google OAuth should navigate directly to Password, got %v", am.focusedField)
	}

	view := ansi.Strip(am.viewForm(72, 30, newManagerChrome(72, CatppuccinMocha, false)))
	if strings.Contains(view, "OAuth · sign in with Google") || strings.Contains(view, "Sign in with Google") {
		t.Fatalf("disabled Google OAuth still rendered sign-in controls:\n%s", view)
	}
	if !strings.Contains(view, "Google OAuth is unavailable") || !strings.Contains(view, "Use a Google App Password instead") {
		t.Fatalf("disabled Google OAuth message missing:\n%s", view)
	}
}

func TestDisabledGoogleOAuthPreservesExistingOAuthAccountOnSave(t *testing.T) {
	am := editFormManager(config.AccountConfig{
		Name: "Gmail", Provider: "Gmail", AuthMethod: config.AuthOAuth2,
		IMAPHost: "imap.gmail.com", IMAPPort: 993, SMTPPort: 587,
		User: "me@gmail.com", RefreshToken: "refresh-1",
	}, config.OAuthConfig{GoogleDisabled: true})

	cfg := am.buildCfg()
	if cfg.AuthMethod != config.AuthOAuth2 || cfg.RefreshToken != "refresh-1" {
		t.Fatalf("disabled preview converted existing OAuth account: auth=%q refresh=%q", cfg.AuthMethod, cfg.RefreshToken)
	}
}

func TestGmailFormRejectsSaveWithoutCredentials(t *testing.T) {
	am := gmailFormManager()
	cfg := am.buildCfg()
	if got := validateAccountForConnect(cfg); !strings.Contains(got, "SIGN IN WITH GOOGLE") {
		t.Fatalf("validation = %q, want a sign-in-or-password hint", got)
	}
}

func TestTypingRunesInFormFieldDoesNotNavigate(t *testing.T) {
	am := gmailFormManager()
	am.focusField(amFieldName)
	before := am.focusedField
	am, _, _ = am.updateForm(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}, DefaultKeys)
	if am.focusedField != before {
		t.Fatalf("typing 'j' in a text field moved focus %v -> %v", before, am.focusedField)
	}
}

// editFormManager mimics opening the Edit form for an existing account:
// resetForm → focus Provider → populateFormFrom.
func editFormManager(acfg config.AccountConfig, oauthCfg config.OAuthConfig) AccountManager {
	am := NewAccountManager(nil)
	am.oauthCfg = oauthCfg
	am.mode = amEdit
	am.resetForm()
	am.focusField(amFieldProvider)
	am.populateFormFrom(acfg)
	return am
}

func cycleProvider(am AccountManager, presses int) AccountManager {
	am.focusField(amFieldProvider)
	dir := tea.KeyRight
	if presses < 0 {
		dir, presses = tea.KeyLeft, -presses
	}
	for i := 0; i < presses; i++ {
		am, _, _ = am.updateForm(tea.KeyMsg{Type: dir}, DefaultKeys)
	}
	return am
}

func TestAppPasswordGmailSurvivesStrayProviderCycle(t *testing.T) {
	am := editFormManager(config.AccountConfig{
		Name: "Gmail", Provider: "Gmail", AuthMethod: config.AuthPassword,
		IMAPHost: "imap.gmail.com", IMAPPort: 993, SMTPPort: 587,
		User: "me@gmail.com", Password: "app-pw",
	}, config.OAuthConfig{GoogleClientID: "cid", GoogleClientSecret: "sec"}) // client configured — the old bug vector

	if am.useOAuth {
		t.Fatal("an app-password Gmail account must not open in OAuth mode")
	}
	// Stray keystrokes on the Provider row, ending back on Gmail.
	am = cycleProvider(am, 2)  // Gmail → Outlook → Yahoo
	am = cycleProvider(am, -2) // Yahoo → Outlook → Gmail
	if am.provider != "Gmail" {
		t.Fatalf("provider = %q, want Gmail", am.provider)
	}
	if am.useOAuth {
		t.Fatal("cycling back to Gmail must restore App password, not default to OAuth")
	}
	cfg := am.buildCfg()
	if cfg.AuthMethod != config.AuthPassword {
		t.Fatalf("auth_method = %q, want password", cfg.AuthMethod)
	}
	if cfg.Password != "app-pw" || cfg.RefreshToken != "" {
		t.Fatalf("password lost / token gained: password=%q refresh=%q", cfg.Password, cfg.RefreshToken)
	}
	if cfg.UsesGoogleOAuth2() {
		t.Fatal("saved account still routes to XOAUTH2")
	}
}

func TestOAuthGmailRestoredAfterCyclingProviderAwayAndBack(t *testing.T) {
	am := editFormManager(config.AccountConfig{
		Name: "Gmail", Provider: "Gmail", AuthMethod: config.AuthOAuth2,
		IMAPHost: "imap.gmail.com", IMAPPort: 993, SMTPPort: 587,
		User: "me@gmail.com", RefreshToken: "refresh-1",
	}, config.OAuthConfig{GoogleClientID: "cid", GoogleClientSecret: "sec"})

	if !am.useOAuth {
		t.Fatal("an oauth2 Gmail account must open in OAuth mode")
	}
	am = cycleProvider(am, 1)  // Gmail → Outlook (token dropped)
	am = cycleProvider(am, -1) // Outlook → Gmail (should restore)
	if !am.useOAuth || am.oauthRefreshToken != "refresh-1" {
		t.Fatalf("cycling back to Gmail did not restore OAuth: useOAuth=%v token=%q", am.useOAuth, am.oauthRefreshToken)
	}
	if cfg := am.buildCfg(); cfg.AuthMethod != config.AuthOAuth2 || cfg.RefreshToken != "refresh-1" {
		t.Fatalf("buildCfg after restore: auth_method=%q refresh=%q", cfg.AuthMethod, cfg.RefreshToken)
	}
}

func TestNewGmailAccountDefaultsToOAuth(t *testing.T) {
	am := NewAccountManager(nil)
	am.mode = amAdd
	am.oauthCfg = config.OAuthConfig{GoogleClientID: "cid", GoogleClientSecret: "sec"} // configured
	am.resetForm()
	am.focusField(amFieldProvider)
	am = cycleProvider(am, 1) // Custom → Gmail
	if am.provider != "Gmail" {
		t.Fatalf("provider = %q", am.provider)
	}
	if !am.useOAuth {
		t.Fatal("a new Gmail account must default to OAuth")
	}
}

// ── Outlook / Microsoft ──────────────────────────────────────────────────────

func outlookFormManager(msClientID string) AccountManager {
	am := NewAccountManager(nil)
	am.mode = amAdd
	am.oauthCfg = config.OAuthConfig{MSClientID: msClientID}
	am.provider = "Outlook"
	am.useOAuth = true
	am.nameInput.SetValue("Work")
	am.userInput.SetValue("me@outlook.com")
	am.focusField(amFieldOAuthSignIn)
	return am
}

func TestOutlookSelectorShownAndDefaultsToOAuth(t *testing.T) {
	am := NewAccountManager(nil)
	am.mode = amAdd
	am.oauthCfg = config.OAuthConfig{MSClientID: config.ThunderbirdMSClientID}
	am.provider = "Custom"
	am.focusField(amFieldProvider)
	// cycle Custom → Gmail → Outlook
	am, _, _ = am.updateForm(tea.KeyMsg{Type: tea.KeyRight}, DefaultKeys)
	am, _, _ = am.updateForm(tea.KeyMsg{Type: tea.KeyRight}, DefaultKeys)
	if am.provider != "Outlook" {
		t.Fatalf("provider = %q, want Outlook", am.provider)
	}
	if !am.useOAuth {
		t.Fatal("Outlook should default to OAuth (Thunderbird client always configured)")
	}
	am.focusField(amFieldUser)
	am.advanceField(1)
	if am.focusedField != amFieldAuthMethod {
		t.Fatalf("focus = %v, want amFieldAuthMethod for Outlook", am.focusedField)
	}
}

func TestOutlookThunderbirdClientUsesPasteFlowDirectly(t *testing.T) {
	am := outlookFormManager(config.ThunderbirdMSClientID)
	am, cmd, _ := am.startOAuthSignIn()
	if am.busy {
		t.Fatal("Thunderbird client forbids device-code — should go straight to paste-back, not busy-poll")
	}
	if !am.oauthAwaitingCode || am.oauthFlow == nil {
		t.Fatal("expected the paste-back flow to be armed")
	}
	if cmd != nil {
		t.Fatal("paste-back start issues no command")
	}
	if am.focusedField != amFieldOAuthCode {
		t.Fatalf("focus = %v, want amFieldOAuthCode", am.focusedField)
	}
}

func TestOutlookCustomClientUsesDeviceFlow(t *testing.T) {
	am := outlookFormManager("my-azure-app")
	am, cmd, _ := am.startOAuthSignIn()
	if !am.busy || !am.oauthActive || cmd == nil {
		t.Fatal("a custom MS client should use the device-code flow (busy + poll cmd)")
	}
	am, _, _ = am.updateForm(DeviceCodeMsg{attempt: am.oauthCtx,
		VerificationURL: "https://microsoft.com/devicelogin",
		UserCode:        "ABCD-EFGH",
	}, DefaultKeys)
	if !strings.Contains(am.busyMsg, "ABCD-EFGH") || !strings.Contains(am.busyMsg, "devicelogin") {
		t.Fatalf("busy line missing MS url/code: %q", am.busyMsg)
	}
}

func TestOutlookBuildCfgUsesPublicClient(t *testing.T) {
	am := outlookFormManager(config.ThunderbirdMSClientID)
	am.oauthRefreshToken = "ms-refresh"
	am.oauthSignedIn = true
	cfg := am.buildCfg()
	if !cfg.UsesMicrosoftOAuth2() {
		t.Fatal("Outlook OAuth account not recognized")
	}
	if cfg.ClientID != config.ThunderbirdMSClientID || cfg.ClientSecret != "" {
		t.Fatalf("Outlook is a public client: want id=%s secret=\"\", got id=%q secret=%q",
			config.ThunderbirdMSClientID, cfg.ClientID, cfg.ClientSecret)
	}
	if cfg.Password != "" {
		t.Fatalf("OAuth account should have no password, got %q", cfg.Password)
	}
}

func TestOutlookOAuthDoneSuccess(t *testing.T) {
	am := outlookFormManager("my-azure-app")
	am, _, _ = am.startOAuthSignIn()
	am, _, _ = am.updateForm(OAuth2DoneMsg{attempt: am.oauthCtx, RefreshToken: "ms-new"}, DefaultKeys)
	if !am.oauthSignedIn || am.oauthRefreshToken != "ms-new" {
		t.Fatalf("sign-in not recorded: %v / %q", am.oauthSignedIn, am.oauthRefreshToken)
	}
	if !strings.Contains(am.statusMsg, "MICROSOFT") {
		t.Fatalf("status = %q, want a Microsoft link confirmation", am.statusMsg)
	}
}

func TestOutlookRejectsSaveWithoutCredentials(t *testing.T) {
	am := outlookFormManager(config.ThunderbirdMSClientID)
	if got := validateAccountForConnect(am.buildCfg()); !strings.Contains(got, "SIGN IN WITH MICROSOFT") {
		t.Fatalf("validation = %q", got)
	}
}

func TestPasteBackHintsAreNumberedSteps(t *testing.T) {
	am := outlookFormManager(config.ThunderbirdMSClientID)
	am, _, _ = am.startOAuthSignIn() // -> paste-back, awaiting code
	view := ansi.Strip(am.View(74, 40, BuildStyles(CatppuccinMocha, "compact", "square")))
	for _, want := range []string{
		"Finish signing in from your browser:",
		"1. Open the sign-in URL",
		"2. Sign in and approve",
		"3. Copy the URL it redirects",
		"4. Paste that here and press enter",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("paste-back hint missing %q in:\n%s", want, view)
		}
	}
}

func TestPasteBackShowsURLWhenClipboardFails(t *testing.T) {
	orig := clipboardCopy
	clipboardCopy = func(string) error { return errStub("no clipboard") }
	t.Cleanup(func() { clipboardCopy = orig })

	am := outlookFormManager(config.ThunderbirdMSClientID)
	am, _, _ = am.startOAuthSignIn()
	if am.oauthURLCopied {
		t.Fatal("oauthURLCopied should be false when the copy fails")
	}
	view := ansi.Strip(am.View(74, 44, BuildStyles(CatppuccinMocha, "compact", "square")))
	if !strings.Contains(view, "shown below") || !strings.Contains(view, "Sign-in URL:") {
		t.Fatalf("expected the URL to be shown inline, got:\n%s", view)
	}
	if !strings.Contains(view, "login.microsoftonline.com") {
		t.Fatalf("the actual sign-in URL should be rendered, got:\n%s", view)
	}
}

type errStub string

func (e errStub) Error() string { return string(e) }

func TestOAuthIgnoresPreviousAttemptResults(t *testing.T) {
	am := gmailFormManager()
	am, _, _ = am.startOAuthSignIn()
	previous := am.oauthCtx
	am.cancelOAuth()
	if previous.Err() == nil {
		t.Fatal("attempt was not canceled")
	}
	am, _, _ = am.startOAuthSignIn()
	current := am.oauthCtx
	for _, msg := range []tea.Msg{
		DeviceCodeMsg{attempt: previous, Err: errStub("canceled")},
		OAuth2DoneMsg{attempt: previous, Err: errStub("canceled")},
		OAuth2DoneMsg{attempt: previous, RefreshToken: "old-token"},
	} {
		var cmd tea.Cmd
		am, cmd, _ = am.updateForm(msg, DefaultKeys)
		if cmd != nil || !am.oauthActive || am.oauthCtx != current || am.oauthSignedIn || am.oauthAwaitingCode {
			t.Fatal("old result changed the current attempt")
		}
	}
	am, _, _ = am.updateForm(OAuth2DoneMsg{attempt: current, RefreshToken: "current-token"}, DefaultKeys)
	if am.oauthRefreshToken != "current-token" {
		t.Fatal("current result was not accepted")
	}
}
