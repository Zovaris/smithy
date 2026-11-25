//go:build !windows

// Package proc runs a command as the head of its own process tree and kills the whole tree, so a
// timeout or cancel never leaves children (background jobs, go test binaries) running.
package proc

import (
	"os/exec"
	"syscall"
)

// Tree puts cmd in its own process group and makes cancelling its context kill the whole group.
func Tree(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.Cancel = func() error { return Kill(cmd) }
}

// Kill kills cmd's process group.
func Kill(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
