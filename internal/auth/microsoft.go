package auth

import (
	"context"
	"fmt"

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
	// Public client: no secret. The device-code flow has no PKCE.
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
	authURL := conf.AuthCodeURL("tidemail", oauth2.S256ChallengeOption(v),
		oauth2.SetAuthURLParam("prompt", "select_account"))
	return &MSAuthCodeFlow{AuthURL: authURL, clientID: clientID, verifier: v}
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

// StartMSDeviceFlow requests a device code from Microsoft. The response carries
// the verification URL and user code to show the user, plus the polling handle
// for PollMSDeviceToken. (Only custom app registrations may use this — the
// bundled Thunderbird client forbids it and must use MSAuthCodeFlow.)
func StartMSDeviceFlow(ctx context.Context, clientID string) (*oauth2.DeviceAuthResponse, error) {
	da, err := msConfig(clientID).DeviceAuth(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth: device code request: %w", err)
	}
	return da, nil
}

// PollMSDeviceToken polls the token endpoint until the user approves the device
// code, it expires (~15 min), or ctx is cancelled. Blocking — run it from a
// goroutine / tea.Cmd.
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

// msCache is the shared IMAP/SMTP access-token cache for Outlook accounts.
// Microsoft is a public client, so the clientSecret argument is unused.
var msCache = newTokenCache(func(ctx context.Context, clientID, _, refreshToken string) (*oauth2.Token, error) {
	return RefreshMSToken(ctx, clientID, refreshToken)
})

// MSAccessToken returns a valid access token for the account, refreshing at most
// once per expiry window. cfgRefreshToken seeds the cache on first use; after a
// refresh the cache's rotated token wins (Microsoft invalidates the old one).
func MSAccessToken(ctx context.Context, clientID, accountName, cfgRefreshToken string) (string, error) {
	return msCache.accessToken(ctx, clientID, "", accountName, cfgRefreshToken)
}

// ForgetMSToken drops the account's cached tokens — used after re-auth so the
// next connection seeds from the freshly issued refresh token.
func ForgetMSToken(accountName string) { msCache.forget(accountName) }
