package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func withMSTokenEndpoint(t *testing.T, srv *httptest.Server) {
	t.Helper()
	prev := msEndpoint
	msEndpoint = oauth2.Endpoint{AuthURL: srv.URL + "/authorize", TokenURL: srv.URL + "/token", DeviceAuthURL: srv.URL + "/devicecode"}
	t.Cleanup(func() { msEndpoint = prev })
}

func testMSBrowserFlow(t *testing.T, ctx context.Context) *MicrosoftBrowserFlow {
	t.Helper()
	f, err := StartMicrosoftBrowserFlow(ctx, "client-id")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.Close)
	return f
}

// msCallbackURL is the browser's redirect, delivered to the real listener (the redirect host is "localhost").
func msCallbackURL(f *MicrosoftBrowserFlow, state, code string) string {
	return "http://" + f.listenAddr + "/?" + url.Values{"state": {state}, "code": {code}}.Encode()
}

func TestMicrosoftBrowserAuthURLAndRedirect(t *testing.T) {
	f := testMSBrowserFlow(t, context.Background())
	redirect, err := f.redirectForTest()
	if err != nil || redirect.Scheme != "http" || redirect.Hostname() != "localhost" || redirect.Port() == "" || redirect.Path != "" {
		t.Fatalf("redirect must be http://localhost:<port>, got %q (%v)", f.redirectURI, err)
	}
	u, _ := url.Parse(f.AuthURL)
	q := u.Query()
	if q.Get("client_id") != "client-id" || q.Get("redirect_uri") != f.redirectURI || q.Get("prompt") != "select_account" {
		t.Fatalf("auth URL parameters wrong: %s", f.AuthURL)
	}
	if q.Get("state") == "" || q.Get("state") == "tidemail" || q.Get("code_challenge") != oauth2.S256ChallengeFromVerifier(f.verifier) || q.Get("code_challenge_method") != "S256" {
		t.Fatal("missing random state or PKCE")
	}
	for _, scope := range MSScopes {
		if !strings.Contains(q.Get("scope"), scope) {
			t.Errorf("scope %q missing from %q", scope, q.Get("scope"))
		}
	}
}

func TestMicrosoftBrowserCallbackExchangesOnce(t *testing.T) {
	var calls atomic.Int32
	var flow *MicrosoftBrowserFlow
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = r.ParseForm()
		if r.Form.Get("code") != "code-123" || r.Form.Get("redirect_uri") != flow.redirectURI ||
			r.Form.Get("code_verifier") != flow.verifier || r.Form.Get("grant_type") != "authorization_code" {
			t.Error("wrong code exchange parameters")
		}
		// A public client sends its ID in the form or as the basic-auth user, depending on the library's probing.
		if user, _, _ := r.BasicAuth(); r.Form.Get("client_id") != "client-id" && user != "client-id" {
			t.Error("client id not sent")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access","refresh_token":"refresh","token_type":"Bearer","expires_in":3600}`))
	}))
	defer srv.Close()
	withMSTokenEndpoint(t, srv)
	flow = testMSBrowserFlow(t, context.Background())

	// A request with the wrong state cannot consume the attempt.
	resp, err := http.Get(msCallbackURL(flow, "wrong", "bad"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || calls.Load() != 0 {
		t.Fatal("invalid state accepted")
	}
	// Other paths are not the redirect.
	resp, err = http.Get("http://" + flow.listenAddr + "/other?" + url.Values{"state": {flow.state}, "code": {"x"}}.Encode())
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatal("unexpected path accepted")
	}

	resp, err = http.Get(msCallbackURL(flow, flow.state, "code-123"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("valid callback rejected: %d", resp.StatusCode)
	}
	tok, err := flow.Wait()
	if err != nil || tok.RefreshToken != "refresh" || tok.AccessToken != "access" || calls.Load() != 1 {
		t.Fatalf("callback failed: %v, %d exchanges", err, calls.Load())
	}
	if resp, err := http.Get(msCallbackURL(flow, flow.state, "again")); err == nil {
		resp.Body.Close()
		t.Fatal("listener still open after completion")
	}
}

func TestMicrosoftBrowserDuplicateCallbackIsRefused(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		close(entered)
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"a","refresh_token":"r"}`))
	}))
	defer srv.Close()
	withMSTokenEndpoint(t, srv)
	f := testMSBrowserFlow(t, context.Background())
	resp, err := http.Get(msCallbackURL(f, f.state, "code"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	<-entered
	resp2, err := http.Get(msCallbackURL(f, f.state, "second"))
	if err == nil {
		resp2.Body.Close()
		if resp2.StatusCode == http.StatusOK {
			t.Fatal("a second redirect was accepted")
		}
	}
	close(release)
	if _, err := f.Wait(); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("more than one exchange")
	}
}

func TestMicrosoftBrowserDenial(t *testing.T) {
	f := testMSBrowserFlow(t, context.Background())
	resp, err := http.Get("http://" + f.listenAddr + "/?" + url.Values{"state": {f.state}, "error": {"access_denied"}}.Encode())
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if _, err := f.Wait(); err == nil {
		t.Fatal("denial did not fail the attempt")
	}
}

func TestMicrosoftBrowserCancellationAndTimeout(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		want := context.Canceled
		if timeout {
			cancel()
			ctx, cancel = context.WithTimeout(context.Background(), 10*time.Millisecond)
			want = context.DeadlineExceeded
		}
		f := testMSBrowserFlow(t, ctx)
		if !timeout {
			cancel()
		}
		_, err := f.Wait()
		cancel()
		if !errors.Is(err, want) {
			t.Fatalf("timeout=%v: got %v, want %v", timeout, err, want)
		}
		if resp, err := http.Get("http://" + f.listenAddr + "/"); err == nil {
			resp.Body.Close()
			t.Fatalf("timeout=%v: listener still open", timeout)
		}
	}
}

// After a new sign-in the cache must use the credential it is given, not the ended one it remembers.
func TestForgetMSTokenLetsANewSignInReplaceAnEndedOne(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		seen = append(seen, r.Form.Get("refresh_token"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access","token_type":"Bearer","expires_in":1}`))
	}))
	defer srv.Close()
	withMSTokenEndpoint(t, srv)
	const key = "me@outlook.com@outlook.office365.com"
	t.Cleanup(func() { ForgetMSToken(key) })

	if _, err := MSAccessToken(context.Background(), "client", key, "old-refresh"); err != nil {
		t.Fatal(err)
	}
	// Without forgetting, a later call with a new seed still uses the remembered one.
	if _, err := MSAccessToken(context.Background(), "client", key, "new-refresh"); err != nil {
		t.Fatal(err)
	}
	ForgetMSToken(key)
	if _, err := MSAccessToken(context.Background(), "client", key, "new-refresh"); err != nil {
		t.Fatal(err)
	}
	if len(seen) < 2 || seen[0] != "old-refresh" || seen[len(seen)-1] != "new-refresh" {
		t.Fatalf("refresh tokens sent: %v", seen)
	}
	for _, s := range seen[:len(seen)-1] {
		if s == "new-refresh" {
			t.Fatalf("the new token was used before the old session was forgotten: %v", seen)
		}
	}
}
