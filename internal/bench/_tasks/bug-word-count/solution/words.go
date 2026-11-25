package p

import "strings"

// Words counts the words of s.
func Words(s string) int {
	return len(strings.Fields(s))
}
