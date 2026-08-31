package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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
		IMAPHost:     "imap.gmail.com",
		IMAPPort:     993,
		SMTPPort:     587,
		User:         "me@gmail.com",
		RefreshToken: "refresh-xyz",
		SyncMinutes:  5,
	})
	am.syncInput.SetValue("10") // change an unrelated field

	cfg := am.buildCfg()
	if !cfg.UsesGoogleOAuth2() {
		t.Fatal("edited Gmail account lost its OAuth status")
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
	if !strings.Contains(am.statusMsg, "TIDEMAIL_GOOGLE_CLIENT_ID") {
		t.Fatalf("expected a not-configured hint, got %q", am.statusMsg)
	}
	if am.oauthActive {
		t.Fatal("flow should not start without a client id")
	}
}

func TestGoogleDeviceCodeMsgShowsCodeInBusyLine(t *testing.T) {
	am := gmailFormManager()
	am, _, _ = am.startOAuthSignIn()
	if !am.oauthActive || !am.busy {
		t.Fatal("startGoogleOAuth should mark the flow active and busy")
	}
	am, cmd, _ := am.updateForm(DeviceCodeMsg{
		VerificationURL: "https://www.google.com/device",
		UserCode:        "WXYZ-ABCD",
	}, DefaultKeys)
	if cmd == nil {
		t.Fatal("expected a poll command after the device code arrives")
	}
	if !strings.Contains(am.busyMsg, "WXYZ-ABCD") || !strings.Contains(am.busyMsg, "google.com/device") {
		t.Fatalf("busy line missing url/code: %q", am.busyMsg)
	}
}

func TestGoogleDeviceCodeErrorFallsBackToPasteFlow(t *testing.T) {
	am := gmailFormManager()
	am, _, _ = am.startOAuthSignIn()
	am, _, _ = am.updateForm(DeviceCodeMsg{Err: errStub("device flow not supported for scope")}, DefaultKeys)
	if !am.oauthAwaitingCode {
		t.Fatal("a device-code error should switch to the paste-back flow")
	}
	if am.oauthFlow == nil {
		t.Fatal("paste-back flow object not created")
	}
	if am.focusedField != amFieldOAuthCode {
		t.Fatalf("focus = %v, want amFieldOAuthCode", am.focusedField)
	}
}

func TestGoogleOAuthDoneSuccess(t *testing.T) {
	am := gmailFormManager()
	am, _, _ = am.startOAuthSignIn()
	am, _, _ = am.updateForm(OAuth2DoneMsg{RefreshToken: "1//new-refresh"}, DefaultKeys)
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

func TestGoogleOAuthDoneErrorKeepsPasteFlowAlive(t *testing.T) {
	am := gmailFormManager()
	am, _, _ = am.startOAuthSignIn()
	am, _, _ = am.updateForm(DeviceCodeMsg{Err: errStub("nope")}, DefaultKeys) // -> paste flow
	am, _, _ = am.updateForm(OAuth2DoneMsg{Err: errStub("bad code")}, DefaultKeys)
	if !am.oauthAwaitingCode || am.oauthFlow == nil {
		t.Fatal("a bad paste should keep the sign-in alive for a retry")
	}
	if !strings.Contains(am.statusMsg, "SIGN-IN FAILED") {
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
	am, _, _ = am.updateForm(DeviceCodeMsg{
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
	am, _, _ = am.updateForm(OAuth2DoneMsg{RefreshToken: "ms-new"}, DefaultKeys)
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

type errStub string

func (e errStub) Error() string { return string(e) }
