package auth

import (
	"context"
	"errors"
	"net"
	"net/url"
	"time"

	"golang.org/x/oauth2"
)

// exchangeRetryDelay is the pause between tries of a code exchange that failed for lack of a network.
var exchangeRetryDelay = 1500 * time.Millisecond

// isNetworkError reports whether err is a connection problem (no DNS answer, no route, a timeout) as opposed to an answer from the
// provider such as invalid_grant. Only the first kind is worth trying again.
func isNetworkError(err error) bool {
	var answer *oauth2.RetrieveError
	if errors.As(err, &answer) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var urlErr *url.Error
	return errors.As(err, &urlErr)
}

// retryExchange runs exchange until it succeeds, the provider answers with an error, or ctx ends. A sign-in code is valid for
// minutes and is only used up by an answer, so when the phone briefly has no network (a mail app waiting in the background while
// the browser is in front can be cut off by the system) trying again once the user is back completes the sign-in.
func retryExchange(ctx context.Context, exchange func(context.Context) (*oauth2.Token, error)) (*oauth2.Token, error) {
	for {
		tok, err := exchange(ctx)
		if err == nil || !isNetworkError(err) || ctx.Err() != nil {
			return tok, err
		}
		select {
		case <-ctx.Done():
			return nil, err
		case <-time.After(exchangeRetryDelay):
		}
	}
}
