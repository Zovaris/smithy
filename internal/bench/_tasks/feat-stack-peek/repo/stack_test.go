package p

import "testing"

func TestPeek(t *testing.T) {
	var s Stack
	if _, ok := s.Peek(); ok {
		t.Fatal("Peek on empty stack")
	}
	s.Push(1)
	s.Push(2)
	if v, ok := s.Peek(); v != 2 || !ok {
		t.Fatalf("Peek = %d %v", v, ok)
	}
	if v, _ := s.Pop(); v != 2 {
		t.Fatal("Peek removed the item")
	}
}
