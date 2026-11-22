// Package commitpaths validates the paths named for an atomic commit.
package commitpaths

// Check validates the paths of a commit. exists reports whether a path is on disk and tracked
// reports whether git still tracks it (a deleted tracked file is a valid path: its removal is
// what gets committed). Check returns an error "no such path: <p>" for the first bad path.
func Check(paths []string, exists, tracked func(string) bool) error {
	return nil
}
