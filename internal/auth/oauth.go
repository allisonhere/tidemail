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

// IsAuthFailure reports whether err looks like a credential failure that
// requires the user to re-authenticate — a rejected app password, or an
// expired/revoked OAuth token (Google or Microsoft). The status layer uses this
// to surface a "re-authenticate" hint instead of a raw error.
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
		"aadsts70008",      // Microsoft: refresh token expired
		"aadsts700082",     // Microsoft: refresh token expired (inactivity)
		"aadsts50173",      // Microsoft: token revoked (password change etc.)
		"no refresh token", // account marked oauth2 but no token stored — needs sign-in
	} {
		if strings.Contains(m, sig) {
			return true
		}
	}
	return false
}

// PersistRefreshToken is called with the rotated refresh token after every
// successful refresh so it survives crashes. Wired to the keyring at startup
// (kept as an injected func to avoid an auth→config import).
var PersistRefreshToken = func(accountName, refreshToken string) {}

// expiryMargin refreshes tokens slightly early so a token that's valid when
// fetched doesn't expire mid-IMAP-session-setup.
const expiryMargin = 2 * time.Minute

type tokenState struct {
	access  string
	expiry  time.Time
	refresh string
}

// tokenCache is a per-account access-token cache for one OAuth provider, shared
// by IMAP and SMTP so a send right after a sync reuses the token. refresh mints
// a new token from a refresh token; providers that are public clients ignore the
// clientSecret argument.
type tokenCache struct {
	mu      sync.Mutex
	states  map[string]*tokenState
	refresh func(ctx context.Context, clientID, clientSecret, refreshToken string) (*oauth2.Token, error)
}

var allCaches []*tokenCache

func newTokenCache(refresh func(ctx context.Context, clientID, clientSecret, refreshToken string) (*oauth2.Token, error)) *tokenCache {
	c := &tokenCache{states: map[string]*tokenState{}, refresh: refresh}
	allCaches = append(allCaches, c)
	return c
}

// accessToken returns a valid access token for the account, refreshing at most
// once per expiry window. seedRefresh seeds the cache on first use; after a
// refresh the cache's rotated token wins.
func (c *tokenCache) accessToken(ctx context.Context, clientID, clientSecret, account, seedRefresh string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	st := c.states[account]
	if st == nil {
		st = &tokenState{refresh: seedRefresh}
		c.states[account] = st
	}
	if st.access != "" && time.Now().Before(st.expiry.Add(-expiryMargin)) {
		return st.access, nil
	}
	if st.refresh == "" {
		return "", fmt.Errorf("auth: no refresh token for %s", account)
	}
	tok, err := c.refresh(ctx, clientID, clientSecret, st.refresh)
	if err != nil {
		return "", err
	}
	st.access = tok.AccessToken
	st.expiry = tok.Expiry
	if tok.RefreshToken != "" && tok.RefreshToken != st.refresh {
		st.refresh = tok.RefreshToken
		PersistRefreshToken(account, tok.RefreshToken)
	}
	return st.access, nil
}

func (c *tokenCache) latest(account string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.states[account]
	if st == nil || st.refresh == "" {
		return "", false
	}
	return st.refresh, true
}

func (c *tokenCache) forget(account string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.states, account)
}

// LatestRefreshToken reports an account's current (possibly rotated) refresh
// token from whichever provider cache holds it. Used by saveConfig to avoid
// writing a stale token over a rotated one.
func LatestRefreshToken(account string) (string, bool) {
	for _, c := range allCaches {
		if tok, ok := c.latest(account); ok {
			return tok, true
		}
	}
	return "", false
}

// ExtractAuthCode accepts either a bare authorization code or the full redirect
// URL (query or fragment form) and returns the code. Shared by the Google and
// Microsoft paste-back flows.
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
