package p

// Max returns the largest element of xs and whether xs had any.
func Max(xs []int) (int, bool) {
	m := xs[0]
	for _, x := range xs {
		if x > m {
			m = x
		}
	}
	return m, true
}
