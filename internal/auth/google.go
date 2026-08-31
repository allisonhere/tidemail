package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// IsAuthFailure reports whether err looks like a credential failure that
// requires the user to re-enter their account password — a rejected app
// password, or an expired/revoked OAuth token. The status layer uses this to
// surface a "re-authenticate" hint instead of a raw error.
func IsAuthFailure(err error) bool {
	if err == nil {
		return false
	}
	m := strings.ToLower(err.Error())
	for _, sig := range []string{
		"authenticationfailed", // IMAP LOGIN rejected
		"authenticate failed",  // IMAP AUTHENTICATE (XOAUTH2) rejected
		"invalid credentials",  // common SMTP/IMAP wording
		"username and password not accepted",
		"invalid_grant", // OAuth token expired/revoked
		"expired or revoked",
	} {
		if strings.Contains(m, sig) {
			return true
		}
	}
	return false
}

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
const googleRedirectURI = "http://localhost"

// GoogleAuthCodeFlow is one authorization-code + PKCE sign-in attempt. Used as
// the fallback when the device-code flow rejects the mail scope.
type GoogleAuthCodeFlow struct {
	// AuthURL is the sign-in page to open in a browser.
	AuthURL      string
	clientID     string
	clientSecret string
	verifier     string
}

// NewGoogleAuthCodeFlow builds the browser URL (with a fresh PKCE verifier) for
// an authorization-code sign-in. Exchange completes it with the pasted code.
func NewGoogleAuthCodeFlow(clientID, clientSecret string) *GoogleAuthCodeFlow {
	conf := googleConfig(clientID, clientSecret)
	conf.RedirectURL = googleRedirectURI
	v := oauth2.GenerateVerifier()
	// prompt=consent + offline access so Google always returns a refresh token,
	// even on a repeat sign-in.
	authURL := conf.AuthCodeURL("tidemail",
		oauth2.AccessTypeOffline,
		oauth2.S256ChallengeOption(v),
		oauth2.SetAuthURLParam("prompt", "consent"),
	)
	return &GoogleAuthCodeFlow{AuthURL: authURL, clientID: clientID, clientSecret: clientSecret, verifier: v}
}

// Exchange redeems the pasted authorization code (or the full
// http://localhost/?code=... URL the browser landed on) for tokens.
func (f *GoogleAuthCodeFlow) Exchange(ctx context.Context, pasted string) (*oauth2.Token, error) {
	code := ExtractAuthCode(pasted)
	if code == "" {
		return nil, fmt.Errorf("auth: no authorization code in pasted text")
	}
	conf := googleConfig(f.clientID, f.clientSecret)
	conf.RedirectURL = googleRedirectURI
	tok, err := conf.Exchange(ctx, code, oauth2.VerifierOption(f.verifier))
	if err != nil {
		return nil, fmt.Errorf("auth: code exchange: %w", err)
	}
	return tok, nil
}

// ExtractAuthCode accepts either a bare authorization code or the full redirect
// URL (query or fragment form) and returns the code.
func ExtractAuthCode(pasted string) string {
	s := strings.TrimSpace(pasted)
	if u, err := url.Parse(s); err == nil {
		if c := u.Query().Get("code"); c != "" {
			return c
		}
		if q, err := url.ParseQuery(u.Fragment); err == nil {
			if c := q.Get("code"); c != "" {
				return c
			}
		}
	}
	if strings.ContainsAny(s, " \n?&=") {
		return "" // looks like URL debris, not a bare code
	}
	return s
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

// PersistRefreshToken is called with the rotated refresh token after every
// successful refresh so it survives crashes. Wired to the keyring at startup
// (kept as an injected func to avoid an auth→config import).
var PersistRefreshToken = func(accountName, refreshToken string) {}

type googleTokenState struct {
	access  string
	expiry  time.Time
	refresh string
}

var (
	googleTokensMu sync.Mutex
	googleTokens   = map[string]*googleTokenState{}
)

// expiryMargin refreshes tokens slightly early so a token that's valid when
// fetched doesn't expire mid-IMAP-session-setup.
const expiryMargin = 2 * time.Minute

// GoogleAccessToken returns a valid access token for the account, refreshing at
// most once per expiry window. The cache is shared by IMAP and SMTP so a send
// right after a sync reuses the token. cfgRefreshToken seeds the cache on first
// use; after a refresh the cache's rotated token wins.
func GoogleAccessToken(ctx context.Context, clientID, clientSecret, accountName, cfgRefreshToken string) (string, error) {
	googleTokensMu.Lock()
	defer googleTokensMu.Unlock()

	st := googleTokens[accountName]
	if st == nil {
		st = &googleTokenState{refresh: cfgRefreshToken}
		googleTokens[accountName] = st
	}
	if st.access != "" && time.Now().Before(st.expiry.Add(-expiryMargin)) {
		return st.access, nil
	}
	if st.refresh == "" {
		return "", fmt.Errorf("auth: no refresh token for %s", accountName)
	}
	tok, err := RefreshGoogleToken(ctx, clientID, clientSecret, st.refresh)
	if err != nil {
		return "", err
	}
	st.access = tok.AccessToken
	st.expiry = tok.Expiry
	if tok.RefreshToken != "" && tok.RefreshToken != st.refresh {
		st.refresh = tok.RefreshToken
		PersistRefreshToken(accountName, tok.RefreshToken)
	}
	return st.access, nil
}

// LatestRefreshToken reports the account's current (possibly rotated) refresh
// token, if the cache holds one.
func LatestRefreshToken(accountName string) (string, bool) {
	googleTokensMu.Lock()
	defer googleTokensMu.Unlock()
	st := googleTokens[accountName]
	if st == nil || st.refresh == "" {
		return "", false
	}
	return st.refresh, true
}

// ForgetGoogleToken drops the account's cached tokens — used after re-auth so
// the next connection seeds from the freshly issued refresh token.
func ForgetGoogleToken(accountName string) {
	googleTokensMu.Lock()
	defer googleTokensMu.Unlock()
	delete(googleTokens, accountName)
}
