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
