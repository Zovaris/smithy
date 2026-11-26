//go:build harness

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Zovaris/smithy/internal/agents"
	"github.com/Zovaris/smithy/internal/auto"
	"github.com/Zovaris/smithy/internal/cards"
	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/ghx"
	"github.com/Zovaris/smithy/internal/gitx"
	"github.com/Zovaris/smithy/internal/text"
)

// TendRef is one PR a warden batch opened: its card names the PR, its plan item the worktree.
type TendRef struct {
	URL   string `json:"url"`
	Batch string `json:"batch"`
	Item  string `json:"item"`
	Dir   string `json:"dir"`
}

// TendPR is what the daemon knows of one watched PR. Status: green, waiting, fixing (a tend
// session is working on it), needs-fix (watch mode: a fix is due), needs-you (tend's needs-you,
// or two tend sessions ended with the same action left), closed, error.
type TendPR struct {
	TendRef
	Action  string    `json:"action"`
	Checks  string    `json:"checks"`
	Status  string    `json:"status"`
	PID     int       `json:"pid,omitempty"`
	Log     string    `json:"log,omitempty"`
	Tries   int       `json:"tries,omitempty"`
	Total   int       `json:"total,omitempty"`
	Error   string    `json:"error,omitempty"`
	Checked time.Time `json:"checked"`
}

// TendList is GET /v1/tend.
type TendList struct {
	Mode string   `json:"mode"`
	PRs  []TendPR `json:"prs"`
}

// tendTries is how many tend sessions one PR gets for the same action before it is the user's;
// tendMaxPerPR caps them across actions, so a PR flapping between two fixes stops too.
const (
	tendTries    = 2
	tendMaxPerPR = 6
)

// prURLRe is the only card value the tender acts on: it reaches gh and an agent's prompt.
var prURLRe = regexp.MustCompile(`^https://[A-Za-z0-9.-]+/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+/pull/[0-9]+$`)

func tendPRURL(u string) bool { return prURLRe.MatchString(u) }

var fixActions = []string{"fix-ci", "sync-base", "answer-review"}

// Tender keeps the PRs of warden batches green the way the tend skill does: each tick it reads
// every open batch PR's state and, in fix mode, starts one tend session (smithy agent --profile
// auto, so auto mode's allow-list and sandbox) for red CI, a conflict or open review threads.
// It never merges: merging stays with warden's merge mode or the user.
type Tender struct {
	Mode    func() string
	Sources func() []TendRef
	Fetch   func(r TendRef) (gitx.PRState, error)
	Launch  func(r TendRef, action string) (pid int, log string, err error)
	Alive   func(pid int) bool

	mu  sync.Mutex
	prs map[string]*TendPR
}

// NewTender is the daemon's tender over the real cards, gh and smithy agent.
func NewTender() *Tender {
	return &Tender{Mode: TendMode, Sources: TendSources, Fetch: fetchPRState, Launch: launchTend, Alive: auto.Alive}
}

// TendMode is tend.daemon from the user settings only (a repo must not turn on sessions that
// spend the user's agent): off, watch or fix. auto.paused holds fix to watch.
func TendMode() string {
	m, err := config.UserGet("tend.daemon", "fix")
	if err != nil || (m != "off" && m != "watch" && m != "fix") {
		return "watch"
	}
	if p, _ := config.UserGet("auto.paused", "false"); m == "fix" && p == "true" {
		return "watch"
	}
	return m
}

// Tick reads every PR once and starts the tend sessions fix mode calls for.
func (t *Tender) Tick() {
	mode := t.Mode()
	if mode == "off" {
		t.mu.Lock()
		t.prs = nil
		t.mu.Unlock()
		return
	}
	refs := t.Sources()
	keep := map[string]bool{}
	for _, r := range refs {
		keep[r.URL] = true
		st, err := t.Fetch(r)
		t.mu.Lock()
		if t.prs == nil {
			t.prs = map[string]*TendPR{}
		}
		p := t.prs[r.URL]
		if p == nil {
			p = &TendPR{}
			t.prs[r.URL] = p
		}
		p.TendRef, p.Checked, p.Error = r, time.Now().UTC(), ""
		if err != nil {
			p.Status, p.Error = "error", text.Clean(err.Error())
			t.mu.Unlock()
			continue
		}
		if st.Action != p.Action {
			p.Tries = 0
		}
		p.Action, p.Checks = st.Action, st.Checks
		busy := p.PID != 0 && t.Alive(p.PID)
		if !busy {
			p.PID = 0
		}
		launch := false
		switch {
		case st.Action == "cleanup":
			p.Status = "closed"
		case st.Action == "done":
			p.Status = "green"
		case st.Action == "wait":
			p.Status = "waiting"
		case slices.Contains(fixActions, st.Action):
			switch {
			case busy:
				p.Status = "fixing"
			case mode != "fix":
				p.Status = "needs-fix"
			case p.Tries >= tendTries || p.Total >= tendMaxPerPR:
				p.Status = "needs-you"
			default:
				launch = true
			}
		default:
			p.Status = "needs-you"
		}
		t.mu.Unlock()
		if !launch {
			continue
		}
		pid, log, lerr := t.Launch(r, st.Action)
		t.mu.Lock()
		p.Tries++
		p.Total++
		if lerr != nil {
			p.Status, p.Error = "error", text.Clean(lerr.Error())
		} else {
			p.Status, p.PID, p.Log = "fixing", pid, log
		}
		t.mu.Unlock()
	}
	t.mu.Lock()
	for u := range t.prs {
		if !keep[u] {
			delete(t.prs, u)
		}
	}
	t.mu.Unlock()
}

// List is the watched PRs, by batch, item and URL.
func (t *Tender) List() []TendPR {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]TendPR, 0, len(t.prs))
	for _, p := range t.prs {
		out = append(out, *p)
	}
	slices.SortFunc(out, func(a, b TendPR) int {
		return strings.Compare(a.Batch+"\x00"+a.Item+"\x00"+a.URL, b.Batch+"\x00"+b.Item+"\x00"+b.URL)
	})
	return out
}

// Run ticks every interval until ctx ends.
func (t *Tender) Run(ctx context.Context, every time.Duration) {
	for {
		t.Tick()
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}

// TendSources is every PR a warden card holds while the PR is open and its worker has ended
// (pr_open or ready), with the worktree its plan item names.
func TendSources() []TendRef {
	var out []TendRef
	for _, b := range cards.Batches() {
		dirs := map[string]string{}
		var plan struct {
			Items []struct {
				ID       string `json:"id"`
				Worktree string `json:"worktree"`
			} `json:"items"`
		}
		if raw, err := os.ReadFile(filepath.Join(cards.Home(), b, "batch.json")); err == nil && json.Unmarshal(raw, &plan) == nil {
			for _, it := range plan.Items {
				dirs[it.ID] = it.Worktree
			}
		}
		rows, err := cards.Rows(b)
		if err != nil {
			continue
		}
		for _, r := range rows {
			if !tendPRURL(r.PR) || (r.State != "pr_open" && r.State != "ready") || dirs[r.ID] == "" {
				continue
			}
			if fi, err := os.Stat(dirs[r.ID]); err != nil || !fi.IsDir() {
				continue
			}
			out = append(out, TendRef{URL: r.PR, Batch: b, Item: r.ID, Dir: dirs[r.ID]})
		}
	}
	return out
}

// fetchPRState runs smithy pr-state in the PR's worktree, so gh resolves its repository.
func fetchPRState(r TendRef) (gitx.PRState, error) {
	dir, url := r.Dir, r.URL
	exe, err := os.Executable()
	if err != nil {
		return gitx.PRState{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "pr-state", url)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return gitx.PRState{}, fmt.Errorf("pr-state: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return gitx.PRState{}, err
	}
	var s gitx.PRState
	if err := json.Unmarshal(out, &s); err != nil {
		return gitx.PRState{}, fmt.Errorf("pr-state: %w", err)
	}
	b, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if head, _ := s.Head.(string); err != nil || strings.TrimSpace(string(b)) != head {
		return gitx.PRState{}, fmt.Errorf("%s is not on the PR's head branch %v; tend leaves it alone", dir, s.Head)
	}
	return s, nil
}

// TendPrompt is the one tend session's task for action on r.
func TendPrompt(r TendRef, action string) string {
	skill := "the tend skill's SKILL.md"
	if p, ok := agents.FindSkill("tend"); ok {
		skill = p
	}
	return fmt.Sprintf("Read %s and follow it for `tend %s`, once: its next step is %s. Work in this worktree on the PR's branch, push the fix and end when the step is done. Never merge, never force-push someone else's branch and never close the PR; anything that needs the user ends in one line starting with `needs-you:`.\n", skill, r.URL, action)
}

// launchTend starts one detached tend session in r's worktree with auto mode's profile.
func launchTend(r TendRef, action string) (int, string, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, "", err
	}
	agent := auto.DefaultAgent()
	mode, err := agents.SandboxMode()
	if err == nil {
		err = agents.SandboxCheckAgent(mode, agent, agents.HostProbe())
	}
	if err != nil {
		return 0, "", fmt.Errorf("auto.sandbox: %v; no tend session starts until it is fixed (smithy doctor)", err)
	}
	dir := filepath.Join(ghx.Home(), "tend", "daemon")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return 0, "", err
	}
	ts := time.Now().Format("20060102-150405") + "-" + strings.NewReplacer("/", "-", " ", "-").Replace(r.Batch+"-"+r.Item)
	prompt := filepath.Join(dir, ts+".prompt.md")
	log := filepath.Join(dir, ts+".log")
	if err := os.WriteFile(prompt, []byte(TendPrompt(r, action)), 0o600); err != nil {
		return 0, "", err
	}
	cmd := exec.Command(exe, "agent", "--bg", "--profile", "auto", "--cwd", r.Dir, "--log", log, prompt)
	cmd.Dir = r.Dir
	env := agents.Scrub(os.Environ())
	if agent != "" && agent != "custom" {
		env = append(env, "AGENT_CLI="+agent)
	}
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return 0, "", fmt.Errorf("%v: %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return 0, "", err
	}
	f := strings.SplitN(strings.TrimSpace(string(out)), " ", 2)
	pid, perr := strconv.Atoi(f[0])
	if len(f) < 2 || perr != nil {
		return 0, "", fmt.Errorf("smithy agent printed %q, not <pid> <log>", strings.TrimSpace(string(out)))
	}
	return pid, f[1], nil
}

// tendView is GET /v1/tend.
func (sv *Server) tendView(w http.ResponseWriter, r *http.Request) {
	out := TendList{Mode: "off", PRs: []TendPR{}}
	if sv.Tend != nil {
		out.Mode, out.PRs = sv.Tend.Mode(), sv.Tend.List()
	}
	writeJSON(w, 200, out)
}

// Tend is GET /v1/tend: the PRs the daemon tends and what it is doing on each.
func (c *Client) Tend(ctx context.Context) (TendList, error) {
	var out TendList
	err := c.call(ctx, "GET", "/v1/tend", nil, &out)
	return out, err
}
