//go:build harness

package daemon

import (
	"context"
	"errors"
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
