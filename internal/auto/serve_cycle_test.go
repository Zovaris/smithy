package auto

import (
	"context"
	"errors"
	"strings"
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

func TestRunOwnsACycleThatOutlastsTheGrace(t *testing.T) {
	home(t)
	fakeSmithy(t)
	old := cycleGrace
	cycleGrace = 20 * time.Millisecond
	t.Cleanup(func() { cycleGrace = old })
	src := &blockingSource{started: make(chan struct{}), release: make(chan struct{})}
	Sources["slow"] = func(Settings) (Source, error) { return src, nil }
	t.Cleanup(func() { delete(Sources, "slow") })
	if err := config.Set(config.UserFile(), "auto.sources", "slow"); err != nil {
		t.Fatal(err)
	}
	sv, err := NewServer(ServeOptions{Agent: "claude"}, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	ret := make(chan error, 1)
	go func() { ret <- sv.Run(ctx) }()
	<-src.started
	cancel()
	time.AfterFunc(200*time.Millisecond, func() { close(src.release) })
	select {
	case <-ret:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}
	if !src.done.Load() {
		t.Fatal("Run returned while its cycle was still running")
	}
}

func TestCycleSkipsGitHubOutsideRepo(t *testing.T) {
	home(t)
	fakeSmithy(t)
	polled := false
	Sources["github"] = func(Settings) (Source, error) {
		return &brokenGitHub{poll: func() { polled = true }}, nil
	}
	t.Cleanup(func() { Sources["github"] = func(s Settings) (Source, error) { return GitHub{Settings: s}, nil } })
	if err := config.Set(config.UserFile(), "auto.sources", "github"); err != nil {
		t.Fatal(err)
	}
	var logs []string
	sv, err := NewServer(ServeOptions{Agent: "claude", Cwd: t.TempDir()}, func(l string) { logs = append(logs, l) })
	if err != nil {
		t.Fatal(err)
	}
	sv.Cycle()
	if polled {
		t.Error("github polled outside a git repository")
	}
	for _, l := range logs {
		if strings.Contains(l, "poll") {
			t.Errorf("logged %q; want a quiet skip", l)
		}
	}
}

type brokenGitHub struct{ poll func() }

func (*brokenGitHub) Name() string { return "github" }
func (f *brokenGitHub) Poll(time.Time) ([]Issue, error) {
	f.poll()
	return nil, errors.New("failed to run git: fatal: not a git repository")
}
func (*brokenGitHub) Report(string, string, string) error { return nil }
