package auth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/oauth2"
)

// MicrosoftSignInTimeout bounds how long a browser sign-in may take.
const MicrosoftSignInTimeout = 5 * time.Minute

type microsoftBrowserResult struct {
	token *oauth2.Token
	err   error
}

// MicrosoftBrowserFlow is an authorization-code + PKCE sign-in that finishes by itself: it listens on a
// short-lived loopback port and the browser's redirect delivers the code. It is the Microsoft twin of
// GoogleBrowserFlow, for apps that have their own Microsoft app registration with the redirect
// "http://localhost" (Mobile and desktop platform). Microsoft ignores the port of a loopback redirect and
// wants the host spelled "localhost", not 127.0.0.1. The desktop's paste-the-code flow (MSAuthCodeFlow) is
// unchanged.
type MicrosoftBrowserFlow struct {
	// AuthURL is the sign-in page to open in a browser.
	AuthURL string

	clientID    string
	verifier    string
	state       string
	redirectURI string
	listenAddr  string
	ctx         context.Context
	cancel      context.CancelFunc
	server      *http.Server
	submitted   atomic.Bool
	codes       chan string
	results     chan microsoftBrowserResult
	received    chan struct{}
	done        chan struct{}
	recvOnce    sync.Once
}

// StartMicrosoftBrowserFlow begins a sign-in for the given Microsoft app (client) ID and listens on 127.0.0.1
// until it finishes, is closed, or times out.
func StartMicrosoftBrowserFlow(ctx context.Context, clientID string) (*MicrosoftBrowserFlow, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("auth: cannot listen for browser sign-in: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirect := fmt.Sprintf("http://localhost:%d", port)

	conf := msConfig(clientID)
	conf.RedirectURL = redirect
	verifier := oauth2.GenerateVerifier()
	state := oauth2.GenerateVerifier()
	// select_account: always show the account picker, so a browser already signed in to the wrong Microsoft
	// account does not silently authorize it.
	authURL := conf.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oauth2.SetAuthURLParam("prompt", "select_account"))

	ctx, cancel := context.WithTimeout(ctx, MicrosoftSignInTimeout)
	f := &MicrosoftBrowserFlow{
		AuthURL: authURL, clientID: clientID, verifier: verifier, state: state,
		redirectURI: redirect, listenAddr: listener.Addr().String(),
		ctx: ctx, cancel: cancel,
		codes: make(chan string, 1), results: make(chan microsoftBrowserResult, 1),
		received: make(chan struct{}), done: make(chan struct{}),
	}
	f.server = &http.Server{Handler: http.HandlerFunc(f.callback), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := f.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			cancel()
		}
	}()
	go func() {
		defer cancel()
		var result microsoftBrowserResult
		select {
		case code := <-f.codes:
			if code == "" {
				result.err = errors.New("auth: Microsoft access was denied")
			} else {
				exchangeCtx, stop := context.WithTimeout(ctx, 45*time.Second)
				result.token, result.err = retryExchange(exchangeCtx, func(c context.Context) (*oauth2.Token, error) { return f.exchange(c, code) })
				stop()
			}
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
		close(f.done)
	}()
	return f, nil
}

func (f *MicrosoftBrowserFlow) exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	conf := msConfig(f.clientID)
	conf.RedirectURL = f.redirectURI
	tok, err := conf.Exchange(ctx, code, oauth2.VerifierOption(f.verifier))
	if err != nil {
		return nil, fmt.Errorf("auth: code exchange: %w", err)
	}
	return tok, nil
}

func (f *MicrosoftBrowserFlow) callback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if r.Method != http.MethodGet || (r.URL.Path != "/" && r.URL.Path != "") {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	if q.Get("state") != f.state {
		http.Error(w, "Sign-in state does not match. Return to TideMail and use the current sign-in link.", http.StatusBadRequest)
		return
	}
	if denied := q.Get("error"); denied != "" {
		if !f.submitted.CompareAndSwap(false, true) {
			http.Error(w, "This sign-in has already been received.", http.StatusConflict)
			return
		}
		// Route the refusal through the worker so cancellation and cleanup keep one owner; an empty code means
		// "denied" and fails the attempt without any request.
		f.codes <- ""
		_, _ = fmt.Fprintln(w, "Microsoft access was denied. Return to TideMail and try signing in again.")
		return
	}
	code := q.Get("code")
	if code == "" {
		http.Error(w, "No authorization code in this redirect.", http.StatusBadRequest)
		return
	}
	if !f.submitted.CompareAndSwap(false, true) {
		http.Error(w, "This sign-in has already been received.", http.StatusConflict)
		return
	}
	f.codes <- code
	f.recvOnce.Do(func() { close(f.received) })
	_, _ = fmt.Fprintln(w, "Authorization received. Return to TideMail to finish adding your account. You can close this tab.")
}

// Received is closed once the browser has delivered a valid sign-in (the exchange for tokens may still be running).
func (f *MicrosoftBrowserFlow) Received() <-chan struct{} { return f.received }

// Done is closed when the attempt has ended, with tokens or with an error.
func (f *MicrosoftBrowserFlow) Done() <-chan struct{} { return f.done }

// Wait blocks until the sign-in finishes and returns its tokens. Call it once.
func (f *MicrosoftBrowserFlow) Wait() (*oauth2.Token, error) {
	result := <-f.results
	return result.token, result.err
}

// Close abandons the sign-in and closes its listener.
func (f *MicrosoftBrowserFlow) Close() {
	f.cancel()
	_ = f.server.Close()
}

// redirectForTest reports the redirect the flow asked Microsoft to use.
func (f *MicrosoftBrowserFlow) redirectForTest() (*url.URL, error) { return url.Parse(f.redirectURI) }
