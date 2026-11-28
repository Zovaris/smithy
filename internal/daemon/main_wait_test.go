//go:build harness

package daemon

import (
	"sync"
	"testing"
	"time"
)

func TestWaitBoundedWaitsThenGivesUp(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { time.Sleep(20 * time.Millisecond); wg.Done() }()
	if !waitBounded(&wg, time.Second) {
		t.Fatal("finished loops reported as stuck")
	}
	wg.Add(1)
	defer wg.Done()
	if waitBounded(&wg, 20*time.Millisecond) {
		t.Fatal("a stuck loop reported as finished")
	}
}
