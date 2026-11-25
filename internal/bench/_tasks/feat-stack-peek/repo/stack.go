package p

// Stack is a last-in, first-out stack of ints.
type Stack struct{ items []int }

// Push adds v on top.
func (s *Stack) Push(v int) { s.items = append(s.items, v) }

// Pop removes and returns the top item.
func (s *Stack) Pop() (int, bool) {
	if len(s.items) == 0 {
		return 0, false
	}
	v := s.items[len(s.items)-1]
	s.items = s.items[:len(s.items)-1]
	return v, true
}
