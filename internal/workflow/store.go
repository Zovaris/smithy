package workflow

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Zovaris/smithy/internal/layout"
)

// CatchUp is how late a missed scheduled run may still start (the daemon was down or asleep).
const CatchUp = time.Hour

// Home is ~/.smithy/workflows: workflow files, schedules/ and runs/.
func Home() string { return filepath.Join(layout.Home(), "workflows") }

// Store keeps workflow files in Dir, schedules in Dir/schedules, their enable state in
// Dir/state.json and runs in Dir/runs/<workflow>/. Keep is how many runs to keep per workflow
// (0: 50).
type Store struct {
	Dir  string
	Keep int
}

// Info is one workflow as listed: Schedules names the schedules attached to it; Err says the
// file doesn't parse.
type Info struct {
	Workflow
	File      string   `json:"file"`
	Schedules []string `json:"schedules,omitempty"`
	Last      *Record  `json:"last,omitempty"`
	Err       string   `json:"error,omitempty"`
}

// SchedInfo is one schedule as listed. Changed: it was enabled but its file or its workflow
// (calls included) changed since, so it doesn't fire until enabled again.
type SchedInfo struct {
	Schedule
	File    string     `json:"file"`
	Enabled bool       `json:"enabled"`
	Changed bool       `json:"changed,omitempty"`
	Next    *time.Time `json:"next,omitempty"`
	Last    *Record    `json:"last,omitempty"`
	Err     string     `json:"error,omitempty"`
}

type state struct {
	Enabled map[string]string    `json:"enabled"`
	Since   map[string]time.Time `json:"since"`
	Hooks   map[string]string    `json:"hooks,omitempty"`
}

var mu sync.Mutex

func (s Store) schedDir() string { return filepath.Join(s.Dir, "schedules") }

func (s Store) file(name string) (string, error) {
	if !nameRe.MatchString(name) {
		return "", fmt.Errorf("workflow: no workflow named %q", name)
	}
	return filepath.Join(s.Dir, name+".json"), nil
}

func (s Store) schedFile(name string) (string, error) {
	if !nameRe.MatchString(name) {
		return "", fmt.Errorf("schedule: no schedule named %q", name)
	}
	return filepath.Join(s.schedDir(), name+".json"), nil
}

func (s Store) load() state {
	st := state{}
	if b, err := os.ReadFile(filepath.Join(s.Dir, "state.json")); err == nil {
		_ = json.Unmarshal(b, &st)
	}
	if st.Enabled == nil {
		st.Enabled = map[string]string{}
	}
	if st.Since == nil {
		st.Since = map[string]time.Time{}
	}
	if st.Hooks == nil {
		st.Hooks = map[string]string{}
	}
	return st
}

func (s Store) save(st state) error {
	b, _ := json.MarshalIndent(st, "", "  ")
	return writeAtomic(filepath.Join(s.Dir, "state.json"), b)
}

func (s Store) state() state {
	mu.Lock()
	defer mu.Unlock()
	return s.load()
}

func writeAtomic(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func hash(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// Get reads and parses one workflow file.
func (s Store) Get(name string) (Workflow, []byte, error) {
	f, err := s.file(name)
	if err != nil {
		return Workflow{}, nil, err
	}
	b, err := os.ReadFile(f)
	if errors.Is(err, os.ErrNotExist) {
		return Workflow{}, nil, fmt.Errorf("workflow: no workflow named %q", name)
	}
	if err != nil {
		return Workflow{}, nil, err
	}
	w, err := Parse(b)
	if err == nil && w.Name != name {
		err = fmt.Errorf("workflow: %s.json names itself %q", name, w.Name)
	}
	return w, b, err
}

// GetSchedule reads and parses one schedule file.
func (s Store) GetSchedule(name string) (Schedule, []byte, error) {
	f, err := s.schedFile(name)
	if err != nil {
		return Schedule{}, nil, err
	}
	b, err := os.ReadFile(f)
	if errors.Is(err, os.ErrNotExist) {
		return Schedule{}, nil, fmt.Errorf("schedule: no schedule named %q", name)
	}
	if err != nil {
		return Schedule{}, nil, err
	}
	sc, err := ParseSchedule(b)
	if err == nil && sc.Name != name {
		err = fmt.Errorf("schedule: %s.json names itself %q", name, sc.Name)
	}
	return sc, b, err
}

func fileNames(dir string) []string {
	paths, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	var out []string
	for _, p := range paths {
		if n := strings.TrimSuffix(filepath.Base(p), ".json"); n != "state" {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// List returns every workflow file, by name.
func (s Store) List() ([]Info, error) {
	by := map[string][]string{}
	for _, n := range fileNames(s.schedDir()) {
		if sc, _, err := s.GetSchedule(n); err == nil {
			by[sc.Workflow] = append(by[sc.Workflow], n)
		}
	}
	var out []Info
	for _, n := range fileNames(s.Dir) {
		w, _, err := s.Get(n)
		in := Info{Workflow: w, File: filepath.Join(s.Dir, n+".json"), Schedules: by[n]}
		if in.Name == "" {
			in.Name = n
		}
		if err != nil {
			in.Err = err.Error()
		}
		if h, _ := s.History(n, 1); len(h) > 0 {
			in.Last = &h[0]
		}
		out = append(out, in)
	}
	return out, nil
}

// pin is what an enable fixes: the schedule file and its workflow with every workflow it calls.
func (s Store) pin(sched []byte, workflow string) (string, error) {
	seen := map[string][]byte{}
	var walk func(name string, depth int) error
	walk = func(name string, depth int) error {
		if _, ok := seen[name]; ok || depth > MaxCallDepth {
			return nil
		}
		w, b, err := s.Get(name)
		if err != nil {
			return err
		}
		seen[name] = b
		var calls func([]Step) error
		calls = func(ss []Step) error {
			for _, st := range ss {
				if st.Kind == KindCall {
					if err := walk(st.Workflow, depth+1); err != nil {
						return err
					}
				}
				if err := calls(st.Do); err != nil {
					return err
				}
			}
			return nil
		}
		return calls(w.Steps)
	}
	if err := walk(workflow, 0); err != nil {
		return "", err
	}
	h := sha256.New()
	h.Write(sched)
	for _, n := range sortedKeysB(seen) {
		h.Write([]byte("\x00" + n + "\x00"))
		h.Write(seen[n])
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func sortedKeysB(m map[string][]byte) []string {
	k := make([]string, 0, len(m))
	for x := range m {
		k = append(k, x)
	}
	sort.Strings(k)
	return k
}

// pinned reports whether schedule name is enabled and nothing it pins changed.
func (s Store) pinned(name string, st state) (Schedule, bool, bool) {
	want, on := st.Enabled[name]
	sc, b, err := s.GetSchedule(name)
	if !on {
		return sc, false, false
	}
	if err != nil {
		return sc, false, true
	}
	got, err := s.pin(b, sc.Workflow)
	if err != nil || got != want {
		return sc, false, true
	}
	return sc, true, false
}

// Schedules returns every schedule file, by name.
func (s Store) Schedules() ([]SchedInfo, error) {
	st := s.state()
	var out []SchedInfo
	for _, n := range fileNames(s.schedDir()) {
		sc, _, err := s.GetSchedule(n)
		in := SchedInfo{Schedule: sc, File: filepath.Join(s.schedDir(), n+".json")}
		if in.Name == "" {
			in.Name = n
		}
		if err != nil {
			in.Err = err.Error()
		} else if _, _, werr := s.Get(sc.Workflow); werr != nil {
			in.Err = werr.Error()
		}
		_, in.Enabled, in.Changed = s.pinned(n, st)
		if in.Enabled && sc.Cron != "" {
			if t := s.next(sc, st.Since[n], time.Now()); !t.IsZero() {
				in.Next = &t
			}
		}
		if h, _ := s.ScheduleHistory(n, 1); len(h) > 0 {
			in.Last = &h[0]
		}
		out = append(out, in)
	}
	return out, nil
}

// next is the cron time due after since, skipping runs missed by more than CatchUp.
func (s Store) next(sc Schedule, since, now time.Time) time.Time {
	c, err := ParseCron(sc.Cron)
	if err != nil {
		return time.Time{}
	}
	n := c.Next(since)
	for !n.IsZero() && n.Add(CatchUp).Before(now) {
		n = c.Next(n)
	}
	return n
}

// Enabled lists the schedules that are enabled with nothing they pin changed.
func (s Store) Enabled() []Schedule {
	st := s.state()
	var out []Schedule
	for _, n := range fileNames(s.schedDir()) {
		if sc, ok, _ := s.pinned(n, st); ok {
			out = append(out, sc)
		}
	}
	return out
}

// Due lists the enabled cron schedules whose next run is at or before now.
func (s Store) Due(now time.Time) []Schedule {
	st := s.state()
	var out []Schedule
	for _, sc := range s.Enabled() {
		if sc.Cron == "" {
			continue
		}
		if n := s.next(sc, st.Since[sc.Name], now); !n.IsZero() && !n.After(now) {
			out = append(out, sc)
		}
	}
	return out
}

// Fired records that schedule name fired at t.
func (s Store) Fired(name string, t time.Time) error {
	mu.Lock()
	defer mu.Unlock()
	st := s.load()
	if _, ok := st.Enabled[name]; !ok {
		return nil
	}
	st.Since[name] = t
	return s.save(st)
}

// EnableSchedule lets name fire from now, pinned to its file and its workflow as they are now.
func (s Store) EnableSchedule(name string, now time.Time) error {
	sc, b, err := s.GetSchedule(name)
	if err != nil {
		return err
	}
	p, err := s.pin(b, sc.Workflow)
	if err != nil {
		return fmt.Errorf("schedule %s: %w", name, err)
	}
	mu.Lock()
	defer mu.Unlock()
	st := s.load()
	st.Enabled[name], st.Since[name] = p, now
	if st.Hooks[name] == "" {
		st.Hooks[name] = newToken()
	}
	return s.save(st)
}

// DisableSchedule stops name.
func (s Store) DisableSchedule(name string) error {
	if _, err := s.schedFile(name); err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	st := s.load()
	delete(st.Enabled, name)
	delete(st.Since, name)
	return s.save(st)
}

// RemoveSchedule disables name and deletes its file.
func (s Store) RemoveSchedule(name string) error {
	f, err := s.schedFile(name)
	if err != nil {
		return err
	}
	if err := s.DisableSchedule(name); err != nil {
		return err
	}
	return os.Remove(f)
}

// SaveSchedule writes sc (validated); it never enables it.
func (s Store) SaveSchedule(sc Schedule) error {
	if err := sc.Validate(); err != nil {
		return err
	}
	f, err := s.schedFile(sc.Name)
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(sc, "", "  ")
	return writeAtomic(f, append(b, '\n'))
}

// Load reads name for a call step. A scheduled run's whole call tree is pinned when its
// schedule fires, so every stored workflow may be called.
func (s Store) Load(name string) (Workflow, bool, error) {
	w, _, err := s.Get(name)
	return w, err == nil, err
}

// HookToken is schedule name's webhook token (made on its first enable; "" before).
func (s Store) HookToken(name string) string { return s.state().Hooks[name] }

// CheckHook reports whether token opens schedule name: it is a webhook schedule, enabled with
// nothing it pins changed, and the token matches (constant time).
func (s Store) CheckHook(name, token string) (Schedule, bool) {
	sc, ok, _ := s.pinned(name, s.state())
	if !ok || !sc.Webhook || token == "" {
		return sc, false
	}
	want := s.HookToken(name)
	return sc, want != "" && subtle.ConstantTimeCompare([]byte(want), []byte(token)) == 1
}

func newToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Save writes w (validated). A save that changes a workflow turns off every schedule that pins
// it, through the pin check.
func (s Store) Save(w Workflow) error {
	if err := w.Validate(); err != nil {
		return err
	}
	f, err := s.file(w.Name)
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(w, "", "  ")
	return writeAtomic(f, append(b, '\n'))
}

// Add copies a workflow or schedule file (a repo's, say) into the store; a schedule lands disabled.
func (s Store) Add(src string) (string, error) {
	b, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	var probe map[string]json.RawMessage
	if json.Unmarshal(b, &probe) != nil {
		return "", fmt.Errorf("%s is not a JSON object", src)
	}
	if _, isSched := probe["workflow"]; isSched {
		sc, err := ParseSchedule(b)
		if err != nil {
			return "", err
		}
		f, _ := s.schedFile(sc.Name)
		if _, err := os.Stat(f); err == nil {
			return "", fmt.Errorf("schedule %s already exists", sc.Name)
		}
		if err := writeAtomic(f, b); err != nil {
			return "", err
		}
		return "schedule " + sc.Name, s.DisableSchedule(sc.Name)
	}
	w, err := Parse(b)
	if err != nil {
		return "", err
	}
	f, err := s.file(w.Name)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(f); err == nil {
		return "", fmt.Errorf("workflow %s already exists", w.Name)
	}
	return "workflow " + w.Name, writeAtomic(f, b)
}

// Record saves a run and drops the oldest past Keep.
func (s Store) Record(r Record) error {
	if !nameRe.MatchString(r.Workflow) {
		return fmt.Errorf("workflow: bad name %q", r.Workflow)
	}
	d := filepath.Join(s.Dir, "runs", r.Workflow)
	b, _ := json.MarshalIndent(r, "", "  ")
	if err := writeAtomic(filepath.Join(d, r.Started.UTC().Format("20060102T150405.000000000Z")+".json"), b); err != nil {
		return err
	}
	keep := s.Keep
	if keep <= 0 {
		keep = 50
	}
	old, _ := filepath.Glob(filepath.Join(d, "*.json"))
	sort.Strings(old)
	for len(old) > keep {
		_ = os.Remove(old[0])
		old = old[1:]
	}
	return nil
}

func (s Store) runs(workflow string, keep func(Record) bool, n int) []Record {
	files, _ := filepath.Glob(filepath.Join(s.Dir, "runs", workflow, "*.json"))
	sort.Sort(sort.Reverse(sort.StringSlice(files)))
	var out []Record
	for _, f := range files {
		if len(out) >= n {
			break
		}
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var r Record
		if json.Unmarshal(b, &r) == nil && keep(r) {
			out = append(out, r)
		}
	}
	return out
}

// History returns workflow name's newest runs first, at most n.
func (s Store) History(name string, n int) ([]Record, error) {
	if !nameRe.MatchString(name) {
		return nil, fmt.Errorf("workflow: no workflow named %q", name)
	}
	return s.runs(name, func(Record) bool { return true }, n), nil
}

// ScheduleHistory returns the newest runs schedule name started, at most n.
func (s Store) ScheduleHistory(name string, n int) ([]Record, error) {
	sc, _, err := s.GetSchedule(name)
	if err != nil {
		return nil, err
	}
	return s.runs(sc.Workflow, func(r Record) bool { return r.Schedule == name }, n), nil
}

// Migrate turns workflow files from before schedules (with schedule or webhook fields) into a
// workflow plus a schedule of the same name. A workflow that was enabled as it was gets its
// schedule enabled, keeping its webhook token. It returns what it moved.
func (s Store) Migrate(now time.Time) []string {
	var moved []string
	for _, n := range fileNames(s.Dir) {
		f := filepath.Join(s.Dir, n+".json")
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var raw map[string]json.RawMessage
		if json.Unmarshal(b, &raw) != nil {
			continue
		}
		cronRaw, hasCron := raw["schedule"]
		hookRaw, hasHook := raw["webhook"]
		if !hasCron && !hasHook {
			continue
		}
		sc := Schedule{Name: n, Workflow: n}
		_ = json.Unmarshal(cronRaw, &sc.Cron)
		_ = json.Unmarshal(hookRaw, &sc.Webhook)
		wroteHook := false
		if sc.Cron != "" && sc.Webhook {
			sc.Webhook = false
			hook := Schedule{Name: n + "-webhook", Workflow: n, Webhook: true}
			if _, _, err := s.GetSchedule(hook.Name); err != nil && hook.Validate() == nil {
				wroteHook = s.SaveSchedule(hook) == nil
			}
		}
		delete(raw, "schedule")
		delete(raw, "webhook")
		nb, _ := json.MarshalIndent(raw, "", "  ")
		if _, err := Parse(nb); err != nil {
			continue
		}
		st := s.state()
		wasOn := st.Enabled[n] == hash(b)
		if sc.Cron == "" && !sc.Webhook {
			continue
		}
		if _, _, err := s.GetSchedule(n); err == nil {
			continue
		}
		if err := writeAtomic(f, append(nb, '\n')); err != nil {
			continue
		}
		if err := s.SaveSchedule(sc); err != nil {
			continue
		}
		mu.Lock()
		st = s.load()
		delete(st.Enabled, n)
		mu.Unlock()
		if err := s.save(st); err != nil {
			continue
		}
		if wasOn {
			_ = s.EnableSchedule(n, now)
			if wroteHook {
				_ = s.EnableSchedule(n+"-webhook", now)
			}
		}
		moved = append(moved, n)
	}
	return moved
}
