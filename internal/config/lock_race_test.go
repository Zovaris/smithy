package config

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestStaleLockTakeoverLetsOneWriterIn(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config.json")
	for round := 0; round < 50; round++ {
		if err := os.WriteFile(file+".lock", []byte("999999999\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		var inside, overlap atomic.Int32
		var wg sync.WaitGroup
		for range 4 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = withLock(file, func() error {
					if inside.Add(1) > 1 {
						overlap.Add(1)
					}
					time.Sleep(2 * time.Millisecond)
					inside.Add(-1)
					return nil
				})
			}()
		}
		wg.Wait()
		if overlap.Load() > 0 {
			t.Fatalf("round %d: two writers held the lock at once", round)
		}
	}
	matches, _ := filepath.Glob(file + ".lock*")
	if len(matches) != 0 {
		t.Fatalf("left behind %v", matches)
	}
}
