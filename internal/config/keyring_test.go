package config

import "testing"

// fillSecrets / stripSecrets shell out to secret-tool; without it every keychain
// lookup returns "" and every store fails. These tests exercise the parts that
// don't depend on a live keychain: the AuthMethod gating.

func TestFillSecretsSkipsClientCredsForPasswordAccount(t *testing.T) {
	cfg := &Config{
		OAuth:    OAuthConfig{GoogleClientID: "app-id", GoogleClientSecret: "app-secret", MSClientID: "ms-id"},
		Accounts: []AccountConfig{{Name: "Gmail", Provider: "Gmail", AuthMethod: AuthPassword}},
	}
	fillSecrets(cfg)
	a := cfg.Accounts[0]
	if a.ClientID != "" || a.ClientSecret != "" {
		t.Fatalf("app-password account got OAuth client creds filled: id=%q secret=%q", a.ClientID, a.ClientSecret)
	}
}

func TestFillSecretsFillsClientCredsForOAuthAccount(t *testing.T) {
	gmail := &Config{
		OAuth:    OAuthConfig{GoogleClientID: "app-id", GoogleClientSecret: "app-secret"},
		Accounts: []AccountConfig{{Name: "Gmail", Provider: "Gmail", AuthMethod: AuthOAuth2}},
	}
	fillSecrets(gmail)
	if a := gmail.Accounts[0]; a.ClientID != "app-id" || a.ClientSecret != "app-secret" {
		t.Fatalf("oauth2 Gmail account: id=%q secret=%q, want app-id/app-secret", a.ClientID, a.ClientSecret)
	}

	outlook := &Config{
		OAuth:    OAuthConfig{MSClientID: "ms-id"},
		Accounts: []AccountConfig{{Name: "Work", Provider: "Outlook", AuthMethod: AuthOAuth2}},
	}
	fillSecrets(outlook)
	if a := outlook.Accounts[0]; a.ClientID != "ms-id" || a.ClientSecret != "" {
		t.Fatalf("oauth2 Outlook account: id=%q secret=%q, want ms-id and no secret", a.ClientID, a.ClientSecret)
	}
}

func TestStripSecretsLeavesTokenOnPasswordAccountUntouched(t *testing.T) {
	// A token on a "password" account is inert (fillSecrets / Uses*OAuth2 gate on
	// AuthMethod). stripSecrets must not wipe it — a wrong auto-migration would
	// otherwise be unrecoverable.
	cfg := &Config{Accounts: []AccountConfig{
		{Name: "Gmail", Provider: "Gmail", AuthMethod: AuthPassword, RefreshToken: "keep-me"},
	}}
	stripSecrets(cfg)
	if got := cfg.Accounts[0].RefreshToken; got != "keep-me" {
		t.Fatalf("stripSecrets wiped a token off an app-password account: %q", got)
	}
}
