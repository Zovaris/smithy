package auto

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/config"
)

type blockingSource struct {
	started chan struct{}
	release chan struct{}
	done    atomic.Bool
}

func (s *blockingSource) Name() string { return "slow" }
func (s *blockingSource) Poll(time.Time) ([]Issue, error) {
	close(s.started)
	<-s.release
	s.done.Store(true)
	return nil, nil
}
func (s *blockingSource) Report(string, string, string) error { return nil }

func TestRunWaitsForCycleOnCancel(t *testing.T) {
	home(t)
	fakeSmithy(t)
	src := &blockingSource{started: make(chan struct{}), release: make(chan struct{})}
	Sources["slow"] = func(Settings) (Source, error) { return src, nil }
	t.Cleanup(func() { delete(Sources, "slow") })
	if err := config.Set(config.UserFile(), "auto.sources", "slow"); err != nil {
		t.Fatal(err)
	}
	sv, err := NewServer(ServeOptions{Agent: "claude"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	ret := make(chan error, 1)
	go func() { ret <- sv.Run(ctx) }()
	<-src.started
	cancel()
	time.AfterFunc(100*time.Millisecond, func() { close(src.release) })
	select {
	case <-ret:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}
	if !src.done.Load() {
		t.Fatal("Run returned while its cycle was still running")
	}
}
