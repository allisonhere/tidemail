package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	// GmailScope is the OAuth2 scope for IMAP/SMTP access.
	GmailScope = "https://mail.google.com/"
	// DefaultPort is the default port for the local OAuth2 callback server.
	DefaultPort = 8085
	// callbackTimeout is how long to wait for the user to complete OAuth in the browser.
	callbackTimeout = 120 * time.Second
)

// StartGmailOAuthFlow starts a local HTTP server, opens the browser for the user to
// authorize, waits for the callback, exchanges the code for tokens, and returns them.
// The port parameter specifies the local port to listen on (0 for random).
func StartGmailOAuthFlow(clientID, clientSecret string, port int) (*oauth2.Token, error) {
	conf := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Scopes:       []string{GmailScope},
		Endpoint:     google.Endpoint,
	}

	// Pick a listener
	if port == 0 {
		port = DefaultPort
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		// Try random port if specified one is busy
		listener, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, fmt.Errorf("auth: find local port: %w", err)
		}
	}
	actualPort := listener.Addr().(*net.TCPAddr).Port
	redirectURL := fmt.Sprintf("http://127.0.0.1:%d/callback", actualPort)
	conf.RedirectURL = redirectURL

	// Generate a random state value to prevent CSRF
	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		return nil, fmt.Errorf("auth: generate state: %w", err)
	}
	state := hex.EncodeToString(stateBytes)

	// Channel to receive the token result
	tokenCh := make(chan *oauth2.Token)
	errCh := make(chan error)

	// Start the HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			errCh <- fmt.Errorf("auth: state mismatch")
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "no code", http.StatusBadRequest)
			errCh <- fmt.Errorf("auth: no authorization code")
			return
		}
		tok, err := conf.Exchange(context.Background(), code)
		if err != nil {
			http.Error(w, "token exchange failed", http.StatusInternalServerError)
			errCh <- fmt.Errorf("auth: token exchange: %w", err)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><h3>Authenticated!</h3><p>You can close this tab.</p></body></html>`)
		tokenCh <- tok
	})

	server := &http.Server{Handler: mux}
	go server.Serve(listener) //nolint:errcheck
	defer server.Close()      //nolint:errcheck

	// Build the auth URL and open the browser.
	// prompt=consent ensures Google always shows a fresh consent screen even on
	// repeat sign-ins, avoiding the "already has access" dead-end page.
	authURL := conf.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"),
	)
	openURL(authURL)

	// Wait for the callback or timeout
	select {
	case tok := <-tokenCh:
		return tok, nil
	case err := <-errCh:
		return nil, err
	case <-time.After(callbackTimeout):
		return nil, fmt.Errorf("auth: timeout waiting for browser authorization (started auth at %s)", authURL)
	}
}

// RefreshAccessToken uses the refresh token to get a new access token.
func RefreshAccessToken(clientID, clientSecret, refreshToken string) (*oauth2.Token, error) {
	conf := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Scopes:       []string{GmailScope},
		Endpoint:     google.Endpoint,
	}
	tok := &oauth2.Token{RefreshToken: refreshToken}
	src := conf.TokenSource(context.Background(), tok)
	newTok, err := src.Token()
	if err != nil {
		return nil, fmt.Errorf("auth: refresh token: %w", err)
	}
	return newTok, nil
}

// TokenJSON serializes an oauth2.Token to JSON.
func TokenJSON(tok *oauth2.Token) (string, error) {
	data, err := json.Marshal(tok)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// TokenFromJSON deserializes an oauth2.Token from JSON.
func TokenFromJSON(data string) (*oauth2.Token, error) {
	var tok oauth2.Token
	if err := json.Unmarshal([]byte(data), &tok); err != nil {
		return nil, err
	}
	return &tok, nil
}

// openURL opens the given URL in the default browser.
func openURL(url string) {
	// Use xdg-open on Linux.
	_ = exec.Command("xdg-open", url).Start()
}
