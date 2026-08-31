package ui

import (
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
)

func TestEmptyAccountsHintUsesUppercaseAccountManagerShortcut(t *testing.T) {
	for _, tt := range []struct {
		name  string
		model Model
	}{
		{
			name: "plain",
			model: Model{
				cfg:    config.DefaultConfig(),
				styles: Styles{PlainUI: true},
			},
		},
		{
			name: "icons",
			model: Model{
				cfg:    config.DefaultConfig(),
				styles: BuildStyles(CatppuccinMocha, "comfortable", "square"),
			},
		},
		{
			name: "no icons",
			model: Model{
				cfg: func() config.Config {
					cfg := config.DefaultConfig()
					cfg.Display.Icons = false
					return cfg
				}(),
				styles: BuildStyles(CatppuccinMocha, "comfortable", "square"),
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.model.emptyAccountsHint()
			if !strings.Contains(got, "press M to add accounts") {
				t.Fatalf("expected uppercase account shortcut, got %q", got)
			}
			if strings.Contains(got, "press m to add accounts") {
				t.Fatalf("expected no lowercase account shortcut, got %q", got)
			}
		})
	}
}

func TestLooksLikeMissingCredential(t *testing.T) {
	yes := []string{
		"login: imap: NO Empty username or password. 006d6...",
		"oauth2: auth: no refresh token for Gmail",
	}
	no := []string{
		"login: imap: NO [AUTHENTICATIONFAILED] Invalid credentials",
		"dial tcp: connection refused",
		"",
	}
	for _, s := range yes {
		if !looksLikeMissingCredential(errString(s)) {
			t.Errorf("expected %q to look like a missing credential", s)
		}
	}
	for _, s := range no {
		if looksLikeMissingCredential(errString(s)) {
			t.Errorf("did not expect %q to look like a missing credential", s)
		}
	}
	if looksLikeMissingCredential(nil) {
		t.Error("nil error must be false")
	}
}

func TestAnyAccountNeedsKeychain(t *testing.T) {
	cases := []struct {
		name string
		accs []config.AccountConfig
		want bool
	}{
		{"all secrets in memory", []config.AccountConfig{
			{AuthMethod: config.AuthPassword, Password: "pw"},
			{AuthMethod: config.AuthOAuth2, RefreshToken: "tok"},
		}, false},
		{"password account missing its password", []config.AccountConfig{
			{AuthMethod: config.AuthPassword},
		}, true},
		{"oauth account missing its token", []config.AccountConfig{
			{AuthMethod: config.AuthOAuth2},
		}, true},
		{"legacy blank auth method, has password", []config.AccountConfig{
			{Password: "pw"},
		}, false},
	}
	for _, tc := range cases {
		m := Model{cfg: config.Config{Accounts: tc.accs}}
		if got := m.anyAccountNeedsKeychain(); got != tc.want {
			t.Errorf("%s: anyAccountNeedsKeychain = %v, want %v", tc.name, got, tc.want)
		}
	}
}

type errString string

func (e errString) Error() string { return string(e) }
