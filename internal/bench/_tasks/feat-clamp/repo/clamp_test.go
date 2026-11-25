package p

import "testing"

func TestClamp(t *testing.T) {
	for _, c := range [][4]int{{5, 0, 10, 5}, {-3, 0, 10, 0}, {42, 0, 10, 10}} {
		if got := Clamp(c[0], c[1], c[2]); got != c[3] {
			t.Errorf("Clamp%v = %d", c[:3], got)
		}
	}
}
