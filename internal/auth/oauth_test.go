package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestRotatedTokenPersistenceRetriesBeforeReuse(t *testing.T) {
	old := PersistRefreshToken
	t.Cleanup(func() { PersistRefreshToken = old })
	writes, refreshes := 0, 0
	PersistRefreshToken = func(account, token string) error {
		writes++
		if account != "account" || token != "rotated" {
			t.Fatal("wrong credentials persisted")
		}
		if writes == 1 {
			return errors.New("secret backend error")
		}
		return nil
	}
	c := &tokenCache{states: map[string]*tokenState{}, refresh: func(ctx context.Context, id, secret, token string) (*oauth2.Token, error) {
		refreshes++
		return &oauth2.Token{AccessToken: "access", RefreshToken: "rotated", Expiry: time.Now().Add(time.Hour)}, nil
	}}
	token, err := c.accessToken(context.Background(), "", "", "account", "seed")
	if err == nil || token != "" || strings.Contains(err.Error(), "secret backend") {
		t.Fatalf("unexpected persistence failure: %q, %v", token, err)
	}
	if latest, ok := c.latest("account"); !ok || latest != "rotated" {
		t.Fatal("rotated token lost")
	}
	token, err = c.accessToken(context.Background(), "", "", "account", "seed")
	if err != nil || token != "access" || writes != 2 || refreshes != 1 {
		t.Fatalf("retry failed: %q, %v, writes=%d refreshes=%d", token, err, writes, refreshes)
	}
	_, _ = c.accessToken(context.Background(), "", "", "account", "seed")
	if writes != 2 {
		t.Fatal("persisted token written again")
	}
}

// TestLatestRefreshTokenIsProviderScoped is the regression test for a stale
// token clobbering a rotated one. Caches are keyed by account name, which is not
// unique across providers, so switching an account between Gmail and Outlook
// leaves the old provider's entry behind — a re-auth only clears the cache for
// the provider the account is configured for. The old provider-agnostic lookup
// returned whichever cache was registered first, and saveConfig then wrote that
// stale token over the one just issued.
func TestLatestRefreshTokenIsProviderScoped(t *testing.T) {
	const account = "Personal"
	t.Cleanup(func() { ForgetToken(account) })

	googleCache.states[account] = &tokenState{refresh: "stale-google-token"}
	msCache.states[account] = &tokenState{refresh: "fresh-microsoft-token"}

	if tok, ok := LatestRefreshToken(ProviderMicrosoft, account); !ok || tok != "fresh-microsoft-token" {
		t.Fatalf("Microsoft lookup = %q,%v; want the Microsoft token", tok, ok)
	}
	if tok, ok := LatestRefreshToken(ProviderGoogle, account); !ok || tok != "stale-google-token" {
		t.Fatalf("Google lookup = %q,%v; want the Google token", tok, ok)
	}
}

// TestForgetTokenClearsEveryProvider verifies a re-auth cannot leave an entry
// behind in the provider the account used to be configured for.
func TestForgetTokenClearsEveryProvider(t *testing.T) {
	const account = "Personal"
	t.Cleanup(func() { ForgetToken(account) })

	googleCache.states[account] = &tokenState{refresh: "google-token"}
	msCache.states[account] = &tokenState{refresh: "microsoft-token"}

	ForgetToken(account)

	if _, ok := LatestRefreshToken(ProviderGoogle, account); ok {
		t.Fatal("expected the Google entry cleared")
	}
	if _, ok := LatestRefreshToken(ProviderMicrosoft, account); ok {
		t.Fatal("expected the Microsoft entry cleared")
	}
}

// TestLatestRefreshTokenUnknownProvider verifies a provider with no cache (a
// password account) reports nothing rather than falling back to another cache.
func TestLatestRefreshTokenUnknownProvider(t *testing.T) {
	const account = "Personal"
	t.Cleanup(func() { ForgetToken(account) })

	googleCache.states[account] = &tokenState{refresh: "google-token"}

	if tok, ok := LatestRefreshToken(Provider("not-a-provider"), account); ok {
		t.Fatalf("expected no token for an unknown provider, got %q", tok)
	}
}
