//go:build !unix

package plugin

import "os/exec"

// configureProcessGroup keeps the default behavior: on timeout only the plugin
// process itself is killed, and cmd.WaitDelay bounds the wait for its output.
func configureProcessGroup(*exec.Cmd) {}
