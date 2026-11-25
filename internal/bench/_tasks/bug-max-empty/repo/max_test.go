package p

import "testing"

func TestMax(t *testing.T) {
	if m, ok := Max([]int{3, 9, 2}); m != 9 || !ok {
		t.Fatalf("Max = %d %v", m, ok)
	}
	if m, ok := Max(nil); m != 0 || ok {
		t.Fatalf("Max(nil) = %d %v", m, ok)
	}
}
