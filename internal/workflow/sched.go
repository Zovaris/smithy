package workflow

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Scheduler runs due workflows (Tick), runs by hand (RunNow) and webhook runs, one run per
// workflow at a time, and keeps each running workflow's live record. OnRun sees each finished run.
type Scheduler struct {
	Store Store
	Env   Env
	OnRun func(Record)

	mu      sync.Mutex
	running map[string]bool
	live    map[string]Record
	wg      sync.WaitGroup
}

func (sc *Scheduler) claim(name string) bool {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.running == nil {
		sc.running, sc.live = map[string]bool{}, map[string]Record{}
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
	delete(sc.live, name)
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

// Live is name's run so far, while it runs.
func (sc *Scheduler) Live(name string) (Record, bool) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	r, ok := sc.live[name]
	return r, ok
}

func (sc *Scheduler) run(ctx context.Context, w Workflow, in Invocation) Record {
	defer sc.free(w.Name)
	env := sc.Env
	if env.Load == nil {
		env.Load = sc.Store.Load
	}
	env.Progress = func(r Record) {
		sc.mu.Lock()
		sc.live[w.Name] = r
		sc.mu.Unlock()
	}
	r := RunWith(ctx, w, env, in)
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
			sc.run(ctx, w, Invocation{Trigger: TriggerSchedule})
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
	return sc.Start(ctx, name, Invocation{Trigger: trigger})
}

// Start runs name with in and waits for it; it fails when name is already running.
func (sc *Scheduler) Start(ctx context.Context, name string, in Invocation) (Record, error) {
	w, _, err := sc.Store.Get(name)
	if err != nil {
		return Record{}, err
	}
	if !sc.claim(name) {
		return Record{}, fmt.Errorf("workflow %s is already running", name)
	}
	return sc.run(ctx, w, in), nil
}
