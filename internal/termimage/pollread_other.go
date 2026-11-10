//go:build !(linux || darwin || freebsd || netbsd || openbsd || dragonfly)

package termimage

import (
	"errors"
	"time"
)

// readReady is unavailable here, so Probe is skipped.
func readReady(int, time.Duration) (bool, error) { return false, errors.New("unsupported") }
