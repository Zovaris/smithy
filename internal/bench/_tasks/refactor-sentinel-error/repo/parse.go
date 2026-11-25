package p

import (
	"errors"
	"strconv"
)

// Parse reads a positive integer.
func Parse(s string) (int, error) {
	if s == "" {
		return 0, errors.New("empty input")
	}
	return strconv.Atoi(s)
}
