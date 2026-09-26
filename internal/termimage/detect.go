package termimage

import (
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/allisonhere/tidemail/internal/richmail"
)

// DetectFromEnv detects capabilities from the live process environment and the
// controlling terminal. Tests should call Detect with an explicit environment
// and geometry instead.
func DetectFromEnv() Capabilities {
	caps := Detect(os.Getenv, DetectGeometry())
	// Never write graphics escapes into a redirected stdout: if the override is
	// not forcing a protocol and stdout is not a terminal, fall back to text.
	if os.Getenv("TIDEMAIL_IMAGE_PROTOCOL") == "" && !term.IsTerminal(int(os.Stdout.Fd())) {
		caps.Protocol = ProtocolNone
	}
	return caps
}

// Detect builds capabilities from an environment lookup and a cell geometry.
// Keeping the environment injectable is what makes terminal detection
// deterministic under test.
func Detect(getenv func(string) string, geom richmail.CellGeometry) Capabilities {
	return Capabilities{
		Protocol:  DetectProtocol(getenv),
		TrueColor: detectTrueColor(getenv),
		Cell:      geom,
	}
}

// DetectProtocol classifies the terminal's graphics support from environment
// variables. TIDEMAIL_IMAGE_PROTOCOL forces the choice:
//
//	kitty            force Kitty graphics
//	none|off|no|0    disable graphics (text placeholders only)
//
// An unrecognised override falls back to automatic detection rather than
// silently disabling a working terminal.
func DetectProtocol(getenv func(string) string) Protocol {
	override := strings.ToLower(strings.TrimSpace(getenv("TIDEMAIL_IMAGE_PROTOCOL")))
	switch override {
	case "kitty":
		return ProtocolKitty
	case "none", "off", "no", "0", "false":
		return ProtocolNone
	}

	if strings.EqualFold(strings.TrimSpace(getenv("TERM")), "dumb") {
		return ProtocolNone
	}

	// WezTerm's basic Kitty graphics support does not imply support for the
	// U=1 virtual placements and Unicode placeholders this backend requires.
	// Check before Kitty hints, which can be inherited by a nested terminal.
	// Patched builds can still opt in using TIDEMAIL_IMAGE_PROTOCOL=kitty.
	if strings.TrimSpace(getenv("WEZTERM_EXECUTABLE")) != "" ||
		strings.TrimSpace(getenv("WEZTERM_PANE")) != "" ||
		strings.EqualFold(strings.TrimSpace(getenv("TERM_PROGRAM")), "wezterm") ||
		strings.Contains(strings.ToLower(getenv("TERM")), "wezterm") {
		return ProtocolNone
	}
	if strings.TrimSpace(getenv("KITTY_WINDOW_ID")) != "" {
		return ProtocolKitty
	}

	term := strings.ToLower(getenv("TERM"))
	for _, marker := range []string{"xterm-kitty", "kitty", "ghostty", "foot"} {
		if strings.Contains(term, marker) {
			return ProtocolKitty
		}
	}

	program := strings.ToLower(getenv("TERM_PROGRAM"))
	switch program {
	case "ghostty", "kitty":
		return ProtocolKitty
	}

	return ProtocolNone
}

// detectTrueColor is a conservative read of the usual environment markers. The
// image protocol itself uses a 256-colour-safe encoding, so this only feeds
// cosmetic decisions.
func detectTrueColor(getenv func(string) string) bool {
	if strings.Contains(strings.ToLower(getenv("COLORTERM")), "truecolor") ||
		strings.Contains(strings.ToLower(getenv("COLORTERM")), "24bit") {
		return true
	}
	switch strings.ToLower(getenv("TERM_PROGRAM")) {
	case "ghostty", "wezterm", "kitty", "iTerm.app":
		return true
	}
	term := strings.ToLower(getenv("TERM"))
	for _, marker := range []string{"kitty", "ghostty", "wezterm", "direct", "truecolor", "24bit"} {
		if strings.Contains(term, marker) {
			return true
		}
	}
	return false
}
