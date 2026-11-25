package p

import (
	"errors"
	"testing"
)

func TestErrEmpty(t *testing.T) {
	_, err := Parse("")
	if !errors.Is(err, ErrEmpty) || err.Error() != "empty input" {
		t.Fatalf("Parse(\"\") err = %v", err)
	}
	if n, err := Parse("12"); n != 12 || err != nil {
		t.Fatalf("Parse(12) = %d %v", n, err)
	}
}
