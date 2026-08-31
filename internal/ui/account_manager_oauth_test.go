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
	am.useGoogleOAuth = true
	am.nameInput.SetValue("Gmail")
	am.userInput.SetValue("me@gmail.com")
	am.focusField(amFieldGoogleSignIn)
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
	am, _, _ = am.startGoogleOAuth()
	if !strings.Contains(am.statusMsg, "TIDEMAIL_GOOGLE_CLIENT_ID") {
		t.Fatalf("expected a not-configured hint, got %q", am.statusMsg)
	}
	if am.googleOAuthActive {
		t.Fatal("flow should not start without a client id")
	}
}

func TestGoogleDeviceCodeMsgShowsCodeInBusyLine(t *testing.T) {
	am := gmailFormManager()
	am, _, _ = am.startGoogleOAuth()
	if !am.googleOAuthActive || !am.busy {
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
	am, _, _ = am.startGoogleOAuth()
	am, _, _ = am.updateForm(DeviceCodeMsg{Err: errStub("device flow not supported for scope")}, DefaultKeys)
	if !am.googleAwaitingCode {
		t.Fatal("a device-code error should switch to the paste-back flow")
	}
	if am.googleFlow == nil {
		t.Fatal("paste-back flow object not created")
	}
	if am.focusedField != amFieldGoogleCode {
		t.Fatalf("focus = %v, want amFieldGoogleCode", am.focusedField)
	}
}

func TestGoogleOAuthDoneSuccess(t *testing.T) {
	am := gmailFormManager()
	am, _, _ = am.startGoogleOAuth()
	am, _, _ = am.updateForm(OAuth2DoneMsg{RefreshToken: "1//new-refresh"}, DefaultKeys)
	if !am.googleSignedIn || am.googleRefreshToken != "1//new-refresh" {
		t.Fatalf("sign-in not recorded: signed=%v tok=%q", am.googleSignedIn, am.googleRefreshToken)
	}
	if am.googleOAuthActive {
		t.Fatal("flow should be cleared after success")
	}
	if am.focusedField != amFieldFrom {
		t.Fatalf("focus = %v, want amFieldFrom", am.focusedField)
	}
}

func TestGoogleOAuthDoneErrorKeepsPasteFlowAlive(t *testing.T) {
	am := gmailFormManager()
	am, _, _ = am.startGoogleOAuth()
	am, _, _ = am.updateForm(DeviceCodeMsg{Err: errStub("nope")}, DefaultKeys) // -> paste flow
	am, _, _ = am.updateForm(OAuth2DoneMsg{Err: errStub("bad code")}, DefaultKeys)
	if !am.googleAwaitingCode || am.googleFlow == nil {
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
	if am.focusedField != amFieldGoogleSignIn {
		t.Fatalf("focus = %v, want amFieldGoogleSignIn (code row skipped until awaiting)", am.focusedField)
	}
	am.advanceField(1)
	if am.focusedField == amFieldGoogleCode {
		t.Fatal("code row should be skipped when not awaiting a pasted code")
	}
}

func TestAuthMethodSelectorTogglesRows(t *testing.T) {
	am := gmailFormManager()
	am.useGoogleOAuth = false // App password
	am.focusField(amFieldAuthMethod)
	am.advanceField(1)
	if am.focusedField != amFieldPass {
		t.Fatalf("App-password mode: after selector expected amFieldPass, got %v", am.focusedField)
	}

	am.useGoogleOAuth = true // OAuth
	am.focusField(amFieldAuthMethod)
	am.advanceField(1)
	if am.focusedField != amFieldGoogleSignIn {
		t.Fatalf("OAuth mode: after selector expected amFieldGoogleSignIn, got %v", am.focusedField)
	}
	if am.focusedField == amFieldPass {
		t.Fatal("password row must be skipped in OAuth mode")
	}
}

func TestAuthMethodLeftRightFlipsMode(t *testing.T) {
	am := gmailFormManager()
	am.useGoogleOAuth = true
	am.focusField(amFieldAuthMethod)
	am, _, _ = am.updateForm(tea.KeyMsg{Type: tea.KeyLeft}, DefaultKeys)
	if am.useGoogleOAuth {
		t.Fatal("left arrow on the selector should flip to App password")
	}
	am, _, _ = am.updateForm(tea.KeyMsg{Type: tea.KeyRight}, DefaultKeys)
	if !am.useGoogleOAuth {
		t.Fatal("right arrow on the selector should flip back to OAuth")
	}
}

func TestAuthMethodSelectorAbsentForNonGmail(t *testing.T) {
	am := NewAccountManager(nil)
	am.mode = amAdd
	am.provider = "Custom"
	am.focusField(amFieldUser)
	am.advanceField(1)
	if am.focusedField == amFieldAuthMethod || am.focusedField == amFieldGoogleSignIn || am.focusedField == amFieldGoogleCode {
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

type errStub string

func (e errStub) Error() string { return string(e) }
