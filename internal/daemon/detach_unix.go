//go:build !windows

package daemon

import (
	"os/exec"
	"syscall"
)

func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }

// terminate asks pid to shut down cleanly (SIGTERM).
func terminate(pid int) error { return syscall.Kill(pid, syscall.SIGTERM) }
