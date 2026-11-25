package p

// Total returns the order total in cents, tax included (8%, rounded down).
func Total(items []int) int {
	sum := 0
	for _, c := range items {
		sum += c
	}
	return sum + sum*8/100
}
