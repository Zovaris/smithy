package sandbox

import "path/filepath"

// RealPath resolves symlinks so a linked warden home still matches; p as given when it can't.
func RealPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}
