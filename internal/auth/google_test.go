package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestTokenJSONRoundTrip(t *testing.T) {
	orig := &oauth2.Token{
		AccessToken:  "access-123",
		TokenType:    "Bearer",
		RefreshToken: "refresh-456",
		Expiry:       time.Now().Add(time.Hour).Round(time.Second),
	}
	s, err := TokenJSON(orig)
	if err != nil {
		t.Fatalf("TokenJSON: %v", err)
	}
	got, err := TokenFromJSON(s)
	if err != nil {
		t.Fatalf("TokenFromJSON: %v", err)
	}
	if got.AccessToken != orig.AccessToken || got.RefreshToken != orig.RefreshToken || got.TokenType != orig.TokenType {
		t.Errorf("round-trip mismatch: got %+v want %+v", got, orig)
	}
	if !got.Expiry.Equal(orig.Expiry) {
		t.Errorf("expiry mismatch: got %v want %v", got.Expiry, orig.Expiry)
	}
}

func TestTokenFromJSONInvalid(t *testing.T) {
	if _, err := TokenFromJSON("not json"); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// withTokenEndpoint points the package OAuth endpoint at srv for the duration of a test.
func withTokenEndpoint(t *testing.T, srv *httptest.Server) {
	t.Helper()
	old := googleEndpoint
	googleEndpoint = oauth2.Endpoint{AuthURL: srv.URL + "/auth", TokenURL: srv.URL + "/token"}
	t.Cleanup(func() { googleEndpoint = old })
}

func TestRefreshAccessToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access",
			"token_type":    "Bearer",
			"refresh_token": "rt",
			"expires_in":    3600,
		})
	}))
	defer srv.Close()
	withTokenEndpoint(t, srv)

	tok, err := RefreshAccessToken("cid", "csecret", "the-refresh-token")
	if err != nil {
		t.Fatalf("RefreshAccessToken: %v", err)
	}
	if tok.AccessToken != "new-access" {
		t.Errorf("got access token %q, want new-access", tok.AccessToken)
	}
}

func TestRefreshAccessTokenServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "invalid_grant", http.StatusBadRequest)
	}))
	defer srv.Close()
	withTokenEndpoint(t, srv)

	if _, err := RefreshAccessToken("cid", "csecret", "bad-refresh-token"); err == nil {
		t.Fatal("expected error when the token endpoint returns 400")
	}
}
