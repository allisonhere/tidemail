package ui

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/allisonhere/tidemail/internal/auth"
	tea "github.com/charmbracelet/bubbletea"
)

type googleBrowserReadyMsg struct {
	attempt    context.Context
	flow       *auth.GoogleBrowserFlow
	err        error
	browserErr error
}

type googleSignInExpiredMsg struct{ attempt context.Context }

// Seams let tests run without opening browsers or contacting Google.
var startGoogleBrowser = auth.StartGoogleBrowserFlow
var openSignInBrowser = launchSignInBrowser

func launchSignInBrowser(ctx context.Context, browser, url string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if browser == "" {
		browser = "xdg-open"
		if runtime.GOOS == "darwin" {
			browser = "open"
		}
	}
	// Settings name an executable. Never pass an authorization URL to a shell.
	cmd := exec.Command(browser, url)
	if err := cmd.Start(); err != nil {
		return err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	// Catch launcher failures (e.g. no desktop handler) without waiting for a
	// browser that stays open until the user closes its window.
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case err := <-exited:
		return err
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (am AccountManager) startGoogleBrowserSignIn() (AccountManager, tea.Cmd, bool) {
	am.cancelOAuth()
	ctx, cancel := context.WithTimeout(context.Background(), auth.GoogleSignInTimeout)
	am.oauthCtx, am.oauthCancel = ctx, cancel
	am.oauthActive, am.busy = true, true
	am.busyMsg = "OPENING GOOGLE SIGN-IN... (ESC CANCELS)"
	am.statusMsg = ""
	id, secret, browser := am.oauthCfg.GoogleClientID, am.oauthCfg.GoogleClientSecret, am.oauthBrowser
	return am, func() tea.Msg {
		flow, err := startGoogleBrowser(ctx, id, secret)
		msg := googleBrowserReadyMsg{attempt: ctx, flow: flow, err: err}
		if err == nil {
			msg.browserErr = openSignInBrowser(ctx, browser, flow.AuthURL)
		}
		return msg
	}, false
}

func (am AccountManager) googleBrowserReady(msg googleBrowserReadyMsg) (AccountManager, tea.Cmd, bool) {
	if !am.oauthActive || msg.attempt != am.oauthCtx {
		if msg.flow != nil {
			msg.flow.Close()
		}
		return am, nil, false
	}
	if msg.err != nil {
		if err := am.oauthCtx.Err(); err != nil {
			am.cancelOAuth()
			am.statusMsg = googleSignInError(err)
			am.focusField(amFieldOAuthSignIn)
			return am, nil, false
		}
		am, cmd, _ := am.startAuthCodeFlow()
		am.statusMsg = "AUTOMATIC SIGN-IN UNAVAILABLE — USE THE MANUAL STEPS BELOW"
		return am, cmd, false
	}
	am.oauthBrowserFlow = msg.flow
	am.oauthFlowURL = msg.flow.AuthURL
	am.busyMsg = "APPROVE IN BROWSER (CTRL+P: MANUAL ENTRY · ESC: CANCEL)"
	if msg.browserErr != nil {
		am.enableGoogleManualEntry()
		am.statusMsg = "COULD NOT OPEN BROWSER — OPEN THE SIGN-IN URL BELOW"
	}
	return am, func() tea.Msg {
		tok, err := msg.flow.Wait()
		result := OAuth2DoneMsg{attempt: msg.attempt, Err: err}
		if tok != nil {
			result.RefreshToken = tok.RefreshToken
		}
		return result
	}, false
}

func (am *AccountManager) enableGoogleManualEntry() {
	am.busy, am.oauthAwaitingCode = false, true
	am.busyMsg = ""
	am.oauthURLCopied = clipboardCopy(am.oauthFlowURL) == nil
	am.focusField(amFieldOAuthCode)
	am.statusMsg = "MANUAL SIGN-IN — OPEN THE URL AND PASTE THE RESULT BELOW"
}

func googleSignInError(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "SIGN-IN TIMED OUT — PRESS CTRL+O TO TRY AGAIN"
	case errors.Is(err, context.Canceled):
		return "SIGN-IN CANCELLED — PRESS CTRL+O TO TRY AGAIN"
	case strings.Contains(err.Error(), "denied"):
		return "GOOGLE ACCESS WAS DENIED — PRESS CTRL+O TO TRY AGAIN"
	default:
		// Provider responses may include codes/tokens. Keep them out of the UI.
		return "GOOGLE SIGN-IN FAILED — CHECK YOUR CONNECTION AND PRESS CTRL+O TO RETRY"
	}
}
