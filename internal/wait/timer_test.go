package wait

import (
	"context"
	"testing"
	"time"
)

func TestTimer(t *testing.T) {
	var slept time.Duration
	old := sleep
	sleep = func(_ context.Context, d time.Duration) bool { slept = d; return true }
	t.Cleanup(func() { sleep = old })
	if code := Main([]string{"timer:10"}); code != 0 || slept != 10*time.Minute {
		t.Fatalf("code %d slept %v", code, slept)
	}
	if code := Main([]string{"timer:0.5"}); code != 0 || slept != 30*time.Second {
		t.Fatalf("code %d slept %v", code, slept)
	}
	for _, a := range [][]string{{"timer:0"}, {"timer:0.01"}, {"timer:NaN"}, {"timer:Inf"}, {"timer:-1"}, {"timer:x"}, {"timer:"}, {"timer:5", "pr:1"}} {
		if code := Main(a); code != 2 {
			t.Errorf("%v: code %d, want 2", a, code)
		}
	}
}

func TestTimerStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if code := Run(ctx, []string{"timer:10"}); code != 130 {
		t.Fatalf("code %d, want 130", code)
	}
	if time.Since(start) > time.Second {
		t.Fatal("a cancelled wait kept sleeping")
	}
}
