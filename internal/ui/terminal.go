package ui

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

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
	theme, _ := ThemeByName(themeName)
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
