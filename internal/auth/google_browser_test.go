package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func testBrowserFlow(t *testing.T, ctx context.Context) *GoogleBrowserFlow {
	t.Helper()
	f, err := StartGoogleBrowserFlow(ctx, "client", "secret")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.Close)
	return f
}

func callbackURL(f *GoogleBrowserFlow, state, code string) string {
	return f.redirectURI + "?" + url.Values{"state": {state}, "code": {code}}.Encode()
}

func TestGoogleBrowserCallback(t *testing.T) {
	var calls atomic.Int32
	var flow *GoogleBrowserFlow
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = r.ParseForm()
		if r.Form.Get("code") != "code-123" || r.Form.Get("redirect_uri") != flow.redirectURI ||
			r.Form.Get("code_verifier") != flow.verifier || r.Form.Get("grant_type") != "authorization_code" {
			t.Error("wrong code exchange parameters")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access","refresh_token":"refresh","token_type":"Bearer","expires_in":3600}`))
	}))
	defer srv.Close()
	withTokenEndpoint(t, srv)
	flow = testBrowserFlow(t, context.Background())
	authURL, _ := url.Parse(flow.AuthURL)
	q := authURL.Query()
	if q.Get("state") == "" || q.Get("state") == "tidemail" || q.Get("code_challenge") != oauth2.S256ChallengeFromVerifier(flow.verifier) || q.Get("redirect_uri") != flow.redirectURI {
		t.Fatal("missing state, PKCE or dynamic redirect")
	}
	// An unrelated local request cannot consume the valid attempt.
	resp, err := http.Get(callbackURL(flow, "wrong", "bad"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || calls.Load() != 0 {
		t.Fatal("invalid state accepted")
	}
	resp, err = http.Get(callbackURL(flow, flow.state, "code-123"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatal("valid callback rejected")
	}
	tok, err := flow.Wait()
	if err != nil || tok.RefreshToken != "refresh" || calls.Load() != 1 {
		t.Fatalf("callback failed: %v", err)
	}
	if err := flow.Submit("duplicate"); err == nil {
		t.Fatal("duplicate accepted")
	}
	if resp, err := http.Get(flow.redirectURI); err == nil {
		resp.Body.Close()
		t.Fatal("listener still open after completion")
	}
}

func TestGoogleBrowserManualSubmissionAndDuplicates(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		close(entered)
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access","refresh_token":"refresh"}`))
	}))
	defer srv.Close()
	withTokenEndpoint(t, srv)
	f := testBrowserFlow(t, context.Background())
	if err := f.Submit(callbackURL(f, "stale", "code")); err == nil {
		t.Fatal("bad pasted state accepted")
	}
	if err := f.Submit(callbackURL(f, f.state, "code")); err != nil {
		t.Fatal(err)
	}
	<-entered
	err := f.Submit("duplicate")
	close(release)
	if err == nil {
		t.Fatal("second submission accepted")
	}
	if _, err := f.Wait(); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("more than one exchange")
	}
}

func TestGoogleBrowserDenial(t *testing.T) {
	f := testBrowserFlow(t, context.Background())
	resp, err := http.Get(f.redirectURI + "?" + url.Values{"state": {f.state}, "error": {"access_denied"}}.Encode())
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if _, err := f.Wait(); err == nil {
		t.Fatal("denial did not fail the attempt")
	}
}

func TestGoogleBrowserCancellationAndTimeout(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		want := context.Canceled
		if timeout {
			cancel()
			ctx, cancel = context.WithTimeout(context.Background(), 10*time.Millisecond)
			want = context.DeadlineExceeded
		}
		f := testBrowserFlow(t, ctx)
		if !timeout {
			cancel()
		}
		_, err := f.Wait()
		cancel()
		if !errors.Is(err, want) {
			t.Fatalf("got %v, want %v", err, want)
		}
		if resp, err := http.Get(f.redirectURI); err == nil {
			resp.Body.Close()
			t.Fatal("listener remained open")
		}
	}
}

func TestGoogleBrowserExchangeFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer srv.Close()
	withTokenEndpoint(t, srv)
	f := testBrowserFlow(t, context.Background())
	if err := f.Submit("bad-code"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Wait(); err == nil {
		t.Fatal("exchange failure ignored")
	}
}

func TestGoogleAttemptsHaveDistinctStateAndVerifier(t *testing.T) {
	first := NewGoogleAuthCodeFlow("client", "secret")
	second := NewGoogleAuthCodeFlow("client", "secret")
	if first.state == second.state || first.verifier == second.verifier {
		t.Fatal("sign-in attempts must not share state or a PKCE verifier")
	}
	oldURL := first.redirectURI + "?" + url.Values{"state": {first.state}, "code": {"old-code"}}.Encode()
	if _, err := second.authorizationCode(oldURL); err == nil {
		t.Fatal("old pasted redirect accepted")
	}
}
