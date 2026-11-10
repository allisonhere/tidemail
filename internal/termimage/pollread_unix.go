//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package termimage

import (
	"errors"
	"time"

	"golang.org/x/sys/unix"
)

// readReady waits up to d for fd to become readable. A terminal on stdin is a
// blocking file, so os.File.SetReadDeadline is unsupported there; poll(2) works
// on any descriptor and never leaves a goroutine holding a pending read.
func readReady(fd int, d time.Duration) (bool, error) {
	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	ms := int(d / time.Millisecond)
	if ms < 0 {
		ms = 0
	}
	for {
		n, err := unix.Poll(fds, ms)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		return n > 0, err
	}
}
