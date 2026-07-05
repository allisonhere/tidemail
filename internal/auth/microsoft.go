package auth

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

// msEndpoint is the Microsoft identity platform endpoint on the /common tenant
// (personal Microsoft accounts and work/school accounts). It's a package var so
// tests can point it at a local httptest server.
var msEndpoint = oauth2.Endpoint{
	AuthURL:       "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
	TokenURL:      "https://login.microsoftonline.com/common/oauth2/v2.0/token",
	DeviceAuthURL: "https://login.microsoftonline.com/common/oauth2/v2.0/devicecode",
}

// MSScopes are the delegated scopes needed for IMAP + SMTP access plus a
// refresh token (offline_access). The resource host must be outlook.office.com:
// that's the resource Thunderbird's app registration has permissions for, and
// the identity platform rejects other hosts with AADSTS70011 invalid_scope.
var MSScopes = []string{
	"https://outlook.office.com/IMAP.AccessAsUser.All",
	"https://outlook.office.com/SMTP.Send",
	"offline_access",
}

func msConfig(clientID string) *oauth2.Config {
	// Public client: no secret. The device code flow has no PKCE.
	return &oauth2.Config{
		ClientID: clientID,
		Scopes:   MSScopes,
		Endpoint: msEndpoint,
	}
}

// msRedirectURI is the redirect the authorization-code flow lands on. Mozilla's
// Thunderbird app registration has https://localhost registered; the page won't
// load (nothing listens there) — the user copies the code out of the address
// bar and pastes it into TideMail. This is the standard mutt/getmail flow.
const msRedirectURI = "https://localhost"

// MSAuthCodeFlow is one authorization-code + PKCE sign-in attempt. Used with
// client registrations that forbid the device-code flow (notably Thunderbird's,
// which TideMail ships as its default client ID).
type MSAuthCodeFlow struct {
	// AuthURL is the sign-in page to open in a browser.
	AuthURL  string
	clientID string
	verifier string
}

// NewMSAuthCodeFlow builds the browser URL (with a fresh PKCE verifier) for an
// authorization-code sign-in. Exchange completes it with the pasted code.
func NewMSAuthCodeFlow(clientID string) *MSAuthCodeFlow {
	conf := msConfig(clientID)
	conf.RedirectURL = msRedirectURI
	v := oauth2.GenerateVerifier()
	// select_account: always show the account picker so a browser already
	// signed into the wrong Microsoft account doesn't silently authorize it.
	url := conf.AuthCodeURL("tidemail", oauth2.S256ChallengeOption(v),
		oauth2.SetAuthURLParam("prompt", "select_account"))
	return &MSAuthCodeFlow{AuthURL: url, clientID: clientID, verifier: v}
}

// Exchange redeems the pasted authorization code (or the full
// https://localhost/?code=... URL the browser landed on) for tokens.
func (f *MSAuthCodeFlow) Exchange(ctx context.Context, pasted string) (*oauth2.Token, error) {
	code := ExtractAuthCode(pasted)
	if code == "" {
		return nil, fmt.Errorf("auth: no authorization code in pasted text")
	}
	conf := msConfig(f.clientID)
	conf.RedirectURL = msRedirectURI
	tok, err := conf.Exchange(ctx, code, oauth2.VerifierOption(f.verifier))
	if err != nil {
		return nil, fmt.Errorf("auth: code exchange: %w", err)
	}
	return tok, nil
}

// ExtractAuthCode accepts either a bare authorization code or the full
// redirect URL (query or fragment form) and returns the code.
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

// StartMSDeviceFlow requests a device code from Microsoft. The response carries
// the verification URL and user code to show the user, plus the polling handle
// for PollMSDeviceToken.
func StartMSDeviceFlow(ctx context.Context, clientID string) (*oauth2.DeviceAuthResponse, error) {
	da, err := msConfig(clientID).DeviceAuth(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth: device code request: %w", err)
	}
	return da, nil
}

// PollMSDeviceToken polls the token endpoint until the user approves the device
// code, it expires (~15 min), or ctx is cancelled. Blocking — run it from a
// goroutine/tea.Cmd.
func PollMSDeviceToken(ctx context.Context, clientID string, da *oauth2.DeviceAuthResponse) (*oauth2.Token, error) {
	tok, err := msConfig(clientID).DeviceAccessToken(ctx, da)
	if err != nil {
		return nil, fmt.Errorf("auth: device sign-in: %w", err)
	}
	return tok, nil
}

// RefreshMSToken exchanges a refresh token for a new access token. Microsoft
// rotates refresh tokens for consumer accounts, so the returned token usually
// carries a NEW refresh token that must replace the stored one.
func RefreshMSToken(ctx context.Context, clientID, refreshToken string) (*oauth2.Token, error) {
	src := msConfig(clientID).TokenSource(ctx, &oauth2.Token{RefreshToken: refreshToken})
	tok, err := src.Token()
	if err != nil {
		return nil, fmt.Errorf("auth: refresh token: %w", err)
	}
	return tok, nil
}

// PersistRefreshToken is called with the rotated refresh token after every
// successful refresh so it survives crashes. Wired to the keyring at startup
// (kept as an injected func to avoid an auth→config import).
var PersistRefreshToken = func(accountName, refreshToken string) {}

type msTokenState struct {
	access  string
	expiry  time.Time
	refresh string
}

var (
	msTokensMu sync.Mutex
	msTokens   = map[string]*msTokenState{}
)

// expiryMargin refreshes tokens slightly early so a token that's valid when
// fetched doesn't expire mid-IMAP-session-setup.
const expiryMargin = 2 * time.Minute

// MSAccessToken returns a valid access token for the account, refreshing at
// most once per expiry window. The cache is shared by IMAP and SMTP so a send
// right after a sync reuses the token. cfgRefreshToken seeds the cache on first
// use; after a refresh the cache's rotated token wins (Microsoft invalidates
// the old one).
func MSAccessToken(ctx context.Context, clientID, accountName, cfgRefreshToken string) (string, error) {
	msTokensMu.Lock()
	defer msTokensMu.Unlock()

	st := msTokens[accountName]
	if st == nil {
		st = &msTokenState{refresh: cfgRefreshToken}
		msTokens[accountName] = st
	}
	if st.access != "" && time.Now().Before(st.expiry.Add(-expiryMargin)) {
		return st.access, nil
	}
	if st.refresh == "" {
		return "", fmt.Errorf("auth: no refresh token for %s", accountName)
	}
	tok, err := RefreshMSToken(ctx, clientID, st.refresh)
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
	msTokensMu.Lock()
	defer msTokensMu.Unlock()
	st := msTokens[accountName]
	if st == nil || st.refresh == "" {
		return "", false
	}
	return st.refresh, true
}

// ForgetMSToken drops the account's cached tokens — used after re-auth so the
// next connection seeds from the freshly issued refresh token.
func ForgetMSToken(accountName string) {
	msTokensMu.Lock()
	defer msTokensMu.Unlock()
	delete(msTokens, accountName)
}
