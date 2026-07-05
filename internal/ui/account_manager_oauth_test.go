package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
)

func newOAuthTestAM(t *testing.T) AccountManager {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	am := NewAccountManager(database)
	am.oauthCfg = config.OAuthConfig{MSClientID: "ms-cid"}
	return am
}

// Editing a signed-in Outlook account (e.g. to change the sync interval) must
// preserve the refresh token on save, not fall back to password login.
func TestBuildCfgPreservesOAuthOnEdit(t *testing.T) {
	am := newOAuthTestAM(t)
	acfg := config.AccountConfig{
		Provider:     "Outlook",
		Name:         "work",
		User:         "me@outlook.com",
		RefreshToken: "refresh-xyz",
		ClientID:     "ms-cid",
		SyncMinutes:  5,
	}
	am.populateFormFrom(acfg)
	if !am.msSignedIn {
		t.Fatal("populateFormFrom should mark account as signed in")
	}
	am.syncInput.SetValue("10") // user changes sync interval

	got := am.buildCfg()
	if !got.UsesMicrosoftOAuth2() {
		t.Fatal("expected built config to remain Microsoft OAuth2, got password mode")
	}
	if got.RefreshToken != "refresh-xyz" {
		t.Fatalf("refresh token not preserved: %q", got.RefreshToken)
	}
	if got.Password != "" {
		t.Fatalf("expected empty password for OAuth2 account, got %q", got.Password)
	}
	if got.ClientID != "ms-cid" {
		t.Fatalf("client ID not filled from oauthCfg: %q", got.ClientID)
	}
	if got.SyncMinutes != 10 {
		t.Fatalf("expected sync interval 10, got %d", got.SyncMinutes)
	}
}

func TestValidateAccountForConnectOutlook(t *testing.T) {
	base := config.AccountConfig{
		Provider: "Outlook",
		Name:     "work",
		IMAPHost: "outlook.office365.com",
		IMAPPort: 993,
		SMTPPort: 587,
		User:     "me@outlook.com",
	}
	if status := validateAccountForConnect(base); !strings.Contains(status, "SIGN IN WITH MICROSOFT") {
		t.Errorf("credential-less Outlook should demand sign-in, got %q", status)
	}
	withToken := base
	withToken.RefreshToken = "rt"
	if status := validateAccountForConnect(withToken); status != "" {
		t.Errorf("token-only Outlook should validate, got %q", status)
	}
	withPassword := base
	withPassword.Password = "app-pw"
	if status := validateAccountForConnect(withPassword); status != "" {
		t.Errorf("password Outlook should validate (M365 app passwords), got %q", status)
	}
}

func TestStartMSOAuthWithoutClientID(t *testing.T) {
	am := newOAuthTestAM(t)
	am.oauthCfg = config.OAuthConfig{}
	am.mode = amAdd
	am.provider = "Outlook"
	next, cmd, _ := am.startMSOAuth()
	if cmd != nil {
		t.Fatal("expected no cmd when client ID is missing")
	}
	if !strings.Contains(next.statusMsg, "TIDEMAIL_MS_CLIENT_ID") {
		t.Errorf("expected not-configured hint, got %q", next.statusMsg)
	}
}

func TestUpdateFormDeviceFlowMessages(t *testing.T) {
	am := newOAuthTestAM(t)
	am.mode = amAdd
	am.provider = "Outlook"

	// Start: ctrl+o kicks off the flow.
	next, cmd, _ := am.Update(tea.KeyMsg{Type: tea.KeyCtrlO}, DefaultKeys)
	if cmd == nil || !next.msOAuthActive || !next.busy {
		t.Fatalf("ctrl+o should start the device flow (cmd=%v active=%v busy=%v)", cmd, next.msOAuthActive, next.busy)
	}

	// Device code arrives: shown to the user, polling cmd dispatched.
	next, cmd, _ = next.Update(DeviceCodeMsg{VerificationURL: "https://microsoft.com/devicelogin", UserCode: "ABCD-EFGH"}, DefaultKeys)
	if cmd == nil {
		t.Fatal("DeviceCodeMsg should dispatch the polling cmd")
	}
	if !strings.Contains(next.busyMsg, "ABCD-EFGH") || !strings.Contains(next.busyMsg, "microsoft.com/devicelogin") {
		t.Errorf("busyMsg should show URL and code, got %q", next.busyMsg)
	}

	// Approval: token stored, form usable again.
	next, _, _ = next.Update(OAuth2DoneMsg{RefreshToken: "refresh-new"}, DefaultKeys)
	if !next.msSignedIn || next.msRefreshToken != "refresh-new" {
		t.Fatalf("OAuth2DoneMsg should mark signed in (signed=%v token=%q)", next.msSignedIn, next.msRefreshToken)
	}
	if next.busy || next.msOAuthActive {
		t.Error("flow state should be cleared after success")
	}
	if !strings.HasPrefix(next.statusMsg, "SIGNED IN") {
		t.Errorf("statusMsg = %q", next.statusMsg)
	}
}

func TestUpdateFormDropsStaleOAuthDone(t *testing.T) {
	am := newOAuthTestAM(t)
	am.mode = amAdd
	am.provider = "Outlook"
	// No active flow — a late result from a cancelled flow must be ignored.
	next, _, _ := am.Update(OAuth2DoneMsg{RefreshToken: "late-token"}, DefaultKeys)
	if next.msSignedIn || next.msRefreshToken != "" {
		t.Errorf("stale OAuth2DoneMsg must be dropped (signed=%v token=%q)", next.msSignedIn, next.msRefreshToken)
	}
}

func TestUpdateFormEscCancelsDeviceFlow(t *testing.T) {
	am := newOAuthTestAM(t)
	am.mode = amAdd
	am.provider = "Outlook"
	ctx, cancel := context.WithCancel(context.Background())
	am.oauthCtx = ctx
	am.oauthCancel = cancel
	am.msOAuthActive = true
	am.busy = true
	am.busyMsg = "GO TO ... AND ENTER CODE ..."

	next, _, _ := am.Update(tea.KeyMsg{Type: tea.KeyEsc}, DefaultKeys)
	if next.msOAuthActive || next.busy {
		t.Error("esc should cancel the active device flow")
	}
	select {
	case <-ctx.Done():
	default:
		t.Error("esc should cancel the polling context")
	}
	if next.mode != amAdd {
		t.Error("esc during device flow should stay on the form, not close it")
	}
	if !strings.Contains(next.statusMsg, "CANCELLED") {
		t.Errorf("statusMsg = %q", next.statusMsg)
	}
}

// The shipped Thunderbird client ID forbids the device-code flow, so the
// account manager must run the authorization-code (paste the code) flow.
func TestUpdateFormAuthCodeFlow(t *testing.T) {
	oldCopy := clipboardCopy
	clipboardCopy = func(string) error { return nil }
	t.Cleanup(func() { clipboardCopy = oldCopy })

	am := newOAuthTestAM(t)
	am.oauthCfg = config.OAuthConfig{MSClientID: config.ThunderbirdMSClientID}
	am.mode = amAdd
	am.provider = "Outlook"

	// ctrl+o opens the browser and switches to code entry — the form stays
	// interactive (not busy) so the user can paste.
	next, cmd, _ := am.Update(tea.KeyMsg{Type: tea.KeyCtrlO}, DefaultKeys)
	if cmd == nil {
		t.Fatal("expected browser-open cmd")
	}
	if !next.msAwaitingCode || next.busy {
		t.Fatalf("expected awaiting-code state (awaiting=%v busy=%v)", next.msAwaitingCode, next.busy)
	}
	if next.focusedField != amFieldMSCode {
		t.Fatalf("expected code field focused, got %v", next.focusedField)
	}
	if next.msFlow == nil || !strings.Contains(next.msFlow.AuthURL, config.ThunderbirdMSClientID) {
		t.Fatal("expected auth-code flow with the Thunderbird client ID")
	}

	// Enter with nothing pasted: prompt, stay put.
	next, cmd, _ = next.Update(tea.KeyMsg{Type: tea.KeyEnter}, DefaultKeys)
	if cmd != nil || !strings.Contains(next.statusMsg, "PASTE") {
		t.Fatalf("empty submit should prompt for the code, got cmd=%v status=%q", cmd, next.statusMsg)
	}

	// Paste + enter: exchange cmd dispatched, form busy.
	next.msCodeInput.SetValue("https://localhost/?code=the-code&state=tidemail")
	next, cmd, _ = next.Update(tea.KeyMsg{Type: tea.KeyEnter}, DefaultKeys)
	if cmd == nil || !next.busy {
		t.Fatalf("expected exchange cmd and busy form (cmd=%v busy=%v)", cmd, next.busy)
	}

	// A failed exchange (bad paste) keeps the flow alive for a retry.
	next, _, _ = next.Update(OAuth2DoneMsg{Err: errors.New("bad code")}, DefaultKeys)
	if !next.msAwaitingCode || next.msFlow == nil || next.busy {
		t.Fatalf("failed exchange should keep flow alive (awaiting=%v flow=%v busy=%v)", next.msAwaitingCode, next.msFlow != nil, next.busy)
	}

	// Success: signed in, flow state cleared.
	next, _, _ = next.Update(OAuth2DoneMsg{RefreshToken: "refresh-new"}, DefaultKeys)
	if !next.msSignedIn || next.msRefreshToken != "refresh-new" {
		t.Fatalf("expected signed in (signed=%v token=%q)", next.msSignedIn, next.msRefreshToken)
	}
	if next.msAwaitingCode || next.msFlow != nil {
		t.Error("flow state should be cleared after success")
	}
}

func TestUpdateFormEscCancelsAuthCodeEntry(t *testing.T) {
	oldCopy := clipboardCopy
	clipboardCopy = func(string) error { return nil }
	t.Cleanup(func() { clipboardCopy = oldCopy })

	am := newOAuthTestAM(t)
	am.oauthCfg = config.OAuthConfig{MSClientID: config.ThunderbirdMSClientID}
	am.mode = amAdd
	am.provider = "Outlook"
	next, _, _ := am.Update(tea.KeyMsg{Type: tea.KeyCtrlO}, DefaultKeys)

	next, _, _ = next.Update(tea.KeyMsg{Type: tea.KeyEsc}, DefaultKeys)
	if next.msAwaitingCode || next.msOAuthActive {
		t.Error("esc should abandon the sign-in")
	}
	if next.mode != amAdd {
		t.Error("esc during code entry should stay on the form, not close it")
	}
	if !strings.Contains(next.statusMsg, "CANCELLED") {
		t.Errorf("statusMsg = %q", next.statusMsg)
	}
}
