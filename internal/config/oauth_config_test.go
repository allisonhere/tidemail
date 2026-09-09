package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestGoogleCredentialsPrecedence(t *testing.T) {
	oldID, oldSecret := DefaultGoogleClientID, DefaultGoogleClientSecret
	DefaultGoogleClientID, DefaultGoogleClientSecret = "bundled-id", "bundled-secret"
	t.Cleanup(func() { DefaultGoogleClientID, DefaultGoogleClientSecret = oldID, oldSecret })
	for _, tc := range []struct {
		name, saved, envID, envSecret, wantID, wantSecret string
	}{
		{"absent", "", "", "", "bundled-id", "bundled-secret"},
		{"empty saved", "[oauth]\ngoogle_client_id = ''\ngoogle_client_secret = ''", "", "", "bundled-id", "bundled-secret"},
		{"saved custom", "[oauth]\ngoogle_client_id = 'custom'\ngoogle_client_secret = 'custom-secret'", "", "", "custom", "custom-secret"},
		{"exports override empty", "[oauth]\ngoogle_client_id = ''\ngoogle_client_secret = ''", "env-id", "env-secret", "env-id", "env-secret"},
		{"exports override saved", "[oauth]\ngoogle_client_id = 'custom'\ngoogle_client_secret = 'custom-secret'", "env-id", "env-secret", "env-id", "env-secret"},
		{"custom never inherits bundled secret", "[oauth]\ngoogle_client_id = 'custom'", "", "", "custom", ""},
		{"changed env client never inherits saved secret", "[oauth]\ngoogle_client_id = 'custom'\ngoogle_client_secret = 'custom-secret'", "env-id", "", "env-id", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TIDEMAIL_GOOGLE_CLIENT_ID", tc.envID)
			t.Setenv("TIDEMAIL_GOOGLE_CLIENT_SECRET", tc.envSecret)
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			// No actual keychain reads in this config-loading regression test.
			t.Setenv("PATH", "")
			path, err := configPath()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			account := "\n[[account]]\nname = 'test'\nprovider = 'Gmail'\nauth_method = 'oauth2'\nrefresh_token = 'refresh'\npassword = 'unused'\n"
			if err := os.WriteFile(path, []byte(tc.saved+account), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.OAuth.GoogleClientID != tc.wantID || cfg.OAuth.GoogleClientSecret != tc.wantSecret {
				t.Fatal("wrong resolved app credentials")
			}
			if cfg.Accounts[0].ClientID != tc.wantID || cfg.Accounts[0].ClientSecret != tc.wantSecret {
				t.Fatal("account did not inherit resolved credentials")
			}
		})
	}
}

func TestBundledGoogleCredentialsNotPinnedOnSave(t *testing.T) {
	oldID, oldSecret := DefaultGoogleClientID, DefaultGoogleClientSecret
	DefaultGoogleClientID, DefaultGoogleClientSecret = "bundled", "secret"
	t.Cleanup(func() { DefaultGoogleClientID, DefaultGoogleClientSecret = oldID, oldSecret })
	t.Setenv("TIDEMAIL_GOOGLE_CLIENT_ID", "")
	t.Setenv("TIDEMAIL_GOOGLE_CLIENT_SECRET", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := DefaultConfig()
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	path, _ := configPath()
	var saved Config
	if _, err := toml.DecodeFile(path, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.OAuth.GoogleClientID != "" || saved.OAuth.GoogleClientSecret != "" {
		t.Fatal("bundled credentials should not be persisted as custom overrides")
	}
	if cfg.OAuth.GoogleClientID != "bundled" {
		t.Fatal("Save mutated running credentials")
	}
}

func TestGoogleAccountSurvivesSaveAndReload(t *testing.T) {
	t.Setenv("PATH", "") // Exercise the existing file fallback without a keychain.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TIDEMAIL_GOOGLE_CLIENT_ID", "test-client")
	t.Setenv("TIDEMAIL_GOOGLE_CLIENT_SECRET", "test-secret")
	cfg := DefaultConfig()
	cfg.Accounts = []AccountConfig{{Name: "Test Gmail", Provider: "Gmail", AuthMethod: AuthOAuth2, RefreshToken: "saved-refresh", User: "test@example.com"}}
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	account := reloaded.Accounts[0]
	if !account.UsesGoogleOAuth2() || account.RefreshToken != "saved-refresh" || account.ClientID != "test-client" || account.ClientSecret != "test-secret" {
		t.Fatal("saved Google account cannot reconnect with the same credentials")
	}
}
