package p

import "strings"

// Words counts the words of s.
func Words(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, " ") + 1
}
