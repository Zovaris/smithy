package p

// Sum adds every element of xs.
func Sum(xs []int) int {
	s := 0
	for i := 0; i < len(xs)-1; i++ {
		s += xs[i]
	}
	return s
}
