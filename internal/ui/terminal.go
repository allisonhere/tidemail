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

// TerminalBackgroundSequences returns ANSI OSC sequences that set and later
// reset the terminal's default background color for xterm-compatible terminals.
func TerminalBackgroundSequences(themeName string) (set string, reset string) {
	theme, _ := ThemeByName(themeName)
	if theme.Bg == "" {
		return "", ""
	}
	return fmt.Sprintf("\x1b]11;%s\x07", string(theme.Bg)), "\x1b]111\x07"
}

// setTermBgCmd returns a Cmd that sets the terminal background color via OSC 11.
// Writes to /dev/tty rather than os.Stdout because BubbleTea owns the stdout
// renderer; interleaved writes corrupt the display.
func setTermBgCmd(bg lipgloss.Color) tea.Cmd {
	return func() tea.Msg {
		if tty, err := openTTY(); err == nil {
			fmt.Fprintf(tty, "\x1b]11;%s\x07", string(bg))
			tty.Close()
		}
		return nil
	}
}
