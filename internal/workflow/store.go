package workflow

import (
	"crypto/sha256"
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

// Home is ~/.smithy/workflows.
func Home() string { return filepath.Join(layout.Home(), "workflows") }

// Store keeps workflow files in Dir, their enable state in Dir/state.json and runs in
// Dir/runs/<name>/. Keep is how many runs to keep per workflow (0: 50).
type Store struct {
	Dir  string
	Keep int
}

// Info is one workflow as listed. Changed: it was enabled but the file changed since, so it
// doesn't run until enabled again. Err: the file doesn't parse.
type Info struct {
	Workflow
	File    string    `json:"file"`
	Enabled bool      `json:"enabled"`
	Changed bool      `json:"changed,omitempty"`
	Next    time.Time `json:"next,omitempty"`
	Last    *Record   `json:"last,omitempty"`
	Err     string    `json:"error,omitempty"`
}

type state struct {
	Enabled map[string]string    `json:"enabled"`
	Since   map[string]time.Time `json:"since"`
}

var mu sync.Mutex

func (s Store) file(name string) (string, error) {
	if !nameRe.MatchString(name) {
		return "", fmt.Errorf("workflow: no workflow named %q", name)
	}
	return filepath.Join(s.Dir, name+".json"), nil
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
	return st
}

func (s Store) save(st state) error {
	b, _ := json.MarshalIndent(st, "", "  ")
	return writeAtomic(filepath.Join(s.Dir, "state.json"), b)
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

// List returns every workflow file, by name.
func (s Store) List() ([]Info, error) {
	mu.Lock()
	st := s.load()
	mu.Unlock()
	paths, _ := filepath.Glob(filepath.Join(s.Dir, "*.json"))
	var out []Info
	for _, p := range paths {
		name := strings.TrimSuffix(filepath.Base(p), ".json")
		if name == "state" {
			continue
		}
		in := Info{File: p}
		w, b, err := s.Get(name)
		in.Workflow = w
		if in.Name == "" {
			in.Name = name
		}
		if err != nil {
			in.Err = err.Error()
		}
		if h, ok := st.Enabled[name]; ok {
			in.Enabled = err == nil && h == hash(b)
			in.Changed = !in.Enabled
		}
		if in.Enabled {
			in.Next = s.next(w, st.Since[name], time.Now())
		}
		if h, _ := s.History(name, 1); len(h) > 0 {
			in.Last = &h[0]
		}
		out = append(out, in)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// next is the scheduled time due after since, skipping runs missed by more than CatchUp.
func (s Store) next(w Workflow, since, now time.Time) time.Time {
	c, err := ParseCron(w.Schedule)
	if err != nil {
		return time.Time{}
	}
	n := c.Next(since)
	for !n.IsZero() && n.Add(CatchUp).Before(now) {
		n = c.Next(n)
	}
	return n
}

// Due lists the enabled workflows whose next run is at or before now.
func (s Store) Due(now time.Time) []Workflow {
	l, _ := s.List()
	mu.Lock()
	st := s.load()
	mu.Unlock()
	var out []Workflow
	for _, in := range l {
		if !in.Enabled {
			continue
		}
		if n := s.next(in.Workflow, st.Since[in.Name], now); !n.IsZero() && !n.After(now) {
			out = append(out, in.Workflow)
		}
	}
	return out
}

// Fired records that name's scheduled run started at t.
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

// Enable lets name run on its schedule from now, pinned to the file's current content.
func (s Store) Enable(name string, now time.Time) error {
	_, b, err := s.Get(name)
	if err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	st := s.load()
	st.Enabled[name], st.Since[name] = hash(b), now
	return s.save(st)
}

// Disable stops name's schedule.
func (s Store) Disable(name string) error {
	if _, err := s.file(name); err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	st := s.load()
	delete(st.Enabled, name)
	delete(st.Since, name)
	return s.save(st)
}

// Save writes w (validated). An enabled workflow saved through smithy stays enabled.
func (s Store) Save(w Workflow) error {
	if err := w.Validate(); err != nil {
		return err
	}
	f, err := s.file(w.Name)
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(w, "", "  ")
	b = append(b, '\n')
	mu.Lock()
	defer mu.Unlock()
	st := s.load()
	if old, err := os.ReadFile(f); err == nil {
		if h, ok := st.Enabled[w.Name]; ok && h == hash(old) {
			st.Enabled[w.Name] = hash(b)
		}
	}
	if err := writeAtomic(f, b); err != nil {
		return err
	}
	return s.save(st)
}

// Add copies a workflow file (a repo's, say) into the store, disabled.
func (s Store) Add(src string) (Workflow, error) {
	b, err := os.ReadFile(src)
	if err != nil {
		return Workflow{}, err
	}
	w, err := Parse(b)
	if err != nil {
		return w, err
	}
	f, _ := s.file(w.Name)
	if _, err := os.Stat(f); err == nil {
		return w, fmt.Errorf("workflow: %s already exists; disable and remove %s first", w.Name, f)
	}
	if err := writeAtomic(f, b); err != nil {
		return w, err
	}
	return w, s.Disable(w.Name)
}

// Record saves a run and drops the oldest past Keep.
func (s Store) Record(r Record) error {
	d := filepath.Join(s.Dir, "runs", r.Workflow)
	if !nameRe.MatchString(r.Workflow) {
		return fmt.Errorf("workflow: bad name %q", r.Workflow)
	}
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

// History returns name's newest runs first, at most n.
func (s Store) History(name string, n int) ([]Record, error) {
	if !nameRe.MatchString(name) {
		return nil, fmt.Errorf("workflow: no workflow named %q", name)
	}
	files, _ := filepath.Glob(filepath.Join(s.Dir, "runs", name, "*.json"))
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
		if json.Unmarshal(b, &r) == nil {
			out = append(out, r)
		}
	}
	return out, nil
}
