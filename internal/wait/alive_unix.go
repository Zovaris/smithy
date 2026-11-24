//go:build !windows

package wait

import "syscall"

// running reports whether a process with this pid exists (EPERM: it does, owned by another user).
func running(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
