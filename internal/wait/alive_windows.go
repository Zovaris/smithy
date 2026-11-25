//go:build windows

package wait

import "os"

// running reports whether a process with this pid exists.
func running(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = p.Release()
	return true
}
