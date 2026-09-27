//go:build unix

package plugin

import (
	"os/exec"
	"syscall"
)

// configureProcessGroup starts the plugin in its own process group and kills
// the whole group on timeout, so child processes the plugin started do not
// outlive it.
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
