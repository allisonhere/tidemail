package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// msTestServer stands in for login.microsoftonline.com. tokenResponses is
// consumed one per poll; a response of "pending" yields authorization_pending.
type msTestServer struct {
	srv        *httptest.Server
	tokenCalls atomic.Int32
	tokenFn    func(call int32, w http.ResponseWriter)
}

func newMSTestServer(t *testing.T, tokenFn func(call int32, w http.ResponseWriter)) *msTestServer {
	t.Helper()
	ts := &msTestServer{tokenFn: tokenFn}
	mux := http.NewServeMux()
	mux.HandleFunc("/devicecode", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"device_code":"dev-123","user_code":"ABCD-EFGH","verification_uri":"https://microsoft.com/devicelogin","expires_in":900,"interval":1}`)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		ts.tokenFn(ts.tokenCalls.Add(1), w)
	})
	ts.srv = httptest.NewServer(mux)
	t.Cleanup(ts.srv.Close)

	old := msEndpoint
	msEndpoint = oauth2.Endpoint{
		AuthURL:       ts.srv.URL + "/authorize",
		TokenURL:      ts.srv.URL + "/token",
		DeviceAuthURL: ts.srv.URL + "/devicecode",
	}
	t.Cleanup(func() { msEndpoint = old })
	return ts
}

func writePending(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	fmt.Fprint(w, `{"error":"authorization_pending"}`)
}

func writeToken(w http.ResponseWriter, access, refresh string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token":  access,
		"token_type":    "Bearer",
		"expires_in":    3600,
		"refresh_token": refresh,
	})
}

func TestStartMSDeviceFlow(t *testing.T) {
	newMSTestServer(t, func(_ int32, w http.ResponseWriter) { writePending(w) })
	da, err := StartMSDeviceFlow(context.Background(), "client-id")
	if err != nil {
		t.Fatalf("StartMSDeviceFlow: %v", err)
	}
	if da.UserCode != "ABCD-EFGH" {
		t.Errorf("user code = %q, want ABCD-EFGH", da.UserCode)
	}
	if da.VerificationURI != "https://microsoft.com/devicelogin" {
		t.Errorf("verification uri = %q", da.VerificationURI)
	}
}

func TestPollMSDeviceTokenPendingThenSuccess(t *testing.T) {
	newMSTestServer(t, func(call int32, w http.ResponseWriter) {
		if call == 1 {
			writePending(w)
			return
		}
		writeToken(w, "access-1", "refresh-1")
	})
	da, err := StartMSDeviceFlow(context.Background(), "client-id")
	if err != nil {
		t.Fatalf("StartMSDeviceFlow: %v", err)
	}
	tok, err := PollMSDeviceToken(context.Background(), "client-id", da)
	if err != nil {
		t.Fatalf("PollMSDeviceToken: %v", err)
	}
	if tok.AccessToken != "access-1" || tok.RefreshToken != "refresh-1" {
		t.Errorf("token = %q/%q, want access-1/refresh-1", tok.AccessToken, tok.RefreshToken)
	}
}

func TestPollMSDeviceTokenCancel(t *testing.T) {
	newMSTestServer(t, func(_ int32, w http.ResponseWriter) { writePending(w) })
	da, err := StartMSDeviceFlow(context.Background(), "client-id")
	if err != nil {
		t.Fatalf("StartMSDeviceFlow: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	done := make(chan error, 1)
	go func() {
		_, err := PollMSDeviceToken(ctx, "client-id", da)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error after cancel, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("PollMSDeviceToken did not return after ctx cancel")
	}
}

func TestMSAccessTokenCachesAndRotates(t *testing.T) {
	ts := newMSTestServer(t, func(_ int32, w http.ResponseWriter) {
		writeToken(w, "access-new", "refresh-rotated")
	})
	const account = "rotate-test"
	ForgetMSToken(account)
	t.Cleanup(func() { ForgetMSToken(account) })

	var persistedName, persistedToken string
	oldPersist := PersistRefreshToken
	PersistRefreshToken = func(name, tok string) { persistedName, persistedToken = name, tok }
	t.Cleanup(func() { PersistRefreshToken = oldPersist })

	got, err := MSAccessToken(context.Background(), "client-id", account, "refresh-seed")
	if err != nil {
		t.Fatalf("MSAccessToken: %v", err)
	}
	if got != "access-new" {
		t.Errorf("access token = %q, want access-new", got)
	}
	if persistedName != account || persistedToken != "refresh-rotated" {
		t.Errorf("persisted %q/%q, want %s/refresh-rotated", persistedName, persistedToken, account)
	}
	if tok, ok := LatestRefreshToken(account); !ok || tok != "refresh-rotated" {
		t.Errorf("LatestRefreshToken = %q,%v, want refresh-rotated,true", tok, ok)
	}

	// Second call must hit the cache, not the network.
	if _, err := MSAccessToken(context.Background(), "client-id", account, "refresh-seed"); err != nil {
		t.Fatalf("MSAccessToken (cached): %v", err)
	}
	if n := ts.tokenCalls.Load(); n != 1 {
		t.Errorf("token endpoint called %d times, want 1", n)
	}
}

func TestMSAccessTokenNoRefreshToken(t *testing.T) {
	const account = "empty-test"
	ForgetMSToken(account)
	t.Cleanup(func() { ForgetMSToken(account) })
	if _, err := MSAccessToken(context.Background(), "client-id", account, ""); err == nil ||
		!strings.Contains(err.Error(), "no refresh token") {
		t.Errorf("expected 'no refresh token' error, got %v", err)
	}
}

func TestExtractAuthCode(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"M.C511_BAY.2.abc-def", "M.C511_BAY.2.abc-def"},
		{"  M.C511_BAY.2.abc-def\n", "M.C511_BAY.2.abc-def"},
		{"https://localhost/?code=M.C511_BAY.2.abc&state=tidemail", "M.C511_BAY.2.abc"},
		{"https://localhost/?state=tidemail&code=M.C511_BAY.2.abc", "M.C511_BAY.2.abc"},
		{"https://localhost/#code=frag-code&state=x", "frag-code"},
		{"localhost/?code=no-scheme", "no-scheme"},
		{"", ""},
		{"error=access_denied&error_description=denied", ""},
	}
	for _, tc := range cases {
		if got := ExtractAuthCode(tc.in); got != tc.want {
			t.Errorf("ExtractAuthCode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestMSAuthCodeFlow(t *testing.T) {
	var gotCode, gotVerifier, gotRedirect string
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotCode = r.Form.Get("code")
		gotVerifier = r.Form.Get("code_verifier")
		gotRedirect = r.Form.Get("redirect_uri")
		writeToken(w, "access-ac", "refresh-ac")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	old := msEndpoint
	msEndpoint = oauth2.Endpoint{
		AuthURL:  srv.URL + "/authorize",
		TokenURL: srv.URL + "/token",
	}
	t.Cleanup(func() { msEndpoint = old })

	flow := NewMSAuthCodeFlow("tb-client-id")
	for _, frag := range []string{"client_id=tb-client-id", "code_challenge=", "code_challenge_method=S256", "redirect_uri=https%3A%2F%2Flocalhost", "prompt=select_account"} {
		if !strings.Contains(flow.AuthURL, frag) {
			t.Errorf("AuthURL missing %q: %s", frag, flow.AuthURL)
		}
	}

	tok, err := flow.Exchange(context.Background(), "https://localhost/?code=the-code&state=tidemail")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if tok.RefreshToken != "refresh-ac" {
		t.Errorf("refresh token = %q, want refresh-ac", tok.RefreshToken)
	}
	if gotCode != "the-code" {
		t.Errorf("server saw code %q, want the-code", gotCode)
	}
	if gotVerifier == "" {
		t.Error("server saw no code_verifier — PKCE not sent")
	}
	if gotRedirect != "https://localhost" {
		t.Errorf("server saw redirect_uri %q, want https://localhost", gotRedirect)
	}

	if _, err := flow.Exchange(context.Background(), "   "); err == nil {
		t.Error("Exchange with empty paste should error")
	}
}
