// Package browser opens external URLs with the user's configured or platform
// default browser.
package browser

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

var startCommand = func(name string, args ...string) error {
	return exec.Command(name, args...).Start()
}

// Command returns the browser command for a target on the supplied platform.
// A configured command takes precedence over the platform default.
func Command(goos, configured, target string) (string, []string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", nil, fmt.Errorf("browser target is empty")
	}
	if configured = strings.TrimSpace(configured); configured != "" {
		return configured, []string{target}, nil
	}
	switch goos {
	case "darwin":
		return "open", []string{target}, nil
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", target}, nil
	case "linux":
		return "xdg-open", []string{target}, nil
	default:
		return "", nil, fmt.Errorf("opening a browser is unsupported on %s", goos)
	}
}

// Open opens target without waiting for the browser process to exit.
func Open(configured, target string) error {
	name, args, err := Command(runtime.GOOS, configured, target)
	if err != nil {
		return err
	}
	return startCommand(name, args...)
}
