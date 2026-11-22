package wait

import (
	"testing"
	"time"
)

func TestTimer(t *testing.T) {
	var slept time.Duration
	old := sleep
	sleep = func(d time.Duration) { slept = d }
	t.Cleanup(func() { sleep = old })
	if code := Main([]string{"timer:10"}); code != 0 || slept != 10*time.Minute {
		t.Fatalf("code %d slept %v", code, slept)
	}
	if code := Main([]string{"timer:0.5"}); code != 0 || slept != 30*time.Second {
		t.Fatalf("code %d slept %v", code, slept)
	}
	for _, a := range [][]string{{"timer:0"}, {"timer:x"}, {"timer:"}, {"timer:5", "pr:1"}} {
		if code := Main(a); code != 2 {
			t.Errorf("%v: code %d, want 2", a, code)
		}
	}
}
