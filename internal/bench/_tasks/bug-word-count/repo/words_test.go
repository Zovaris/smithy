package p

import "testing"

func TestWords(t *testing.T) {
	for in, want := range map[string]int{"a b": 2, "  a   b  c ": 3, "": 0, "   ": 0} {
		if got := Words(in); got != want {
			t.Errorf("Words(%q) = %d, want %d", in, got, want)
		}
	}
}
