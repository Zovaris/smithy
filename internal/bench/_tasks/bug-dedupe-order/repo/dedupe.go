package p

import "sort"

// Dedupe drops repeated items, keeping the first of each in order.
func Dedupe(xs []string) []string {
	seen := map[string]bool{}
	for _, x := range xs {
		seen[x] = true
	}
	out := []string{}
	for x := range seen {
		out = append(out, x)
	}
	sort.Strings(out)
	return out
}
