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
var PersistRefreshToken = func(accountName, refreshToken string) error { return nil }

// expiryMargin refreshes tokens slightly early so a token that's valid when
// fetched doesn't expire mid-IMAP-session-setup.
const expiryMargin = 2 * time.Minute

type tokenState struct {
	access         string
	expiry         time.Time
	refresh        string
	pendingPersist bool
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

// Provider identifies which OAuth provider a cached token belongs to. Caches are
// keyed by account name, which is not unique across providers, so every lookup
// has to say which provider it means.
type Provider string

const (
	ProviderGoogle    Provider = "google"
	ProviderMicrosoft Provider = "microsoft"
)

var allCaches = map[Provider]*tokenCache{}

func newTokenCache(p Provider, refresh func(ctx context.Context, clientID, clientSecret, refreshToken string) (*oauth2.Token, error)) *tokenCache {
	c := &tokenCache{states: map[string]*tokenState{}, refresh: refresh}
	allCaches[p] = c
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
	// Retry a failed write before using or refreshing this token again.
	if st.pendingPersist {
		if err := c.persist(account, st); err != nil {
			return "", err
		}
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
		st.pendingPersist = true
		if err := c.persist(account, st); err != nil {
			return "", err
		}
	}
	return st.access, nil
}

func (c *tokenCache) persist(account string, st *tokenState) error {
	if err := PersistRefreshToken(account, st.refresh); err != nil {
		// Do not include storage errors: a backend could echo secret material.
		return fmt.Errorf("auth: could not save refreshed credentials for %s; restore credential storage and retry", account)
	}
	st.pendingPersist = false
	return nil
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
// token from the given provider's cache. Used by saveConfig to avoid writing a
// stale token over a rotated one.
//
// The provider must be specified. Searching every cache for the account name
// meant that switching an account between Gmail and Outlook — which leaves the
// old provider's entry behind, since a re-auth only clears the new provider's
// cache — returned the other provider's stale token, which saveConfig then wrote
// over the token that had just been issued. Whichever cache was registered first
// won, so the result also depended on package initialisation order. -allie
func LatestRefreshToken(p Provider, account string) (string, bool) {
	c := allCaches[p]
	if c == nil {
		return "", false
	}
	return c.latest(account)
}

// ForgetToken drops an account's cached tokens from every provider, so a stale
// entry cannot outlive a switch from one provider to another.
func ForgetToken(account string) {
	for _, c := range allCaches {
		c.forget(account)
	}
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
