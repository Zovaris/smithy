package p

import (
	"reflect"
	"testing"
)

func TestDedupe(t *testing.T) {
	got := Dedupe([]string{"b", "a", "b", "c", "a"})
	if want := []string{"b", "a", "c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Dedupe = %v, want %v", got, want)
	}
}
