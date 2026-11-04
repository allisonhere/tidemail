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

func TestRefreshIsolationAndForget(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	c := &tokenCache{states: map[string]*tokenState{}, refresh: func(ctx context.Context, _, _, token string) (*oauth2.Token, error) {
		if token == "slow" {
			close(entered)
			<-release
		}
		return &oauth2.Token{AccessToken: token, Expiry: time.Now().Add(time.Hour)}, nil
	}}
	done := make(chan error, 1)
	go func() { _, err := c.accessToken(context.Background(), "", "", "a", "slow"); done <- err }()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	token, err := c.accessToken(ctx, "", "", "b", "fast")
	if err != nil || token != "fast" {
		t.Fatalf("other account blocked: %q %v", token, err)
	}
	canceled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := c.accessToken(canceled, "", "", "a", "slow"); err == nil {
		t.Fatal("same-account waiter ignored cancellation")
	}
	c.forget("a")
	close(release)
	if err := <-done; err == nil {
		t.Fatal("forgotten refresh was accepted")
	}
	if _, ok := c.latest("a"); ok {
		t.Fatal("forgotten credentials repopulated")
	}
}
