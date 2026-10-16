//go:build unix

package runner

import (
	"os/exec"
	"syscall"
)

// setProcessGroup starts cmd as the leader of a new process group and makes
// context cancellation kill the whole group, not only the test binary.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

// killProcessGroup kills whatever is left of cmd's process group after the
// binary exited. It is a no-op when the group is already gone.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
