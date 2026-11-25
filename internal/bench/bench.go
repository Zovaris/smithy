// Package bench is smithy's own benchmark: small fixture repos whose tests are the oracle, run
// by an agent in a fresh git repo in a temp dir each, with pass rate, time, tokens and cost recorded.
package bench

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/usage"
)

//go:embed all:_tasks
var taskFS embed.FS

// Task is one benchmark task: a fixture repo, a prompt and the fixture's tests as the oracle.
type Task struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"` // bug, feature, refactor or docs
	Prompt string `json:"prompt"`
}

// Tasks lists the task set, sorted by id.
func Tasks() ([]Task, error) {
	ents, err := taskFS.ReadDir("_tasks")
	if err != nil {
		return nil, err
	}
	var ts []Task
	for _, e := range ents {
		b, err := taskFS.ReadFile(path.Join("_tasks", e.Name(), "task.json"))
		if err != nil {
			return nil, err
		}
		t := Task{ID: e.Name()}
		if err := json.Unmarshal(b, &t); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		ts = append(ts, t)
	}
	sort.Slice(ts, func(i, j int) bool { return ts[i].ID < ts[j].ID })
	return ts, nil
}

func copyTree(root, dir string, keep func(string) bool) error {
	return fs.WalkDir(taskFS, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel := strings.TrimPrefix(p, root+"/")
		if !keep(rel) {
			return nil
		}
		b, err := taskFS.ReadFile(p)
		if err != nil {
			return err
		}
		dst := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	})
}

func all(string) bool { return true }

func isTest(p string) bool { return strings.HasSuffix(p, "_test.go") }

func git(dir string, args ...string) error {
	cmd := exec.Command("git", append([]string{"-c", "user.name=smithy bench", "-c", "user.email=bench@localhost", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %s: %v: %s", args[0], err, out)
	}
	return nil
}

// Prepare writes task id's fixture into dir as a fresh git repo with one commit.
func Prepare(id, dir string) error {
	if _, err := taskFS.ReadDir(path.Join("_tasks", id, "repo")); err != nil {
		return fmt.Errorf("unknown task %q", id)
	}
	if err := copyTree(path.Join("_tasks", id, "repo"), dir, all); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module bench/"+id+"\n\ngo 1.21\n"), 0o644); err != nil {
		return err
	}
	if err := git(dir, "init", "-q"); err != nil {
		return err
	}
	if err := git(dir, "add", "-A"); err != nil {
		return err
	}
	return git(dir, "commit", "-q", "-m", "fixture "+id)
}

func applySolution(id, dir string) error {
	return copyTree(path.Join("_tasks", id, "solution"), dir, all)
}

// OracleTimeout caps one task's test run.
var OracleTimeout = 2 * time.Minute

// Oracle puts the fixture's tests back (an agent may not weaken them) and runs them in dir.
func Oracle(id, dir string) (bool, string) {
	if err := copyTree(path.Join("_tasks", id, "repo"), dir, isTest); err != nil {
		return false, err.Error()
	}
	ctx, cancel := context.WithTimeout(context.Background(), OracleTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "test", "-count=1", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=", "GOTOOLCHAIN=local")
	cmd.WaitDelay = 2 * time.Second
	defer track(cmd)()
	out, err := cmd.CombinedOutput()
	return err == nil, string(out)
}

// AgentFunc runs the agent on one task: dir is its task repo, prompt the prompt file, log the
// run's log (its usage goes to the log's usage sidecar).
type AgentFunc func(t Task, dir, prompt, log string) error

// Options are one bench run's settings.
type Options struct {
	Agent   string   // the CLI (claude, codex, …); api:<provider>/<model> once the api backend lands
	Model   string   // passed to the agent; recorded
	Skills  string   // the skills version under test; recorded
	Tasks   []string // task ids; empty means all
	Out     string   // where the result JSON goes
	Keep    bool     // keep the task repos and logs
	Timeout string   // per-task agent timeout in minutes; empty is the agent default
}

// TaskResult is one task's outcome.
type TaskResult struct {
	ID      string      `json:"id"`
	Kind    string      `json:"kind"`
	Pass    bool        `json:"pass"`
	Seconds float64     `json:"seconds"`
	Usage   usage.Usage `json:"usage"`
	Error   string      `json:"error,omitempty"`
}

// Result is one bench run, saved as <Out>/<ID>.json.
type Result struct {
	ID       string       `json:"id"`
	Agent    string       `json:"agent"`
	Model    string       `json:"model,omitempty"`
	Skills   string       `json:"skills,omitempty"`
	Started  time.Time    `json:"started"`
	Seconds  float64      `json:"seconds"`
	Passed   int          `json:"passed"`
	Total    int          `json:"total"`
	PassRate float64      `json:"pass_rate"`
	Usage    usage.Usage  `json:"usage"`
	Tasks    []TaskResult `json:"tasks"`
}

const promptTail = "\n\nThe repo's tests judge the change: run `go test ./...` to check. Do not edit the *_test.go files; they are restored before grading.\n"

func pick(ids []string) ([]Task, error) {
	ts, err := Tasks()
	if err != nil || len(ids) == 0 {
		return ts, err
	}
	by := map[string]Task{}
	for _, t := range ts {
		by[t.ID] = t
	}
	var out []Task
	for _, id := range ids {
		t, ok := by[id]
		if !ok {
			return nil, fmt.Errorf("unknown task %q (smithy bench list)", id)
		}
		out = append(out, t)
	}
	return out, nil
}

func slug(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '.' {
			return r
		}
		return '-'
	}, s)
}

// Run runs every chosen task with agent, each in a fresh git repo in a temp dir, and saves the result.
func Run(o Options, agent AgentFunc) (Result, error) {
	ts, err := pick(o.Tasks)
	if err != nil {
		return Result{}, err
	}
	start := time.Now()
	r := Result{ID: start.UTC().Format("20060102-150405.000") + "-" + slug(o.Agent), Agent: o.Agent, Model: o.Model, Skills: o.Skills, Started: start.UTC()}
	work, err := os.MkdirTemp("", "smithy-bench-")
	if err != nil {
		return r, err
	}
	if !o.Keep {
		defer os.RemoveAll(work)
	}
	defer onInterrupt(work, o.Keep)()
	for _, t := range ts {
		tr := runOne(t, filepath.Join(work, t.ID), agent)
		r.Tasks = append(r.Tasks, tr)
		r.Usage = r.Usage.Add(tr.Usage)
		if tr.Pass {
			r.Passed++
		}
	}
	r.Total = len(ts)
	if r.Total > 0 {
		r.PassRate = float64(r.Passed) / float64(r.Total)
	}
	r.Seconds = time.Since(start).Seconds()
	return r, save(o.Out, r)
}

func runOne(t Task, dir string, agent AgentFunc) TaskResult {
	tr := TaskResult{ID: t.ID, Kind: t.Kind}
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		tr.Error = err.Error()
		return tr
	}
	if err := Prepare(t.ID, repo); err != nil {
		tr.Error = err.Error()
		return tr
	}
	prompt, log := filepath.Join(dir, "prompt.md"), filepath.Join(dir, "agent.log")
	if err := os.WriteFile(prompt, []byte(t.Prompt+promptTail), 0o644); err != nil {
		tr.Error = err.Error()
		return tr
	}
	start := time.Now()
	if err := agent(t, repo, prompt, log); err != nil {
		tr.Error = err.Error()
	}
	tr.Seconds = time.Since(start).Seconds()
	tr.Usage, _ = usage.Read(log)
	tr.Pass, _ = Oracle(t.ID, repo)
	return tr
}

func save(dir string, r Result) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, r.ID+".json"), append(b, '\n'), 0o644)
}

// Load reads a saved result.
func Load(file string) (Result, error) {
	var r Result
	b, err := os.ReadFile(file)
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return r, errors.New(file + ": " + err.Error())
	}
	return r, nil
}
