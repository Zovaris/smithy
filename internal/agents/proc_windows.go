//go:build windows

package agents

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

func exitCode(err error) int {
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() < 0 {
		return 1
	}
	return ee.ExitCode()
}

func ownGroup(cmd *exec.Cmd) {}

func forward(cmd *exec.Cmd, s os.Signal) {
	if cmd.Process != nil {
		cmd.Process.Kill()
	}
}
