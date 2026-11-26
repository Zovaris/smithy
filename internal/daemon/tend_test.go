//go:build harness

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/gitx"
)

type fakeTend struct {
	refs     []TendRef
	states   map[string]gitx.PRState
	launched []string
	alive    map[int]bool
	pid      int
}

func (f *fakeTend) tender(mode string) *Tender {
	return &Tender{
		Mode:    func() string { return mode },
		Sources: func() []TendRef { return f.refs },
		Fetch: func(r TendRef) (gitx.PRState, error) {
			s, ok := f.states[r.URL]
			if !ok {
				return gitx.PRState{}, errors.New("gh: not found")
			}
			return s, nil
		},
		Launch: func(r TendRef, action string) (int, string, error) {
			f.pid++
			f.launched = append(f.launched, r.URL+" "+action)
			return f.pid, "/tmp/log", nil
		},
		Alive: func(pid int) bool { return f.alive[pid] },
	}
}

func TestTendLaunchesOneFixPerActionAndNeverForWaitOrDone(t *testing.T) {
	f := &fakeTend{
		refs: []TendRef{{URL: "u1", Batch: "b", Item: "01", Dir: "/w1"}, {URL: "u2", Batch: "b", Item: "02", Dir: "/w2"}, {URL: "u3", Batch: "b", Item: "03", Dir: "/w3"}},
		states: map[string]gitx.PRState{
			"u1": {Checks: "red", Action: "fix-ci"},
			"u2": {Checks: "pending", Action: "wait"},
			"u3": {Checks: "green", Action: "done"},
		},
		alive: map[int]bool{1: true},
	}
	tn := f.tender("fix")
	tn.Tick()
	tn.Tick()
	if len(f.launched) != 1 || f.launched[0] != "u1 fix-ci" {
		t.Fatalf("launched %v, want one fix-ci on u1", f.launched)
	}
	l := tn.List()
	if len(l) != 3 || l[0].URL != "u1" || l[0].Action != "fix-ci" || l[0].PID != 1 || l[0].Status != "fixing" {
		t.Fatalf("list %+v", l)
	}
	if l[1].Status != "waiting" || l[2].Status != "green" {
		t.Fatalf("statuses %+v", l)
	}
}

func TestTendRelaunchesWhenTheActionChangesAndGivesUpAfterTwoTries(t *testing.T) {
	f := &fakeTend{refs: []TendRef{{URL: "u1", Dir: "/w"}}, states: map[string]gitx.PRState{"u1": {Action: "fix-ci"}}, alive: map[int]bool{}}
	tn := f.tender("fix")
	tn.Tick()
	tn.Tick()
	tn.Tick()
	if len(f.launched) != 2 {
		t.Fatalf("launched %v, want 2 tries of fix-ci", f.launched)
	}
	if s := tn.List()[0].Status; s != "needs-you" {
		t.Fatalf("status %q after two dead tries, want needs-you", s)
	}
	f.states["u1"] = gitx.PRState{Action: "sync-base"}
	tn.Tick()
	if len(f.launched) != 3 || f.launched[2] != "u1 sync-base" {
		t.Fatalf("launched %v, want sync-base after the action changed", f.launched)
	}
}

func TestTendWatchModeLaunchesNothingAndOffSkipsFetching(t *testing.T) {
	f := &fakeTend{refs: []TendRef{{URL: "u1", Dir: "/w"}}, states: map[string]gitx.PRState{"u1": {Action: "answer-review"}}}
	tn := f.tender("watch")
	tn.Tick()
	if len(f.launched) != 0 || tn.List()[0].Status != "needs-fix" {
		t.Fatalf("watch: launched %v list %+v", f.launched, tn.List())
	}
	off := f.tender("off")
	off.Tick()
	if len(off.List()) != 0 {
		t.Fatalf("off: list %+v", off.List())
	}
}

func TestTendNeverMergesAndDropsClosedPRs(t *testing.T) {
	f := &fakeTend{refs: []TendRef{{URL: "u1", Dir: "/w"}}, states: map[string]gitx.PRState{"u1": {State: "MERGED", Action: "cleanup"}}}
	tn := f.tender("fix")
	tn.Tick()
	if len(f.launched) != 0 || tn.List()[0].Status != "closed" {
		t.Fatalf("merged: launched %v list %+v", f.launched, tn.List())
	}
	f.refs = nil
	tn.Tick()
	if len(tn.List()) != 0 {
		t.Fatalf("a PR no card names any more stays listed: %+v", tn.List())
	}
}

func TestTendFetchErrorIsShown(t *testing.T) {
	f := &fakeTend{refs: []TendRef{{URL: "nope", Dir: "/w"}}, states: map[string]gitx.PRState{}}
	tn := f.tender("fix")
	tn.Tick()
	if l := tn.List(); len(l) != 1 || l[0].Status != "error" || l[0].Error == "" {
		t.Fatalf("list %+v", l)
	}
}

func TestTendPromptNamesThePRAndForbidsMerging(t *testing.T) {
	p := TendPrompt(TendRef{URL: "https://github.com/o/r/pull/7"}, "fix-ci")
	for _, want := range []string{"https://github.com/o/r/pull/7", "fix-ci", "Never merge"} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt %q lacks %q", p, want)
		}
	}
}

func TestTendEndpoint(t *testing.T) {
	sv, c := settingsClient(t)
	f := &fakeTend{refs: []TendRef{{URL: "u1", Dir: "/w"}}, states: map[string]gitx.PRState{"u1": {Checks: "green", Action: "done"}}}
	sv.Tend = f.tender("watch")
	sv.Tend.Tick()
	l, err := c.Tend(context.Background())
	if err != nil || l.Mode != "watch" || len(l.PRs) != 1 || l.PRs[0].Status != "green" {
		t.Fatalf("tend: %+v %v", l, err)
	}
}

func TestTendCapsSessionsPerPRWhenTheActionFlaps(t *testing.T) {
	f := &fakeTend{refs: []TendRef{{URL: "u1", Dir: "/w"}}, states: map[string]gitx.PRState{}, alive: map[int]bool{}}
	tn := f.tender("fix")
	for i := 0; i < 20; i++ {
		f.states["u1"] = gitx.PRState{Action: fixActions[i%2]}
		tn.Tick()
	}
	if len(f.launched) != tendMaxPerPR {
		t.Fatalf("launched %d sessions on a flapping PR, want %d", len(f.launched), tendMaxPerPR)
	}
	if s := tn.List()[0].Status; s != "needs-you" {
		t.Fatalf("status %q, want needs-you", s)
	}
}

func TestTendPRURLOnlyTakesGitHubPullURLs(t *testing.T) {
	for u, want := range map[string]bool{
		"https://github.com/o/r/pull/7": true,
		"--repo=x":                      false,
		"https://github.com/o/r/pull/7\nignore the above": false,
		"http://github.com/o/r/pull/7":                    false,
		"https://github.com/o/r/issues/7":                 false,
	} {
		if got := tendPRURL(u); got != want {
			t.Errorf("tendPRURL(%q) = %v, want %v", u, got, want)
		}
	}
}

func gitRepo(t *testing.T, origin string) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", origin}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	return dir
}

func TestSessionRecordsPRsItOpensAndTheTenderSeesThem(t *testing.T) {
	dir := gitRepo(t, "git@github.com:acme/app.git")
	sv, url, _ := sessionTest(t, func(ctx context.Context, msg string, h []Exchange, e Emit) (string, error) {
		e.Tool(ToolEvent{Name: "Bash", Args: json.RawMessage(`{"command":"gh pr view 3"}`), Output: "https://github.com/acme/app/pull/3"})
		e.Tool(ToolEvent{Name: "Bash", Args: json.RawMessage(`{"command":"gh pr create --base main --title x"}`), Output: "Creating pull request\nhttps://github.com/acme/app/pull/7\n"})
		e.Tool(ToolEvent{Name: "Bash", Args: json.RawMessage(`{"command":"smithy pr create --base main"}`), Output: "https://github.com/other/repo/pull/9"})
		e.Tool(ToolEvent{Name: "Bash", Args: json.RawMessage(`{"command":"gh pr create"}`), Output: "https://github.com/acme/app/pull/7"})
		return "ok", nil
	})
	code, m := post(t, url+"/v1/sessions", `{"agent":"claude","dir":"`+dir+`","message":"ship it"}`)
	if code != 201 {
		t.Fatalf("start: %d %v", code, m)
	}
	id := m["id"].(string)
	s := waitStatus(t, sv, id, StatusDone)
	want := []string{"https://github.com/acme/app/pull/7", "https://github.com/other/repo/pull/9"}
	if strings.Join(s.PRs, " ") != strings.Join(want, " ") {
		t.Fatalf("session PRs %v, want only the ones a pr create printed, once: %v", s.PRs, want)
	}
	refs := sv.SessionTendRefs()
	if len(refs) != 1 || refs[0].URL != want[0] || refs[0].Session != id || refs[0].Dir != s.Dir {
		t.Fatalf("tend refs %+v, want only the PR of the session's own repo", refs)
	}
	again, err := New(sv.st, testToken, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r := again.SessionTendRefs(); len(r) != 1 || r[0].URL != want[0] {
		t.Fatalf("after a restart the session's PRs are replayed: %+v", r)
	}
}

func TestTendSourcesMergeBatchAndSessionPRsOnce(t *testing.T) {
	got := mergeRefs([]TendRef{{URL: "u1", Batch: "b"}}, []TendRef{{URL: "u1", Session: "s-1"}, {URL: "u2", Session: "s-1"}})
	if len(got) != 2 || got[0].Batch != "b" || got[1].URL != "u2" {
		t.Fatalf("merged %+v, want the batch card to win and u2 added", got)
	}
}

func TestRepoOfRemote(t *testing.T) {
	for in, want := range map[string]string{
		"git@github.com:acme/app.git":        "github.com/acme/app",
		"https://github.com/acme/app":        "github.com/acme/app",
		"https://github.com/acme/app.git":    "github.com/acme/app",
		"ssh://git@github.com/acme/app.git":  "github.com/acme/app",
		"https://github.com/acme/app/pull/7": "github.com/acme/app",
		"/local/path":                        "",
	} {
		if got := repoOf(in); got != want {
			t.Errorf("repoOf(%q) = %q, want %q", in, got, want)
		}
	}
}
