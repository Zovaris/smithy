package p

import (
	"math"
	"testing"
)

func TestToCelsius(t *testing.T) {
	for f, want := range map[float64]float64{32: 0, 212: 100, -40: -40} {
		if got := ToCelsius(f); math.Abs(got-want) > 1e-9 {
			t.Errorf("ToCelsius(%v) = %v, want %v", f, got, want)
		}
	}
}
