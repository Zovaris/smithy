// Package warden holds the warden-only commands: wait-turn (a launched worker waits for its
// prereqs and a free slot) and doctor (checks everything a batch needs).
package warden

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/agents"
	"github.com/Zovaris/smithy/internal/cards"
	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/ghx"
	"github.com/Zovaris/smithy/internal/harness"
)

type plan struct {
	Max   *int `json:"max"`
	Stack bool `json:"stack"`
	Items []struct {
		ID      string   `json:"id"`
		Prereqs []string `json:"prereqs"`
	} `json:"items"`
}

func readPlan(dir string) (plan, error) {
	var p plan
	b, err := os.ReadFile(filepath.Join(dir, "batch.json"))
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal(b, &p); err != nil {
		var te *json.UnmarshalTypeError
		if errors.As(err, &te) && te.Field == "stack" {
			return p, errors.New("batch.json unreadable: stack must be true or false")
		}
		return p, fmt.Errorf("batch.json unreadable: %v", err)
	}
	return p, nil
}

// planErr is the message for a plan readPlan could not use.
func planErr(batch string, err error) string {
	if os.IsNotExist(err) {
		return "no batch " + batch
	}
	return err.Error()
}

func cardState(dir, id string) string {
	var c struct{ State string }
	if b, err := os.ReadFile(filepath.Join(dir, id+".json")); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c.State
}

const waitUsage = `usage: smithy wait-turn <batch> <id> [--max <min>]   (default 9; a bare third argument is the same)
A launched worker whose item can't start yet waits here, inside its own session, until its
prereqs are merged (or pr_open/ready with stack) and a slot is free under the batch's max.
Then it claims the slot (state=working) and prints "go <id>".
--max caps one call in minutes; a subagent uses --max 1 so each call fits a harness's
foreground limit.
Exit 0 = go; 4 = still waiting after the cap (call it again: one blocking call per turn); 5 = a prereq was dropped (card set blocked); 2 = usage or
unknown item.`

// waitStep is how often wait-turn rechecks; the last sleep is cut to the cap.
var waitStep = 20 * time.Second

// WaitTurn runs `smithy wait-turn`.
func WaitTurn(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, waitUsage)
		return 2
	}
	maxMin := 9.0
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if v, ok := strings.CutPrefix(a, "--max="); ok {
			a = v
		} else if a == "--max" && i+1 < len(args) {
			i++
			a = args[i]
		} else if len(pos) < 2 {
			pos = append(pos, a)
			continue
		}
		v, err := strconv.ParseFloat(a, 64)
		if err != nil || v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			fmt.Fprintln(os.Stderr, waitUsage)
			return 2
		}
		maxMin = v
	}
	if len(pos) < 2 {
		fmt.Fprintln(os.Stderr, waitUsage)
		return 2
	}
	batch, id := pos[0], pos[1]
	dir := filepath.Join(cards.Home(), batch)
	p, err := readPlan(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, planErr(batch, err))
		return 2
	}
	var prereqs []string
	found := false
	for _, it := range p.Items {
		if it.ID == id {
			found, prereqs = true, it.Prereqs
		}
	}
	if !found {
		fmt.Fprintf(os.Stderr, "no item %s\n", id)
		return 2
	}
	set := func(pairs ...string) {
		if _, err := cards.Set(batch, id, pairs); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}
	set("state=waiting", "phase=waiting")
	end := time.Now().Add(time.Duration(maxMin * float64(time.Minute)))
	lock := filepath.Join(dir, ".slot.lock")
	for {
		if p, err = readPlan(dir); err != nil {
			fmt.Fprintln(os.Stderr, planErr(batch, err))
			return 2
		}
		max := 5
		if p.Max != nil {
			max = *p.Max
		}
		ready := true
		for _, pr := range prereqs {
			st := cardState(dir, pr)
			if st == "dropped" {
				set("state=blocked", "note=prereq "+pr+" was dropped")
				return 5
			}
			if !(st == "merged" || p.Stack && (st == "pr_open" || st == "ready")) {
				ready = false
			}
		}
		if ready && os.Mkdir(lock, 0o755) == nil {
			working := 0
			entries, _ := os.ReadDir(dir)
			for _, e := range entries {
				n := e.Name()
				if strings.HasSuffix(n, ".json") && n != "batch.json" && cardState(dir, strings.TrimSuffix(n, ".json")) == "working" {
					working++
				}
			}
			if working < max {
				set("state=working", "phase=start")
				_ = os.Remove(lock)
				fmt.Println("go", id)
				return 0
			}
			_ = os.Remove(lock)
		}
		left := time.Until(end)
		if left <= 0 {
			break
		}
		time.Sleep(min(waitStep, left))
	}
	fmt.Println("waiting", id)
	return 4
}

type report struct {
	quiet, failed bool
}

func (r *report) ok(msg string) {
	if !r.quiet {
		fmt.Printf("  ok    %s\n", msg)
	}
}
func (r *report) warn(msg, fix string) { fmt.Printf("  warn  %s\n        fix: %s\n", msg, fix) }
func (r *report) bad(msg, fix string) {
	fmt.Printf("  FAIL  %s\n        fix: %s\n", msg, fix)
	r.failed = true
}

func have(name string) bool { _, err := exec.LookPath(name); return err == nil }

type claudeSettings struct {
	Permissions struct {
		Allow []string `json:"allow"`
	} `json:"permissions"`
}

func claudeFiles() []string {
	home, _ := os.UserHomeDir()
	return []string{filepath.Join(home, ".claude", "settings.json"), filepath.Join(".claude", "settings.json"), filepath.Join(".claude", "settings.local.json")}
}

func readSettings(f string) (claudeSettings, bool) {
	var s claudeSettings
	b, err := os.ReadFile(f)
	if err != nil {
		return s, false
	}
	return s, json.Unmarshal(b, &s) == nil
}

var findSkill = agents.FindSkill

var missingInHarness = agents.MissingInHarness

func githubMode(r *report) {
	if ok, origin := ghx.OriginIsGitHub(); !ok && origin != "" {
		r.warn("origin is not a GitHub remote ("+origin+"): ship items stop at a pushed branch, no PR", "point origin at GitHub, or run items as scout/branch-only")
	}
	switch ghx.Mode() {
	case "gh":
		r.ok("gh found")
		if exec.Command("gh", "auth", "status").Run() == nil {
			r.ok("gh signed in")
			r.ok("github: gh (the GitHub CLI)")
		} else {
			r.bad("gh not signed in", "gh auth login (or set GH_TOKEN)")
		}
	case "token":
		r.ok("github: token (" + ghx.TokenVar() + ", the GitHub API; gh not used)")
	default:
		r.bad("github: none (no gh, no GH_TOKEN): can't open, merge, verify or watch PRs; smithy pr create only prints a compare URL",
			"install gh (brew install gh) and run gh auth login, or set GH_TOKEN")
	}
}

// Doctor runs `smithy doctor [--quiet]`: one line per check, exit 1 when something failed.
func Doctor(args []string) int {
	r := &report{quiet: len(args) > 0 && args[0] == "--quiet"}
	if !r.quiet {
		fmt.Println("warden doctor")
	}
	if have("git") {
		r.ok("git found")
	} else {
		r.bad("git missing", "install git (brew install git)")
	}
	githubMode(r)
	h, adapter := harness.Detect()
	r.ok(fmt.Sprintf("harness: %s (adapter: %s; override with WARDEN_HARNESS)", h, adapter))
	cli := ""
	if os.Getenv("AGENT_CMD") != "" {
		cli = "AGENT_CMD"
	} else {
		for _, c := range []string{"claude", "codex", "opencode", "gemini", "agent", "pi", "dsh"} {
			if have(c) {
				cli = c
				break
			}
		}
	}
	switch {
	case adapter == "desktop":
	case cli != "":
		r.ok("agent CLI: " + cli)
	case adapter == "any":
		r.bad("no headless agent CLI on PATH (needed to spawn workers here)", "install one (claude, codex, opencode, gemini, cursor agent, pi, dsh) or set AGENT_CMD")
	default:
		r.warn("no headless agent CLI on PATH (only consult needs it here)", "optional: install one or set AGENT_CMD")
	}
	if (adapter == "any" && cli == "") || (adapter != "any" && !have("claude")) {
		why := "no agent CLI to spawn them"
		if adapter == "desktop" {
			why = "no claude CLI; start_session if it loads"
		}
		r.warn("workers: subagent fallback; they end with this session ("+why+")",
			"say it in the plan line; install a headless agent CLI for workers that outlive this session")
	}
	for _, s := range []string{"smelt", "hush", "scout", "tend", "use-smithy"} {
		if p, ok := findSkill(s); ok {
			r.ok(s + " skill: " + p)
		} else {
			r.warn(s+" skill not installed", "npx skills add Zovaris/smithy -g --skill "+s)
		}
	}
	if missing, dir := missingInHarness(h); len(missing) > 0 {
		r.warn(h+" can't load "+strings.Join(missing, ", ")+" (installed elsewhere, not in its skills folder)",
			"link each into "+dir+" (ln -s <skill-dir> "+dir+"/<name>), or npx skills add Zovaris/smithy -g --skill <name>")
	}
	for _, m := range []string{"cheap", "strong"} {
		if v, err := config.Model(m); err == nil {
			r.ok("model " + m + ": " + v)
		} else {
			r.warn("no "+m+" model set for "+h, "ask the user, then: smithy config set-model "+m+" <name>")
		}
	}
	if strings.HasPrefix(h, "claude") {
		allowed := ""
		for _, f := range claudeFiles() {
			if s, ok := readSettings(f); ok {
				for _, a := range s.Permissions.Allow {
					if strings.HasPrefix(a, "Bash(gh pr merge") {
						allowed = f
					}
				}
			}
			if allowed != "" {
				break
			}
		}
		if allowed != "" {
			r.ok("gh pr merge allowed (" + allowed + ")")
		} else {
			r.warn("gh pr merge not in the allow list (auto merges may stop at ready)", `add "Bash(gh pr merge:*)" to permissions.allow in ~/.claude/settings.json, or use merge manual`)
		}
	}
	d := cards.Home()
	if os.MkdirAll(d, 0o755) == nil && writable(d) {
		r.ok("writable " + d)
	} else {
		r.bad("cannot write "+d, "check permissions on "+d)
	}
	dir := os.Getenv("WARDEN_WORKSPACE")
	if dir == "" {
		dir, _ = os.Getwd()
	}
	found := ""
	for found == "" {
		for _, n := range []string{"AGENTS.md", "CLAUDE.md"} {
			if _, err := os.Stat(filepath.Join(dir, n)); err == nil {
				found = filepath.Join(dir, n)
				break
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	if found != "" {
		r.ok("agent instructions for workers: " + found)
	} else {
		r.warn("no AGENTS.md / CLAUDE.md here or in a parent folder", "optional: workers follow smelt and the brief")
	}
	if r.failed {
		fmt.Println("not ready")
		return 1
	}
	fmt.Println("ready")
	return 0
}

func writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".probe")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	return os.Remove(name) == nil
}
