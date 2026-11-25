package p

import "testing"

func TestTotal(t *testing.T) {
	if got := Total([]int{1000, 250}); got != 1350 {
		t.Fatalf("Total = %d", got)
	}
	if got := Tax(1250); got != 100 {
		t.Fatalf("Tax = %d", got)
	}
}
