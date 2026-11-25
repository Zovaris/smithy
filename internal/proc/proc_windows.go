// Package proc runs a command as the head of its own process tree and kills the whole tree, so a
// timeout or cancel never leaves children (background jobs, go test binaries) running.
package proc

import (
	"os/exec"
	"strconv"
	"syscall"
)

// Tree starts cmd in a new process group and makes cancelling its context kill the whole tree.
func Tree(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= syscall.CREATE_NEW_PROCESS_GROUP
	cmd.Cancel = func() error { return Kill(cmd) }
}

// Kill kills cmd and every process it started (taskkill /T).
func Kill(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	err := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
	_ = cmd.Process.Kill()
	return err
}
