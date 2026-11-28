package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Triggers a run records.
const (
	TriggerSchedule = "schedule"
	TriggerWebhook  = "webhook"
	TriggerGitHub   = "github"
	TriggerWatch    = "watch"
)

// Event is one GitHub item a schedule saw: an alert, a red PR or a labeled issue.
type Event struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url,omitempty"`
}

// MaxWatched caps the files a watch schedule looks at.
const MaxWatched = 5000

// Scheduler fires enabled schedules (Tick: cron, GitHub events, file changes; Webhook from the
// daemon) and runs workflows by hand (Start), one run per workflow at a time, keeping each running
// workflow's live record. GitHub reads events (nil: GitHub schedules never fire); PollEvery is
// how often (default 5m). The first look at a GitHub or watch schedule only notes what is there.
type Scheduler struct {
	Store     Store
	Env       Env
	OnRun     func(Record)
	GitHub    func(ctx context.Context, dir string, t GitHubTrigger) ([]Event, error)
	PollEvery time.Duration

	mu      sync.Mutex
	running map[string]bool
	live    map[string]Record
	seen    map[string]map[string]bool
	polled  map[string]time.Time
	snaps   map[string]map[string]string
	wg      sync.WaitGroup
}

func (sc *Scheduler) init() {
	if sc.running == nil {
		sc.running, sc.live = map[string]bool{}, map[string]Record{}
		sc.seen, sc.polled, sc.snaps = map[string]map[string]bool{}, map[string]time.Time{}, map[string]map[string]string{}
	}
}

func (sc *Scheduler) claim(name string) bool {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.init()
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

// Live is workflow name's run so far, while it runs.
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

func merge(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

// fire starts schedule s's workflow in the background; false when it is already running.
func (sc *Scheduler) fire(ctx context.Context, s Schedule, trigger string, inputs map[string]string, now time.Time) bool {
	w, _, err := sc.Store.Get(s.Workflow)
	if err != nil || !sc.claim(w.Name) {
		return false
	}
	_ = sc.Store.Fired(s.Name, now)
	sc.wg.Add(1)
	go func() {
		defer sc.wg.Done()
		sc.run(ctx, w, Invocation{Trigger: trigger, Schedule: s.Name, Inputs: merge(s.Inputs, inputs)})
	}()
	return true
}

// Tick fires every due cron schedule, then looks for new GitHub events and file changes.
func (sc *Scheduler) Tick(ctx context.Context, now time.Time) {
	for _, s := range sc.Store.Due(now) {
		sc.fire(ctx, s, TriggerSchedule, nil, now)
	}
	every := sc.PollEvery
	if every <= 0 {
		every = 5 * time.Minute
	}
	for _, s := range sc.Store.Enabled() {
		switch {
		case s.GitHub != nil && sc.GitHub != nil:
			sc.mu.Lock()
			sc.init()
			due := now.Sub(sc.polled[s.Name]) >= every
			if due {
				sc.polled[s.Name] = now
			}
			sc.mu.Unlock()
			if due {
				sc.github(ctx, s, now)
			}
		case s.Watch != "":
			sc.watch(ctx, s, now)
		}
	}
}

func (sc *Scheduler) github(ctx context.Context, s Schedule, now time.Time) {
	dir := s.Dir
	if dir == "" {
		if w, _, err := sc.Store.Get(s.Workflow); err == nil {
			dir = w.Dir
		}
	}
	evs, err := sc.GitHub(ctx, dir, *s.GitHub)
	if err != nil {
		return
	}
	sc.mu.Lock()
	seen, primed := sc.seen[s.Name]
	if !primed {
		seen = map[string]bool{}
		sc.seen[s.Name] = seen
	}
	var fresh []Event
	for _, e := range evs {
		if !seen[e.ID] {
			if primed {
				fresh = append(fresh, e)
			}
			seen[e.ID] = true
		}
	}
	sc.mu.Unlock()
	if len(fresh) == 0 {
		return
	}
	b, _ := json.Marshal(fresh)
	if !sc.fire(ctx, s, TriggerGitHub, map[string]string{"event": string(b)}, now) {
		sc.mu.Lock()
		for _, e := range fresh {
			delete(seen, e.ID)
		}
		sc.mu.Unlock()
	}
}

// snapshot is path → size and mtime for a file, or every file under a folder (no .git or
// node_modules), at most MaxWatched.
func snapshot(path string) map[string]string {
	out := map[string]string{}
	path = dirOr(path)
	_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if n := d.Name(); p != path && (n == ".git" || n == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if len(out) >= MaxWatched {
			return filepath.SkipAll
		}
		if fi, err := d.Info(); err == nil {
			out[p] = strconv.FormatInt(fi.Size(), 10) + "@" + strconv.FormatInt(fi.ModTime().UnixNano(), 10)
		}
		return nil
	})
	return out
}

func (sc *Scheduler) watch(ctx context.Context, s Schedule, now time.Time) {
	cur := snapshot(s.Watch)
	sc.mu.Lock()
	sc.init()
	prev, primed := sc.snaps[s.Name]
	sc.snaps[s.Name] = cur
	sc.mu.Unlock()
	if !primed {
		return
	}
	var changed []string
	for p, v := range cur {
		if prev[p] != v {
			changed = append(changed, p)
		}
	}
	for p := range prev {
		if _, ok := cur[p]; !ok {
			changed = append(changed, p)
		}
	}
	if len(changed) == 0 {
		return
	}
	sort.Strings(changed)
	if !sc.fire(ctx, s, TriggerWatch, map[string]string{"changed": strings.Join(changed, "\n")}, now) {
		sc.mu.Lock()
		sc.snaps[s.Name] = prev
		sc.mu.Unlock()
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

// RunNow runs workflow name once with its default inputs and waits for it.
func (sc *Scheduler) RunNow(ctx context.Context, name, trigger string) (Record, error) {
	return sc.Start(ctx, name, Invocation{Trigger: trigger})
}

// Start runs workflow name with in and waits for it; it fails when name is already running.
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

// Webhook runs schedule name's workflow for a webhook call that passed CheckHook, with body's
// fields as inputs (only the ones the workflow declares), and waits for it.
func (sc *Scheduler) Webhook(ctx context.Context, s Schedule, body map[string]string) (Record, error) {
	w, _, err := sc.Store.Get(s.Workflow)
	if err != nil {
		return Record{}, err
	}
	in := map[string]string{}
	for k, v := range body {
		if _, ok := w.Inputs[k]; ok {
			in[k] = v
		}
	}
	if !sc.claim(w.Name) {
		return Record{}, fmt.Errorf("workflow %s is already running", w.Name)
	}
	_ = sc.Store.Fired(s.Name, time.Now())
	return sc.run(ctx, w, Invocation{Trigger: TriggerWebhook, Schedule: s.Name, Inputs: merge(s.Inputs, in)}), nil
}

// GitHubEvents reads trigger t's current items with gh in dir.
func GitHubEvents(ctx context.Context, dir string, t GitHubTrigger) ([]Event, error) {
	var cmd string
	switch t.Event {
	case EventDependabot:
		cmd = `gh api 'repos/{owner}/{repo}/dependabot/alerts?state=open' --jq '[.[] | {id: (.number|tostring), title: (.security_advisory.severity + " " + .dependency.package.name + ": " + .security_advisory.summary), url: .html_url}]'`
	case EventPRRed:
		cmd = `gh pr list --state open --limit 50 --json number,title,url,headRefOid,statusCheckRollup --jq '[.[] | select([.statusCheckRollup[]? | (.conclusion // .state)] | any(. == "FAILURE" or . == "ERROR")) | {id: ((.number|tostring) + "@" + .headRefOid), title, url}]'`
	case EventIssue:
		if strings.ContainsAny(t.Label, `'"\`+"`$") {
			return nil, fmt.Errorf("label holds a quote")
		}
		cmd = `gh issue list --state open --limit 50 --label '` + t.Label + `' --json number,title,url --jq '[.[] | {id: (.number|tostring), title, url}]'`
	default:
		return nil, fmt.Errorf("unknown event %q", t.Event)
	}
	out, exit, err := Shell(ctx, dir, cmd, nil)
	if err != nil || exit != 0 {
		return nil, fmt.Errorf("gh: %s", clipText(out, 200))
	}
	var evs []Event
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &evs); err != nil {
		return nil, err
	}
	return evs, nil
}
