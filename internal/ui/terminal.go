package ui

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
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
//
// With transparent off, the default background is set one step away from the
// theme's: terminals such as kitty apply their background opacity to every cell
// whose color equals the default background, and every TideMail cell is painted
// in the theme background, so an exact match would make the whole UI see-through.
func TerminalColorSequences(themeName string, transparent bool) (set string, reset string) {
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
	if bg := terminalDefaultBg(theme.Bg, transparent); bg != "" {
		set += fmt.Sprintf("\x1b]11;%s\x07", string(bg))
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

// terminalDefaultBg is the color TideMail sets as the terminal's default
// background. Transparent uses the theme background itself, so the terminal's
// opacity applies to it; otherwise it is nudged by one step in one channel,
// invisible to the eye but enough that no painted cell matches it.
func terminalDefaultBg(bg lipgloss.Color, transparent bool) lipgloss.Color {
	if bg == "" || transparent {
		return bg
	}
	hex := strings.TrimPrefix(string(bg), "#")
	if len(hex) != 6 {
		return bg
	}
	v, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return bg
	}
	if v&0xff == 0xff {
		v--
	} else {
		v++
	}
	return lipgloss.Color(fmt.Sprintf("#%06x", v))
}

// termColorsCmd sets the terminal's default colors for a theme, honoring the
// transparent-background setting.
func (m Model) termColorsCmd(fg, bg lipgloss.Color) tea.Cmd {
	return setTermColorsCmd(fg, terminalDefaultBg(bg, m.cfg.Display.Transparent))
}

// transparentBackground rewrites every cell painted in the theme background to
// the terminal's default background, which TideMail has set to the same color,
// so the terminal's own opacity shows through. Selected rows, overlays and
// other surfaces keep their colors and stay solid.
func transparentBackground(view string, bg lipgloss.Color) string {
	if bg == "" {
		return view
	}
	params := themeBgParams(bg)
	if params == "" {
		return view
	}
	r := strings.NewReplacer(
		"\x1b["+params+"m", "\x1b[49m",
		"\x1b["+params+";", "\x1b[49;",
		";"+params+"m", ";49m",
		";"+params+";", ";49;",
	)
	return r.Replace(view)
}

var themeBgParamsCache sync.Map // lipgloss.Color + profile → SGR parameters

// themeBgParams returns the SGR parameters ("48;2;r;g;b" or "48;5;n") the
// current color profile uses for bg.
func themeBgParams(bg lipgloss.Color) string {
	key := fmt.Sprintf("%s/%d", bg, lipgloss.ColorProfile())
	if v, ok := themeBgParamsCache.Load(key); ok {
		return v.(string)
	}
	out := lipgloss.NewStyle().Background(bg).Render("x")
	params := ""
	if start := strings.Index(out, "\x1b["); start >= 0 {
		if end := strings.IndexByte(out[start:], 'm'); end > 2 {
			params = out[start+2 : start+end]
		}
	}
	if !strings.HasPrefix(params, "48;") {
		params = ""
	}
	themeBgParamsCache.Store(key, params)
	return params
}
