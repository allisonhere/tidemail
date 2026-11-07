package termimage

import (
	"bytes"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/term"
)

// Terminals reached through a multiplexer (Herdr, for one) often advertise
// nothing in the environment even though they pass Kitty graphics through. The
// probe asks the terminal directly: a Kitty graphics query followed by a
// primary device attributes (DA1) request. Every terminal answers DA1, so the
// reply to it marks the end of the wait; a graphics reply that arrives first
// means Kitty graphics works.
const (
	probeQuery   = "\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\" + "\x1b[c"
	probeTimeout = 200 * time.Millisecond
)

// probedKitty records a successful Probe so DetectFromEnv, which runs more than
// once, never queries the terminal again.
var probedKitty atomic.Bool

// parseProbeReply reports whether buf contains a Kitty graphics reply and
// whether the DA1 reply, which ends the probe, has arrived.
func parseProbeReply(buf []byte) (graphics, done bool) {
	if i := bytes.Index(buf, []byte("\x1b_Gi=31")); i >= 0 {
		graphics = bytes.Contains(buf[i:], []byte(";OK"))
	}
	if i := bytes.Index(buf, []byte("\x1b[?")); i >= 0 {
		done = bytes.IndexByte(buf[i:], 'c') > 0
	}
	return graphics, done
}

// probeEligible reports whether the environment leaves room for a probe: the
// override is unset, detection found nothing, and the terminal is not one known
// to lack Unicode placeholders (foot, WezTerm answer the query but cannot
// render the placeholders this backend needs).
func probeEligible(getenv func(string) string) bool {
	if strings.TrimSpace(getenv("TIDEMAIL_IMAGE_PROTOCOL")) != "" {
		return false
	}
	if DetectProtocol(getenv) != ProtocolNone {
		return false
	}
	t := strings.ToLower(getenv("TERM"))
	p := strings.ToLower(getenv("TERM_PROGRAM"))
	if t == "dumb" || t == "" || t == "foot" || strings.HasPrefix(t, "foot-") || p == "foot" || p == "wezterm" ||
		strings.Contains(t, "wezterm") || getenv("WEZTERM_PANE") != "" || getenv("WEZTERM_EXECUTABLE") != "" {
		return false
	}
	return true
}

// Probe queries the controlling terminal for Kitty graphics support and
// remembers a positive answer for DetectFromEnv. Call it once at startup,
// before the UI takes over the terminal. It does nothing when stdin or stdout
// is not a terminal or the environment already decides the answer.
func Probe() {
	if !probeEligible(os.Getenv) {
		return
	}
	in, out := int(os.Stdin.Fd()), int(os.Stdout.Fd())
	if !term.IsTerminal(in) || !term.IsTerminal(out) {
		return
	}
	state, err := term.MakeRaw(in)
	if err != nil {
		return
	}
	// Best effort: there is nothing more to do if restoring fails.
	defer func() { _ = term.Restore(in, state) }()
	if os.Stdin.SetReadDeadline(time.Now().Add(probeTimeout)) != nil {
		return
	}
	defer func() { _ = os.Stdin.SetReadDeadline(time.Time{}) }()
	if _, err := os.Stdout.WriteString(probeQuery); err != nil {
		return
	}
	var buf []byte
	chunk := make([]byte, 256)
	for len(buf) < 4096 {
		n, err := os.Stdin.Read(chunk)
		buf = append(buf, chunk[:n]...)
		if graphics, done := parseProbeReply(buf); done || err != nil {
			if graphics {
				probedKitty.Store(true)
			}
			return
		}
	}
}
