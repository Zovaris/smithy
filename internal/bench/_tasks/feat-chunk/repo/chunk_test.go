package p

import (
	"fmt"
	"testing"
)

func TestChunk(t *testing.T) {
	if got := fmt.Sprint(Chunk([]int{1, 2, 3, 4, 5}, 2)); got != "[[1 2] [3 4] [5]]" {
		t.Fatalf("Chunk = %s", got)
	}
	if got := len(Chunk(nil, 3)); got != 0 {
		t.Fatalf("Chunk(nil) has %d chunks", got)
	}
}
