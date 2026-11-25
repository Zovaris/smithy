package p

import "testing"

func TestLeap(t *testing.T) {
	for y, want := range map[int]bool{2024: true, 2023: false, 1900: false, 2000: true} {
		if got := Leap(y); got != want {
			t.Errorf("Leap(%d) = %v, want %v", y, got, want)
		}
	}
}
