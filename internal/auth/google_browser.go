package auth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"golang.org/x/oauth2"
)

// GoogleSignInTimeout bounds browser approval, including manual entry.
const GoogleSignInTimeout = 5 * time.Minute

type googleBrowserResult struct {
	token *oauth2.Token
	err   error
}

// GoogleBrowserFlow accepts one callback or manually pasted code. Both routes
// share the PKCE verifier, redirect URI, and one token exchange.
type GoogleBrowserFlow struct {
	*GoogleAuthCodeFlow
	ctx       context.Context
	cancel    context.CancelFunc
	server    *http.Server
	submitted atomic.Bool
	codes     chan string
	results   chan googleBrowserResult
}

func StartGoogleBrowserFlow(ctx context.Context, clientID, clientSecret string) (*GoogleBrowserFlow, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("auth: cannot listen for browser sign-in: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, GoogleSignInTimeout)
	f := &GoogleBrowserFlow{
		GoogleAuthCodeFlow: newGoogleAuthCodeFlow(clientID, clientSecret, "http://"+listener.Addr().String()+"/oauth/callback"),
		ctx:                ctx, cancel: cancel,
		codes: make(chan string, 1), results: make(chan googleBrowserResult, 1),
	}
	f.server = &http.Server{Handler: http.HandlerFunc(f.callback), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := f.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			cancel()
		}
	}()
	go func() {
		defer cancel()
		var result googleBrowserResult
		select {
		case code := <-f.codes:
			exchangeCtx, stop := context.WithTimeout(ctx, 30*time.Second)
			result.token, result.err = f.GoogleAuthCodeFlow.Exchange(exchangeCtx, code)
			stop()
		case <-ctx.Done():
			result.err = ctx.Err()
		}
		// Let the callback response finish before closing accepted connections.
		shutdownCtx, stop := context.WithTimeout(context.Background(), time.Second)
		if err := f.server.Shutdown(shutdownCtx); err != nil {
			_ = f.server.Close()
		}
		stop()
		f.results <- result
	}()
	return f, nil
}

func (f *GoogleBrowserFlow) callback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if r.Method != http.MethodGet || r.URL.Path != "/oauth/callback" {
		http.NotFound(w, r)
		return
	}
	if r.URL.Query().Get("state") != f.state {
		http.Error(w, "Sign-in state does not match. Return to TideMail and use the current sign-in link.", http.StatusBadRequest)
		return
	}
	if r.URL.Query().Get("error") != "" {
		if !f.submitted.CompareAndSwap(false, true) {
			http.Error(w, "This sign-in has already been received.", http.StatusConflict)
			return
		}
		// Route denial through the same worker so cancellation and cleanup still
		// have a single owner. Exchange validates the error before any request.
		f.codes <- f.redirectURI + "?" + r.URL.RawQuery
		_, _ = fmt.Fprintln(w, "Google access was denied. Return to TideMail and try signing in again.")
		return
	}
	if err := f.Submit(f.redirectURI + "?" + r.URL.RawQuery); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_, _ = fmt.Fprintln(w, "Authorization received. Return to TideMail to finish adding your account. You can close this tab.")
}

// Submit lets the advanced manual flow reuse an in-progress browser attempt.
// Invalid input does not consume the attempt; valid input is accepted only once.
func (f *GoogleBrowserFlow) Submit(pasted string) error {
	if err := f.ctx.Err(); err != nil {
		return err
	}
	code, err := f.authorizationCode(pasted)
	if err != nil {
		return err
	}
	if !f.submitted.CompareAndSwap(false, true) {
		return fmt.Errorf("auth: this sign-in has already been received")
	}
	f.codes <- code
	return nil
}

// Wait is called once by the UI's background command.
func (f *GoogleBrowserFlow) Wait() (*oauth2.Token, error) {
	result := <-f.results
	return result.token, result.err
}

func (f *GoogleBrowserFlow) Close() {
	f.cancel()
	_ = f.server.Close()
}
