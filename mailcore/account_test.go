package mailcore

import "testing"

func TestAccountConfigCarriesGoogleOAuth(t *testing.T) {
	cfg := Account{
		IMAPHost: "imap.gmail.com", User: "me@gmail.com",
		Provider: "Gmail", AuthMethod: "oauth2",
		ClientID: "id", ClientSecret: "secret", RefreshToken: "refresh",
	}.config()
	if !cfg.UsesGoogleOAuth2() {
		t.Fatal("expected the config to use Google OAuth2")
	}
	if cfg.ClientID != "id" || cfg.ClientSecret != "secret" || cfg.RefreshToken != "refresh" {
		t.Fatalf("oauth fields not carried over: %+v", cfg)
	}
}

func TestPasswordAccountDoesNotUseOAuth(t *testing.T) {
	cfg := Account{User: "me@example.com", Password: "pw"}.config()
	if cfg.UsesGoogleOAuth2() || cfg.UsesMicrosoftOAuth2() {
		t.Fatal("a password account must not use OAuth2")
	}
}
