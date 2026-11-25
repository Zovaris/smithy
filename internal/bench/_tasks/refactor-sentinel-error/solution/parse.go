package p

import (
	"errors"
	"strconv"
)

// ErrEmpty is returned for empty input.
var ErrEmpty = errors.New("empty input")

// Parse reads a positive integer.
func Parse(s string) (int, error) {
	if s == "" {
		return 0, ErrEmpty
	}
	return strconv.Atoi(s)
}
