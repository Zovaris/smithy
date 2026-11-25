package p

// Chunk splits xs into slices of at most n items.
func Chunk(xs []int, n int) [][]int {
	var out [][]int
	for len(xs) > 0 {
		k := min(n, len(xs))
		out = append(out, xs[:k])
		xs = xs[k:]
	}
	return out
}
