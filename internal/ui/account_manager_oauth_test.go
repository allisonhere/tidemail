package ui

import (
	"testing"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
)

// Editing a signed-in OAuth2 account (e.g. to change the sync interval) must
// preserve the refresh token on save, not fall back to password login.
func TestBuildCfgPreservesOAuthOnEdit(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	am := NewAccountManager(database)
	am.oauthCfg = config.OAuthConfig{GoogleClientID: "cid", GoogleClientSecret: "secret"}

	acfg := config.AccountConfig{
		Provider:     "Gmail",
		Name:         "gmail",
		User:         "me@gmail.com",
		RefreshToken: "refresh-xyz",
		ClientID:     "cid",
		ClientSecret: "secret",
		SyncMinutes:  5,
	}
	am.populateFormFrom(acfg)
	am.syncInput.SetValue("10") // user changes sync interval

	got := am.buildCfg()
	if !got.UsesOAuth2() {
		t.Fatalf("expected built config to remain OAuth2, got password mode")
	}
	if got.RefreshToken != "refresh-xyz" {
		t.Fatalf("refresh token not preserved: %q", got.RefreshToken)
	}
	if got.Password != "" {
		t.Fatalf("expected empty password for OAuth2 account, got %q", got.Password)
	}
	if got.SyncMinutes != 10 {
		t.Fatalf("expected sync interval 10, got %d", got.SyncMinutes)
	}
}
