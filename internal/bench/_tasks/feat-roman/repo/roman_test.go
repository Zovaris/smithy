package p

import "testing"

func TestRoman(t *testing.T) {
	for n, want := range map[int]string{1: "I", 4: "IV", 9: "IX", 14: "XIV", 1994: "MCMXCIV", 3999: "MMMCMXCIX"} {
		if got := Roman(n); got != want {
			t.Errorf("Roman(%d) = %q, want %q", n, got, want)
		}
	}
}
