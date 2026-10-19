// Package passprompt asks for a secret in a native graphical dialog, so a
// password can be typed outside the terminal (and outside a screen recording or
// shoulder-surfed TUI). It is optional: Available reports false over SSH, with
// no display, or with no supported dialog program installed, and callers keep
// their own terminal input in that case.
//
// The dialog program is run directly (no shell). The secret comes back on its
// standard output, so it never appears in a command line, an environment
// variable, or a log. Errors never include the program's output.
package passprompt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

var (
	// ErrCanceled means the user dismissed the dialog or left it empty.
	ErrCanceled = errors.New("password entry canceled")
	// ErrUnavailable means no graphical dialog can be shown here.
	ErrUnavailable = errors.New("no graphical password dialog available")
)

// timeout bounds how long a forgotten dialog can stay open.
const timeout = 5 * time.Minute

// provider builds the command line of one dialog program. The title and message
// are the only text passed on the command line, and neither is secret.
type provider struct {
	name string
	args func(title, message string) []string
}

// providers are tried in order. Both print the entered text to standard output
// and exit 1 when canceled.
var providers = []provider{
	{"zenity", func(title, message string) []string {
		return []string{"--password", "--title=" + title, "--text=" + message}
	}},
	{"kdialog", func(title, message string) []string {
		return []string{"--title", title, "--password", message}
	}},
}

// Available reports whether Ask can show a dialog: a graphical session that is
// not remote, and an installed dialog program.
func Available() bool {
	_, ok := find()
	return ok
}

func find() (string, bool) {
	if os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != "" || os.Getenv("SSH_CLIENT") != "" {
		return "", false // a dialog would open on the remote machine's screen
	}
	if os.Getenv("WAYLAND_DISPLAY") == "" && os.Getenv("DISPLAY") == "" {
		return "", false
	}
	for _, p := range providers {
		if _, err := exec.LookPath(p.name); err == nil {
			return p.name, true
		}
	}
	return "", false
}

// Ask shows a password dialog and returns what was entered. It returns
// ErrUnavailable when no dialog can be shown and ErrCanceled when the user
// cancels or submits nothing.
func Ask(title, message string) (string, error) {
	name, ok := find()
	if !ok {
		return "", ErrUnavailable
	}
	var args []string
	for _, p := range providers {
		if p.name == name {
			args = p.args(title, message)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	defer wipe(out.Bytes())
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		switch {
		case ctx.Err() != nil:
			return "", fmt.Errorf("%s: password dialog timed out", name)
		case errors.As(err, &exit) && exit.ExitCode() == 1:
			return "", ErrCanceled
		default:
			// Do not include out: it may hold what was typed.
			return "", fmt.Errorf("%s: could not show the password dialog", name)
		}
	}
	secret := strings.TrimRight(out.String(), "\r\n")
	if secret == "" {
		return "", ErrCanceled
	}
	return secret, nil
}

func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
