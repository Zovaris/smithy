package p

import "testing"

func TestAverage(t *testing.T) {
	if got := Average([]float64{1, 2, 3}); got != 2 {
		t.Fatalf("Average = %v", got)
	}
	if got := Spread([]float64{1, 2, 3}); got != 1 {
		t.Fatalf("Spread = %v", got)
	}
}
