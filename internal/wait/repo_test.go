package wait

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestSeenSetFirstRunRecordsThenReportsOnlyNew(t *testing.T) {
	t.Setenv("TEND_HOME", t.TempDir())
	s := &seenSet{file: filepath.Join(t.TempDir(), "x.json"), keys: map[string]bool{}}
	if got := s.fresh([]string{"1", "2"}); got != nil {
		t.Fatalf("first run reported %v", got)
	}
	if got := s.fresh([]string{"1", "2", "3"}); !reflect.DeepEqual(got, []string{"3"}) {
		t.Fatalf("got %v", got)
	}
	again := &seenSet{file: s.file, keys: map[string]bool{}}
	again.init = true
	for _, k := range []string{"1", "2", "3"} {
		again.keys[k] = true
	}
	if got := again.fresh([]string{"3", "4"}); !reflect.DeepEqual(got, []string{"4"}) {
		t.Fatalf("after restart got %v", got)
	}
}

func TestSeenSetPersists(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TEND_HOME", dir)
	a := newSeen("unit")
	a.fresh([]string{"a"})
	b := newSeen("unit")
	if !b.init || !b.keys["a"] {
		t.Fatalf("not persisted: %+v", b)
	}
	if got := b.fresh([]string{"a", "b"}); !reflect.DeepEqual(got, []string{"b"}) {
		t.Fatalf("got %v", got)
	}
}
