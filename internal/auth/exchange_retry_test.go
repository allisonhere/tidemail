package auth

import (
	"context"
	"errors"
	"net"
	"net/url"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func fastRetry(t *testing.T) {
	old := exchangeRetryDelay
	exchangeRetryDelay = time.Millisecond
	t.Cleanup(func() { exchangeRetryDelay = old })
}

func noNetwork() error {
	return &url.Error{Op: "Post", URL: "https://oauth2.example/token", Err: &net.DNSError{Err: "no such host", Name: "oauth2.example", IsNotFound: true}}
}

func TestRetryExchangeSucceedsAfterNetworkFailures(t *testing.T) {
	fastRetry(t)
	calls := 0
	tok, err := retryExchange(context.Background(), func(context.Context) (*oauth2.Token, error) {
		calls++
		if calls < 4 {
			return nil, noNetwork()
		}
		return &oauth2.Token{AccessToken: "ok"}, nil
	})
	if err != nil || tok.AccessToken != "ok" || calls != 4 {
		t.Fatalf("tok=%v err=%v calls=%d", tok, err, calls)
	}
}

func TestRetryExchangeDoesNotRepeatAProviderAnswer(t *testing.T) {
	fastRetry(t)
	calls := 0
	_, err := retryExchange(context.Background(), func(context.Context) (*oauth2.Token, error) {
		calls++
		return nil, &oauth2.RetrieveError{ErrorCode: "invalid_grant"}
	})
	var re *oauth2.RetrieveError
	if !errors.As(err, &re) || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestRetryExchangeStopsWhenTheWindowEnds(t *testing.T) {
	fastRetry(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := retryExchange(ctx, func(context.Context) (*oauth2.Token, error) { return nil, noNetwork() })
	if err == nil {
		t.Fatal("expected the last network error")
	}
}
