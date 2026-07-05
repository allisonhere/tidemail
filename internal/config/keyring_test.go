package config

import "testing"

// fillSecrets must give Outlook OAuth accounts the Microsoft client ID (public
// client — no secret) and leave legacy Google-token accounts on the Google
// credentials.
func TestFillSecretsClientCredentialsByProvider(t *testing.T) {
	cfg := Config{
		OAuth: OAuthConfig{
			GoogleClientID:     "google-cid",
			GoogleClientSecret: "google-secret",
			MSClientID:         "ms-cid",
		},
		Accounts: []AccountConfig{
			{Name: "fill-test-outlook", Provider: "Outlook", RefreshToken: "rt-1"},
			{Name: "fill-test-gmail", Provider: "Gmail", RefreshToken: "rt-2"},
			{Name: "fill-test-plain", Provider: "Custom", Password: "pw"},
		},
	}
	fillSecrets(&cfg)

	if got := cfg.Accounts[0]; got.ClientID != "ms-cid" || got.ClientSecret != "" {
		t.Errorf("outlook account: ClientID=%q ClientSecret=%q, want ms-cid and empty", got.ClientID, got.ClientSecret)
	}
	if got := cfg.Accounts[1]; got.ClientID != "google-cid" || got.ClientSecret != "google-secret" {
		t.Errorf("gmail account: ClientID=%q ClientSecret=%q, want google creds", got.ClientID, got.ClientSecret)
	}
	if got := cfg.Accounts[2]; got.ClientID != "" || got.ClientSecret != "" {
		t.Errorf("password account should get no client credentials, got %q/%q", got.ClientID, got.ClientSecret)
	}
}

func TestUsesMicrosoftOAuth2(t *testing.T) {
	cases := []struct {
		acfg AccountConfig
		want bool
	}{
		{AccountConfig{Provider: "Outlook", RefreshToken: "rt"}, true},
		{AccountConfig{Provider: "Outlook"}, false},                   // password Outlook
		{AccountConfig{Provider: "Gmail", RefreshToken: "rt"}, false}, // orphaned legacy token
		{AccountConfig{Provider: "Custom", RefreshToken: "rt"}, false},
	}
	for _, tc := range cases {
		if got := tc.acfg.UsesMicrosoftOAuth2(); got != tc.want {
			t.Errorf("UsesMicrosoftOAuth2(%s, token=%q) = %v, want %v", tc.acfg.Provider, tc.acfg.RefreshToken, got, tc.want)
		}
	}
}
