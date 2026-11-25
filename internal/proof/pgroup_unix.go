//go:build !windows

package proof

import (
	"os"
	"os/exec"
	"syscall"
)

// ownGroup runs cmd in its own process group so a signal to smithy can be forwarded to the
// whole command tree.
func ownGroup(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

func forward(cmd *exec.Cmd, s os.Signal) {
	if n, ok := s.(syscall.Signal); ok && cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, n)
	}
}
