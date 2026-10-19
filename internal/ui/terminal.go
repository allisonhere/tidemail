package ui

import (
	"fmt"
	"io"
	"os"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// termMu orders every write that reaches the terminal. Image uploads go to
// /dev/tty while Bubble Tea's renderer flushes frames to stdout from its own
// goroutine; a frame landing inside a large upload carries an ESC that ends the
// image escape early, and the rest of the base64 prints as text. Holding one
// lock around each upload and each frame flush keeps them from interleaving.
var termMu sync.Mutex

// serializedFile is os.File with Write held under termMu. It embeds the file so
// it still has Fd, which Bubble Tea needs to detect a terminal and its size.
type serializedFile struct{ *os.File }

func (f serializedFile) Write(p []byte) (int, error) { return writeLocked(f.File, p) }

// writeLocked writes p to w while holding termMu.
func writeLocked(w io.Writer, p []byte) (int, error) {
	termMu.Lock()
	defer termMu.Unlock()
	return w.Write(p)
}

// SerializedStdout returns os.Stdout wrapped so its writes cannot interleave
// with image uploads. Pass it to tea.WithOutput.
func SerializedStdout() io.Writer { return serializedFile{os.Stdout} }

// openTTY opens /dev/tty for writing terminal control sequences safely,
// without going through BubbleTea's renderer-owned stdout.
func openTTY() (*os.File, error) {
	return os.OpenFile("/dev/tty", os.O_WRONLY, 0)
}

// TerminalColorSequences returns ANSI OSC sequences that set and later reset the
// terminal's default foreground (OSC 10) and background (OSC 11) colors for
// xterm-compatible terminals. Setting the default foreground keeps unstyled text
// — message bodies, and any text following an ANSI reset — in the theme color
// instead of the terminal's own default. This matters most for the monochrome
// phosphor themes (vt52/vt100), where that default would otherwise be white.
func TerminalColorSequences(themeName string) (set string, reset string) {
	var theme Theme
	if isMatchOmarchy(themeName) {
		theme = omarchyOrFallbackTheme()
	} else {
		theme, _ = ThemeByName(themeName)
	}
	if theme.Fg != "" {
		set += fmt.Sprintf("\x1b]10;%s\x07", string(theme.Fg))
		reset += "\x1b]110\x07"
	}
	if theme.Bg != "" {
		set += fmt.Sprintf("\x1b]11;%s\x07", string(theme.Bg))
		reset += "\x1b]111\x07"
	}
	return set, reset
}

// setTermColorsCmd returns a Cmd that sets the terminal default foreground (OSC 10)
// and background (OSC 11) colors. Writes to /dev/tty rather than os.Stdout because
// BubbleTea owns the stdout renderer; interleaved writes corrupt the display.
func setTermColorsCmd(fg, bg lipgloss.Color) tea.Cmd {
	return func() tea.Msg {
		if tty, err := openTTY(); err == nil {
			if fg != "" {
				_, _ = fmt.Fprintf(tty, "\x1b]10;%s\x07", string(fg))
			}
			if bg != "" {
				_, _ = fmt.Fprintf(tty, "\x1b]11;%s\x07", string(bg))
			}
			tty.Close()
		}
		return nil
	}
}
