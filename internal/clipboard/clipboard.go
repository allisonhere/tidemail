// Package clipboard provides system-clipboard access shared by the TUI and the
// editor harness. Copy tries local clipboard tools and falls back to an OSC 52
// terminal escape (which works over SSH on capable terminals); Read uses local
// tools only — OSC 52 reads are unreliable and usually disabled, so paste-in
// over remote sessions should rely on the terminal's own bracketed paste.
package clipboard

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/aymanbagabas/go-osc52/v2"
)

// Seams for tests: override to force the no-tool fallback path and to capture
// the emitted OSC 52 sequence without touching the real terminal.
var (
	lookPath              = exec.LookPath
	osc52Writer io.Writer = os.Stderr
)

// Copy writes text to the system clipboard. It tries wl-copy, xclip, xsel, and
// pbcopy in turn; if none are available it emits an OSC 52 sequence so capable
// terminals (including over SSH) still receive the copy. It returns an error
// only when every mechanism fails.
func Copy(text string) error {
	candidates := [][]string{
		{"wl-copy"},
		{"xclip", "-selection", "clipboard"},
		{"xsel", "--clipboard", "--input"},
		{"pbcopy"},
	}
	for _, args := range candidates {
		path, err := lookPath(args[0])
		if err != nil {
			continue
		}
		cmd := exec.Command(path, args[1:]...)
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}
	if _, err := osc52.New(text).WriteTo(osc52Writer); err != nil {
		return fmt.Errorf("no clipboard tool found and OSC 52 fallback failed: %w", err)
	}
	return nil
}

// Read returns the system clipboard contents via wl-paste, xclip, xsel, or
// pbpaste. There is no OSC 52 fallback for reads.
func Read() (string, error) {
	candidates := [][]string{
		{"wl-paste", "--no-newline"},
		{"xclip", "-selection", "clipboard", "-out"},
		{"xsel", "--clipboard", "--output"},
		{"pbpaste"},
	}
	for _, args := range candidates {
		path, err := lookPath(args[0])
		if err != nil {
			continue
		}
		out, err := exec.Command(path, args[1:]...).Output()
		if err == nil {
			return string(out), nil
		}
	}
	return "", fmt.Errorf("no clipboard tool found (wl-paste/xclip/xsel/pbpaste)")
}
