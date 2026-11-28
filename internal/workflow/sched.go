package workflow

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Scheduler runs due workflows (Tick) and runs by hand (RunNow), one run per workflow at a time.
// OnRun, when set, sees each finished run.
type Scheduler struct {
	Store Store
	Env   Env
	OnRun func(Record)

	mu      sync.Mutex
	running map[string]bool
	wg      sync.WaitGroup
}

func (sc *Scheduler) claim(name string) bool {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.running == nil {
		sc.running = map[string]bool{}
	}
	if sc.running[name] {
		return false
	}
	sc.running[name] = true
	return true
}

func (sc *Scheduler) free(name string) {
	sc.mu.Lock()
	delete(sc.running, name)
	sc.mu.Unlock()
}

// Running lists the workflows running now.
func (sc *Scheduler) Running() map[string]bool {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	out := map[string]bool{}
	for k := range sc.running {
		out[k] = true
	}
	return out
}

func (sc *Scheduler) run(ctx context.Context, w Workflow, trigger string) Record {
	defer sc.free(w.Name)
	r := Run(ctx, w, sc.Env)
	r.Trigger = trigger
	if err := sc.Store.Record(r); err != nil && r.Error == "" {
		r.Error = "history: " + err.Error()
	}
	if sc.OnRun != nil {
		sc.OnRun(r)
	}
	return r
}

// Tick starts every due workflow in the background.
func (sc *Scheduler) Tick(ctx context.Context, now time.Time) {
	for _, w := range sc.Store.Due(now) {
		if !sc.claim(w.Name) {
			continue
		}
		if err := sc.Store.Fired(w.Name, now); err != nil {
			sc.free(w.Name)
			continue
		}
		sc.wg.Add(1)
		go func(w Workflow) {
			defer sc.wg.Done()
			sc.run(ctx, w, "schedule")
		}(w)
	}
}

// Wait blocks until the runs Tick started end.
func (sc *Scheduler) Wait() { sc.wg.Wait() }

// Loop ticks every minute until ctx ends.
func (sc *Scheduler) Loop(ctx context.Context) {
	for {
		sc.Tick(ctx, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(time.Now().Truncate(time.Minute).Add(time.Minute))):
		}
	}
}

// RunNow runs name once, enabled or not (the user asked for it), and waits for it.
func (sc *Scheduler) RunNow(ctx context.Context, name, trigger string) (Record, error) {
	w, _, err := sc.Store.Get(name)
	if err != nil {
		return Record{}, err
	}
	if !sc.claim(name) {
		return Record{}, fmt.Errorf("workflow %s is already running", name)
	}
	return sc.run(ctx, w, trigger), nil
}
