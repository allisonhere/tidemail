package ui

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/auth"
	"github.com/allisonhere/tidemail/internal/config"
	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/oauth2"
)

type googleTokenTransport func(*http.Request) (*http.Response, error)

func (f googleTokenTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func mockGoogleBrowser(t *testing.T, browserErr error) *string {
	t.Helper()
	oldStart, oldOpen, oldCopy := startGoogleBrowser, openSignInBrowser, clipboardCopy
	t.Cleanup(func() { startGoogleBrowser, openSignInBrowser, clipboardCopy = oldStart, oldOpen, oldCopy })
	client := &http.Client{Transport: googleTokenTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{"access_token":"access","refresh_token":"refresh","token_type":"Bearer"}`))}, nil
	})}
	startGoogleBrowser = func(ctx context.Context, id, secret string) (*auth.GoogleBrowserFlow, error) {
		return auth.StartGoogleBrowserFlow(context.WithValue(ctx, oauth2.HTTPClient, client), id, secret)
	}
	opened := new(string)
	openSignInBrowser = func(ctx context.Context, browser, u string) error {
		if browser != "test-browser" {
			t.Errorf("configured browser was not used: %q", browser)
		}
		*opened = u
		return browserErr
	}
	clipboardCopy = func(string) error { return errStub("no clipboard") }
	return opened
}

func TestGoogleAutomaticSignInAndSave(t *testing.T) {
	opened := mockGoogleBrowser(t, nil)
	am := gmailFormManager()
	am.oauthBrowser = "test-browser"
	am, start, _ := am.startOAuthSignIn()
	defer func() { am.cancelOAuth() }()
	am, wait, _ := am.updateForm(start(), DefaultKeys)
	if !am.busy || am.oauthAwaitingCode || am.oauthBrowserFlow == nil || *opened == "" {
		t.Fatal("browser attempt not armed")
	}
	u, _ := url.Parse(*opened)
	q := u.Query()
	callback := q.Get("redirect_uri") + "?" + url.Values{"state": {q.Get("state")}, "code": {"test-code"}}.Encode()
	resp, err := http.Get(callback)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	am, _, _ = am.updateForm(wait(), DefaultKeys)
	if !am.oauthSignedIn || am.oauthActive || am.busy || am.oauthRefreshToken != "refresh" {
		t.Fatal("automatic sign-in did not finish")
	}
	cfg := am.buildCfg()
	if !cfg.UsesGoogleOAuth2() || cfg.ClientID != "cid" || cfg.ClientSecret != "secret" || cfg.RefreshToken != "refresh" || cfg.Password != "" {
		t.Fatal("signed-in credentials not carried to account save")
	}
	am, save, _ := am.updateForm(tea.KeyMsg{Type: tea.KeyCtrlS}, DefaultKeys)
	if save == nil || !am.busy {
		t.Fatalf("Ctrl+S did not initiate save: %s", am.statusMsg)
	}
}

func TestGoogleManualFallback(t *testing.T) {
	for _, automaticFallback := range []bool{true, false} {
		t.Run(map[bool]string{true: "browser unavailable", false: "SSH manual shortcut"}[automaticFallback], func(t *testing.T) {
			var browserErr error
			if automaticFallback {
				browserErr = errStub("no browser")
			}
			mockGoogleBrowser(t, browserErr)
			am := gmailFormManager()
			am.oauthBrowser = "test-browser"
			am, start, _ := am.startOAuthSignIn()
			defer func() { am.cancelOAuth() }()
			am, wait, _ := am.updateForm(start(), DefaultKeys)
			attempt := am.oauthCtx
			if !automaticFallback {
				am, _, _ = am.updateForm(tea.KeyMsg{Type: tea.KeyCtrlP}, DefaultKeys)
			}
			if am.busy || !am.oauthAwaitingCode || am.oauthCtx != attempt {
				t.Fatal("manual fallback lost attempt")
			}
			am.oauthCodeInput.SetValue("not a valid code")
			am, _, _ = am.submitOAuthCode()
			if !am.oauthActive || am.busy {
				t.Fatal("invalid paste consumed attempt")
			}
			am.oauthCodeInput.SetValue("valid-code")
			am, cmd, _ := am.submitOAuthCode()
			if cmd != nil || !am.busy {
				t.Fatal("manual submission should use existing exchange worker")
			}
			am, _, _ = am.updateForm(wait(), DefaultKeys)
			if !am.oauthSignedIn {
				t.Fatalf("manual sign-in failed: %s", am.statusMsg)
			}
		})
	}
}

func TestGoogleListenerFailureFallsBack(t *testing.T) {
	oldStart, oldCopy := startGoogleBrowser, clipboardCopy
	t.Cleanup(func() { startGoogleBrowser, clipboardCopy = oldStart, oldCopy })
	startGoogleBrowser = func(context.Context, string, string) (*auth.GoogleBrowserFlow, error) {
		return nil, errStub("listen failed")
	}
	clipboardCopy = func(string) error { return nil }
	am := gmailFormManager()
	am, start, _ := am.startOAuthSignIn()
	am, timeout, _ := am.updateForm(start(), DefaultKeys)
	defer am.cancelOAuth()
	if !am.oauthAwaitingCode || am.oauthFlow == nil || timeout == nil {
		t.Fatal("no manual fallback")
	}
}

func TestGoogleBrowserCancelAndStaleReady(t *testing.T) {
	mockGoogleBrowser(t, nil)
	am := gmailFormManager()
	am.oauthBrowser = "test-browser"
	am, start, _ := am.startOAuthSignIn()
	ready := start().(googleBrowserReadyMsg)
	am, _, _ = am.updateForm(tea.KeyMsg{Type: tea.KeyEsc}, DefaultKeys)
	am, cmd, _ := am.updateForm(ready, DefaultKeys)
	if am.oauthActive || cmd != nil || am.busy {
		t.Fatal("late browser result revived canceled attempt")
	}
	if _, err := ready.flow.Wait(); err == nil {
		t.Fatal("stale browser listener not canceled")
	}
}

func TestGoogleFailureStates(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{context.DeadlineExceeded, "TIMED OUT"},
		{errStub("access denied"), "ACCESS WAS DENIED"},
		{errStub("network failure secret-token"), "CHECK YOUR CONNECTION"},
		{nil, "NO REFRESH TOKEN"},
	} {
		am := gmailFormManager()
		am, _, _ = am.startOAuthSignIn()
		am, _, _ = am.updateForm(OAuth2DoneMsg{attempt: am.oauthCtx, Err: tc.err}, DefaultKeys)
		if am.oauthActive || am.busy || !strings.Contains(am.statusMsg, tc.want) || strings.Contains(am.statusMsg, "secret-token") {
			t.Fatalf("wrong failure state: %s", am.statusMsg)
		}
	}
}

func TestGmailDefaultWithoutCustomConfig(t *testing.T) {
	am := gmailFormManager()
	am.oauthCfg = config.OAuthConfig{}
	if !am.defaultUseOAuth() {
		t.Fatal("Gmail should default to OAuth regardless of custom configuration")
	}
}

func TestGoogleBrowserLaunchFailure(t *testing.T) {
	if err := launchSignInBrowser(context.Background(), "/bin/false", "https://example.com"); err == nil {
		t.Fatal("launcher exit failure was ignored")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := launchSignInBrowser(ctx, "/bin/true", "https://example.com"); err != context.Canceled {
		t.Fatal("canceled attempt tried to launch browser")
	}
}
