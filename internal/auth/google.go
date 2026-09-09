package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// IsTokenRevoked reports whether err is Google's "invalid_grant" response, which
// means the refresh token has expired or been revoked and the account must be
// re-authenticated. (A common cause: an OAuth consent screen left in "Testing"
// status, where Google expires refresh tokens after 7 days.)
func IsTokenRevoked(err error) bool {
	if err == nil {
		return false
	}
	var re *oauth2.RetrieveError
	if errors.As(err, &re) && re.ErrorCode == "invalid_grant" {
		return true
	}
	m := strings.ToLower(err.Error())
	return strings.Contains(m, "invalid_grant") || strings.Contains(m, "expired or revoked")
}

// GoogleScopes is the single restricted scope that grants full IMAP + SMTP
// access plus (with offline access) a refresh token.
var GoogleScopes = []string{"https://mail.google.com/"}

// googleEndpoint is Google's OAuth2 endpoint. It already carries the
// DeviceAuthURL. It's a package var (not google.Endpoint inline) so tests can
// point it at a local httptest server.
var googleEndpoint = google.Endpoint

func googleConfig(clientID, clientSecret string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Scopes:       GoogleScopes,
		Endpoint:     googleEndpoint,
	}
}

// StartGoogleDeviceFlow requests a device code from Google. The response carries
// the verification URL and user code to show the user, plus the polling handle
// for PollGoogleDeviceToken. Use with an OAuth client of type "TVs and Limited
// Input devices".
func StartGoogleDeviceFlow(ctx context.Context, clientID, clientSecret string) (*oauth2.DeviceAuthResponse, error) {
	da, err := googleConfig(clientID, clientSecret).DeviceAuth(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth: device code request: %w", err)
	}
	return da, nil
}

// PollGoogleDeviceToken polls the token endpoint until the user approves the
// device code, it expires, or ctx is cancelled. Blocking — run it from a
// goroutine / tea.Cmd.
func PollGoogleDeviceToken(ctx context.Context, clientID, clientSecret string, da *oauth2.DeviceAuthResponse) (*oauth2.Token, error) {
	tok, err := googleConfig(clientID, clientSecret).DeviceAccessToken(ctx, da)
	if err != nil {
		return nil, fmt.Errorf("auth: device sign-in: %w", err)
	}
	return tok, nil
}

// googleRedirectURI is the redirect the authorization-code (paste-back) flow
// lands on. Nothing listens there — the browser fails to load the page and the
// user copies the code out of the address bar. This is the standard
// mutt/getmail loopback flow and needs no local server (works over SSH).
const googleRedirectURI = "http://127.0.0.1"

// GoogleAuthCodeFlow is one authorization-code + PKCE sign-in attempt, shared
// by automatic loopback callbacks and the advanced manual fallback.
type GoogleAuthCodeFlow struct {
	// AuthURL is the sign-in page to open in a browser.
	AuthURL      string
	clientID     string
	clientSecret string
	verifier     string
	state        string
	redirectURI  string
}

// NewGoogleAuthCodeFlow builds the browser URL (with a fresh PKCE verifier) for
// an authorization-code sign-in. Exchange completes it with the pasted code.
func NewGoogleAuthCodeFlow(clientID, clientSecret string) *GoogleAuthCodeFlow {
	return newGoogleAuthCodeFlow(clientID, clientSecret, googleRedirectURI)
}

func newGoogleAuthCodeFlow(clientID, clientSecret, redirectURI string) *GoogleAuthCodeFlow {
	conf := googleConfig(clientID, clientSecret)
	conf.RedirectURL = redirectURI
	v := oauth2.GenerateVerifier()
	state := oauth2.GenerateVerifier()
	// prompt=consent + offline access so Google always returns a refresh token,
	// even on a repeat sign-in.
	authURL := conf.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.S256ChallengeOption(v),
		oauth2.SetAuthURLParam("prompt", "consent"),
	)
	return &GoogleAuthCodeFlow{AuthURL: authURL, clientID: clientID, clientSecret: clientSecret, verifier: v, state: state, redirectURI: redirectURI}
}

// authorizationCode validates full pasted redirects as strictly as callbacks.
// A bare code remains available only for the explicit manual-entry path.
func (f *GoogleAuthCodeFlow) authorizationCode(pasted string) (string, error) {
	pasted = strings.TrimSpace(pasted)
	if strings.Contains(pasted, "://") {
		u, err := url.Parse(pasted)
		redirect, _ := url.Parse(f.redirectURI)
		if err != nil || u.Scheme != redirect.Scheme || u.Host != redirect.Host ||
			strings.TrimRight(u.Path, "/") != strings.TrimRight(redirect.Path, "/") || u.Fragment != "" || u.User != nil {
			return "", fmt.Errorf("auth: paste the redirect URL from this sign-in attempt")
		}
		if u.Query().Get("state") != f.state {
			return "", fmt.Errorf("auth: sign-in state does not match; use the current sign-in URL")
		}
		if u.Query().Get("error") != "" {
			return "", fmt.Errorf("auth: Google access was denied; press Ctrl+O to try again")
		}
		if code := u.Query().Get("code"); code != "" {
			return code, nil
		}
		return "", fmt.Errorf("auth: no authorization code in redirect URL")
	}
	if code := ExtractAuthCode(pasted); code != "" {
		return code, nil
	}
	return "", fmt.Errorf("auth: no authorization code in pasted text")
}

// Exchange redeems a pasted authorization code or the full loopback redirect.
func (f *GoogleAuthCodeFlow) Exchange(ctx context.Context, pasted string) (*oauth2.Token, error) {
	code, err := f.authorizationCode(pasted)
	if err != nil {
		return nil, err
	}
	conf := googleConfig(f.clientID, f.clientSecret)
	conf.RedirectURL = f.redirectURI
	tok, err := conf.Exchange(ctx, code, oauth2.VerifierOption(f.verifier))
	if err != nil {
		return nil, fmt.Errorf("auth: code exchange: %w", err)
	}
	return tok, nil
}

// RefreshGoogleToken exchanges a refresh token for a new access token. Google
// may rotate the refresh token, so the returned token can carry a NEW refresh
// token that must replace the stored one.
func RefreshGoogleToken(ctx context.Context, clientID, clientSecret, refreshToken string) (*oauth2.Token, error) {
	src := googleConfig(clientID, clientSecret).TokenSource(ctx, &oauth2.Token{RefreshToken: refreshToken})
	tok, err := src.Token()
	if err != nil {
		return nil, fmt.Errorf("auth: refresh token: %w", err)
	}
	return tok, nil
}

// TokenJSON serializes an oauth2.Token to JSON.
func TokenJSON(tok *oauth2.Token) (string, error) {
	data, err := json.Marshal(tok)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// TokenFromJSON deserializes an oauth2.Token from JSON.
func TokenFromJSON(data string) (*oauth2.Token, error) {
	var tok oauth2.Token
	if err := json.Unmarshal([]byte(data), &tok); err != nil {
		return nil, err
	}
	return &tok, nil
}

// googleCache is the shared IMAP/SMTP access-token cache for Gmail accounts.
var googleCache = newTokenCache(RefreshGoogleToken)

// GoogleAccessToken returns a valid access token for the account, refreshing at
// most once per expiry window. cfgRefreshToken seeds the cache on first use;
// after a refresh the cache's rotated token wins.
func GoogleAccessToken(ctx context.Context, clientID, clientSecret, accountName, cfgRefreshToken string) (string, error) {
	return googleCache.accessToken(ctx, clientID, clientSecret, accountName, cfgRefreshToken)
}

// ForgetGoogleToken drops the account's cached tokens — used after re-auth so
// the next connection seeds from the freshly issued refresh token.
func ForgetGoogleToken(accountName string) { googleCache.forget(accountName) }
