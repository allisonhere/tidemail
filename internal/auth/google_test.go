package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestIsAuthFailure(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("dial tcp: connection refused"), false},
		{errors.New("login: [AUTHENTICATIONFAILED] Invalid credentials"), true},
		{errors.New("535 5.7.8 Username and Password not accepted"), true},
		{errors.New(`oauth2: "invalid_grant" "Token has been expired or revoked."`), true},
		{errors.New("authenticate failed: AUTHENTICATE XOAUTH2 rejected"), true},
	}
	for _, tc := range cases {
		if got := IsAuthFailure(tc.err); got != tc.want {
			t.Errorf("IsAuthFailure(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

func TestIsTokenRevoked(t *testing.T) {
	typed := &oauth2.RetrieveError{ErrorCode: "invalid_grant"}
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"typed invalid_grant", typed, true},
		{"wrapped typed", errors.New("auth: refresh token: " + typed.Error()), true},
		{"string invalid_grant", errors.New(`oauth2: "invalid_grant" "Token has been expired or revoked."`), true},
		{"other typed code", &oauth2.RetrieveError{ErrorCode: "invalid_client"}, false},
		{"unrelated", errors.New("connection refused"), false},
	}
	for _, tc := range cases {
		if got := IsTokenRevoked(tc.err); got != tc.want {
			t.Errorf("%s: IsTokenRevoked = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestExtractAuthCode(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"4/0AbCd", "4/0AbCd"},
		{"http://localhost/?code=4/0AbCd&scope=x", "4/0AbCd"},
		{"http://localhost/#code=4/0AbCd&state=y", "4/0AbCd"},
		{"  http://localhost/?state=y&code=4%2F0AbCd  ", "4/0AbCd"},
		{"no code here at all", ""},
		{"http://localhost/?error=access_denied", ""},
	}
	for _, tc := range cases {
		if got := ExtractAuthCode(tc.in); got != tc.want {
			t.Errorf("ExtractAuthCode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTokenJSONRoundTrip(t *testing.T) {
	orig := &oauth2.Token{
		AccessToken:  "ya29.abc",
		RefreshToken: "1//refresh",
		TokenType:    "Bearer",
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
	if got.AccessToken != orig.AccessToken || got.RefreshToken != orig.RefreshToken || !got.Expiry.Equal(orig.Expiry) {
		t.Fatalf("round trip mismatch: %+v vs %+v", got, orig)
	}
}

// withTokenEndpoint points googleEndpoint at srv for the duration of the test.
func withTokenEndpoint(t *testing.T, srv *httptest.Server) {
	t.Helper()
	prev := googleEndpoint
	googleEndpoint = oauth2.Endpoint{
		AuthURL:       srv.URL + "/auth",
		TokenURL:      srv.URL + "/token",
		DeviceAuthURL: srv.URL + "/device",
	}
	t.Cleanup(func() { googleEndpoint = prev })
}

func TestRefreshGoogleToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.FormValue("grant_type") != "refresh_token" || r.FormValue("refresh_token") != "old-refresh" {
			http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access",
			"refresh_token": "rotated-refresh",
			"token_type":    "Bearer",
			"expires_in":    3600,
		})
	}))
	defer srv.Close()
	withTokenEndpoint(t, srv)

	tok, err := RefreshGoogleToken(context.Background(), "cid", "secret", "old-refresh")
	if err != nil {
		t.Fatalf("RefreshGoogleToken: %v", err)
	}
	if tok.AccessToken != "new-access" || tok.RefreshToken != "rotated-refresh" {
		t.Fatalf("unexpected token: %+v", tok)
	}
}

func TestRefreshGoogleTokenRevoked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`))
	}))
	defer srv.Close()
	withTokenEndpoint(t, srv)

	_, err := RefreshGoogleToken(context.Background(), "cid", "secret", "dead-refresh")
	if err == nil {
		t.Fatal("expected an error for a revoked refresh token")
	}
	if !IsTokenRevoked(err) {
		t.Fatalf("IsTokenRevoked should classify %v", err)
	}
}

func TestGoogleDeviceFlow(t *testing.T) {
	var approved bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/device":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code":      "dev-123",
				"user_code":        "WXYZ-ABCD",
				"verification_uri": "https://www.google.com/device",
				"expires_in":       600,
				"interval":         1,
			})
		case "/token":
			if !approved {
				approved = true
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "dev-access",
				"refresh_token": "dev-refresh",
				"token_type":    "Bearer",
				"expires_in":    3600,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	withTokenEndpoint(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	da, err := StartGoogleDeviceFlow(ctx, "cid", "secret")
	if err != nil {
		t.Fatalf("StartGoogleDeviceFlow: %v", err)
	}
	if da.UserCode != "WXYZ-ABCD" {
		t.Fatalf("user code = %q", da.UserCode)
	}
	tok, err := PollGoogleDeviceToken(ctx, "cid", "secret", da)
	if err != nil {
		t.Fatalf("PollGoogleDeviceToken: %v", err)
	}
	if tok.RefreshToken != "dev-refresh" {
		t.Fatalf("refresh token = %q", tok.RefreshToken)
	}
}

func TestGoogleAccessTokenCachesAndRotates(t *testing.T) {
	var refreshCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshCalls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access-1",
			"refresh_token": "rotated-1",
			"token_type":    "Bearer",
			"expires_in":    3600,
		})
	}))
	defer srv.Close()
	withTokenEndpoint(t, srv)

	acct := "cache-test-" + t.Name()
	t.Cleanup(func() { ForgetGoogleToken(acct) })

	var persisted string
	prev := PersistRefreshToken
	PersistRefreshToken = func(name, tok string) { persisted = tok }
	t.Cleanup(func() { PersistRefreshToken = prev })

	got, err := GoogleAccessToken(context.Background(), "cid", "secret", acct, "seed-refresh")
	if err != nil {
		t.Fatalf("GoogleAccessToken: %v", err)
	}
	if got != "access-1" {
		t.Fatalf("access token = %q", got)
	}
	if persisted != "rotated-1" {
		t.Fatalf("rotated refresh token not persisted: %q", persisted)
	}
	if latest, ok := LatestRefreshToken(acct); !ok || latest != "rotated-1" {
		t.Fatalf("LatestRefreshToken = %q,%v", latest, ok)
	}

	// A second call within the expiry window must not hit the network again.
	if _, err := GoogleAccessToken(context.Background(), "cid", "secret", acct, "seed-refresh"); err != nil {
		t.Fatalf("second GoogleAccessToken: %v", err)
	}
	if refreshCalls != 1 {
		t.Fatalf("expected 1 refresh call, got %d", refreshCalls)
	}
}

func TestNewGoogleAuthCodeFlowURL(t *testing.T) {
	f := NewGoogleAuthCodeFlow("my-client", "my-secret")
	if !strings.Contains(f.AuthURL, "client_id=my-client") {
		t.Fatalf("AuthURL missing client_id: %s", f.AuthURL)
	}
	if !strings.Contains(f.AuthURL, "code_challenge=") {
		t.Fatalf("AuthURL missing PKCE challenge: %s", f.AuthURL)
	}
	if !strings.Contains(f.AuthURL, "access_type=offline") {
		t.Fatalf("AuthURL missing offline access: %s", f.AuthURL)
	}
}
