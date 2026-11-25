package bench

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/usage"
)

func TestTaskSetCoversEveryKind(t *testing.T) {
	ts, err := Tasks()
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) < 20 {
		t.Fatalf("%d tasks, want at least 20", len(ts))
	}
	kinds := map[string]int{}
	for _, x := range ts {
		if x.Prompt == "" {
			t.Errorf("%s: empty prompt", x.ID)
		}
		kinds[x.Kind]++
	}
	for _, k := range []string{"bug", "feature", "refactor", "docs"} {
		if kinds[k] < 3 {
			t.Errorf("kind %s has %d tasks, want 3 or more", k, kinds[k])
		}
	}
}

// Every oracle fails on the fixture as given and passes with the reference solution, so a task
// is reproducible and its tests really judge the change.
func TestEveryOracleFailsThenPassesWithSolution(t *testing.T) {
	ts, _ := Tasks()
	for _, x := range ts {
		t.Run(x.ID, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := Prepare(x.ID, dir); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
				t.Fatal("not a git repo")
			}
			if ok, _ := Oracle(x.ID, dir); ok {
				t.Fatal("oracle passes before any change")
			}
			if err := applySolution(x.ID, dir); err != nil {
				t.Fatal(err)
			}
			if ok, out := Oracle(x.ID, dir); !ok {
				t.Fatalf("oracle fails with the solution:\n%s", out)
			}
		})
	}
}

func TestOracleRestoresTheTests(t *testing.T) {
	dir := t.TempDir()
	if err := Prepare("bug-sum-last", dir); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "sum_test.go"), []byte("package p\n"), 0o644)
	if ok, _ := Oracle("bug-sum-last", dir); ok {
		t.Fatal("an agent that empties the tests passes")
	}
}

func TestRunRecordsPassRateTimeTokensAndCost(t *testing.T) {
	out := t.TempDir()
	cost := 0.25
	var dirs []string
	agent := func(x Task, dir, prompt, log string) error {
		dirs = append(dirs, dir)
		if b, _ := os.ReadFile(prompt); !strings.Contains(string(b), x.Prompt) {
			t.Errorf("%s: prompt file lacks the task prompt", x.ID)
		}
		usage.Write(log, usage.Usage{Input: 100, Output: 10, CostUSD: &cost})
		if x.ID == "bug-sum-last" {
			return applySolution(x.ID, dir)
		}
		return nil
	}
	r, err := Run(Options{Agent: "claude", Model: "m", Skills: "v0.22.0", Tasks: []string{"bug-sum-last", "feat-clamp"}, Out: out}, agent)
	if err != nil {
		t.Fatal(err)
	}
	if r.Passed != 1 || r.Total != 2 || r.PassRate != 0.5 {
		t.Fatalf("passed %d/%d rate %v", r.Passed, r.Total, r.PassRate)
	}
	if r.Usage.Input != 200 || r.Usage.CostUSD == nil || *r.Usage.CostUSD != 0.5 {
		t.Fatalf("usage %+v", r.Usage)
	}
	if len(dirs) != 2 || dirs[0] == dirs[1] {
		t.Fatalf("tasks did not get their own worktree: %v", dirs)
	}
	if r.Agent != "claude" || r.Model != "m" || r.Skills != "v0.22.0" || r.Tasks[0].Seconds < 0 {
		t.Fatalf("result %+v", r)
	}
	saved, err := Load(filepath.Join(out, r.ID+".json"))
	if err != nil || saved.Passed != 1 || len(saved.Tasks) != 2 {
		t.Fatalf("saved %+v %v", saved, err)
	}
}

func TestRunRefusesUnknownTaskAndAPIAgent(t *testing.T) {
	nop := func(Task, string, string, string) error { return nil }
	if _, err := Run(Options{Agent: "claude", Tasks: []string{"nope"}, Out: t.TempDir()}, nop); err == nil {
		t.Fatal("unknown task accepted")
	}
	if code := Main([]string{"run", "--agent", "api:anthropic/x"}); code != 2 {
		t.Fatalf("api agent exit %d, want 2", code)
	}
	if code := Main([]string{"run", "--agent", "nosuchcli"}); code != 2 {
		t.Fatalf("unknown cli exit %d, want 2", code)
	}
	if code := Main([]string{"run"}); code != 2 {
		t.Fatalf("no --agent exit %d, want 2", code)
	}
}

func TestMainListsTasks(t *testing.T) {
	if code := Main([]string{"list"}); code != 0 {
		t.Fatalf("list exit %d", code)
	}
}

func TestMain(m *testing.M) {
	if _, err := exec.LookPath("go"); err != nil {
		os.Exit(0)
	}
	os.Exit(m.Run())
}
