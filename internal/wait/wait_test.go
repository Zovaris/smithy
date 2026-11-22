package wait

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSummarize(t *testing.T) {
	if s := summarize(nil); s.state != "none" {
		t.Errorf("no checks: %v", s)
	}
	if s := summarize([]check{{Name: "unit", Status: "IN_PROGRESS"}}); s.state != "pending" {
		t.Errorf("in progress: %v", s)
	}
	s := summarize([]check{{Name: "unit tests", Status: "COMPLETED", Conclusion: "FAILURE"}, {Name: "lint", Status: "COMPLETED", Conclusion: "SUCCESS"}})
	if s.state != "red" || len(s.failed) != 1 || s.failed[0] != "unit-tests" {
		t.Errorf("red: %v", s)
	}
	if s := summarize([]check{{Context: "ci/legacy", State: "SUCCESS"}}); s.state != "green" {
		t.Errorf("status context: %v", s)
	}
}

const fakeGH = `#!/bin/sh
D="$FAKE_GH"
if [ -e "$D/fail-$1-$2" ]; then echo boom >&2; exit 1; fi
case "$1 $2" in
  "auth status") [ -e "$D/signedout" ] && exit 1; exit 0 ;;
  "pr view") cat "$D/pr.json" ;;
  "run view") cat "$D/run.json" ;;
  "run list") cat "$D/runs.json" ;;
  "pr list") cat "$D/prs.json" ;;
  "issue list") cat "$D/issues.json" ;;
  "repo view") echo o/r ;;
  "api user") echo me ;;
esac
`

type env struct{ dir string }

func (e env) put(t *testing.T, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(e.dir, name), []byte(body), 0o644); err != nil {
		t.Error(err)
	}
}

func (e env) rm(name string) { _ = os.Remove(filepath.Join(e.dir, name)) }

func sandbox(t *testing.T) env {
	t.Helper()
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	data := filepath.Join(root, "data")
	for _, d := range []string{bin, data, filepath.Join(root, "work")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(fakeGH), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(filepath.Join(root, "work"))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_GH", data)
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("SMITHY_HOME", filepath.Join(root, "smithy"))
	t.Setenv("SMITHY_CONFIG", filepath.Join(root, "config.json"))
	t.Setenv("WARDEN_HOME", filepath.Join(root, "warden"))
	t.Setenv("TEND_HOME", filepath.Join(root, "tend"))
	e := env{data}
	e.put(t, "prs.json", "[]")
	e.put(t, "issues.json", "[]")
	e.put(t, "runs.json", "[]")
	return e
}

func capture(t *testing.T, f func() int) (string, string, int) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	ro, wo, _ := os.Pipe()
	re, we, _ := os.Pipe()
	os.Stdout, os.Stderr = wo, we
	code := f()
	os.Stdout, os.Stderr = oldOut, oldErr
	wo.Close()
	we.Close()
	o, _ := io.ReadAll(ro)
	e, _ := io.ReadAll(re)
	return strings.TrimSpace(string(o)), strings.TrimSpace(string(e)), code
}

func mainOut(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	return capture(t, func() int { return Main(args) })
}

func allOpts() opts {
	o := opts{events: map[string]bool{}}
	for _, e := range allEvents {
		o.events[e] = true
	}
	return o
}

func only(events ...string) opts {
	o := opts{events: map[string]bool{}}
	for _, e := range events {
		o.events[e] = true
	}
	return o
}

type prFixture struct {
	state, mergeable, review, head, rollup string
	comments, reviews                      int
}

func (p prFixture) json() string {
	if p.state == "" {
		p.state = "OPEN"
	}
	if p.mergeable == "" {
		p.mergeable = "MERGEABLE"
	}
	if p.head == "" {
		p.head = "abc1234def"
	}
	if p.rollup == "" {
		p.rollup = "[]"
	}
	rep := func(n int) string { return "[" + strings.TrimSuffix(strings.Repeat("{},", n), ",") + "]" }
	return fmt.Sprintf(`{"number":5,"url":"u5","state":%q,"mergeable":%q,"reviewDecision":%q,"headRefOid":%q,"statusCheckRollup":%s,"comments":%s,"reviews":%s}`,
		p.state, p.mergeable, p.review, p.head, p.rollup, rep(p.comments), rep(p.reviews))
}

const (
	green   = `[{"name":"unit","status":"COMPLETED","conclusion":"SUCCESS"}]`
	red     = `[{"name":"unit tests","status":"COMPLETED","conclusion":"FAILURE"},{"name":"lint","status":"COMPLETED","conclusion":"SUCCESS"}]`
	pending = `[{"name":"unit","status":"IN_PROGRESS"}]`
)

func TestPRWatcherFirstPoll(t *testing.T) {
	cases := []struct {
		name string
		pr   prFixture
		o    opts
		want []string
	}{
		{"green", prFixture{rollup: green}, allOpts(), []string{"pr 5 checks=green u5"}},
		{"red", prFixture{rollup: red}, allOpts(), []string{"pr 5 checks=red failed=unit-tests u5"}},
		{"pending is quiet", prFixture{rollup: pending}, allOpts(), nil},
		{"no checks is quiet", prFixture{}, allOpts(), nil},
		{"merged", prFixture{state: "MERGED", rollup: green}, allOpts(), []string{"pr 5 merged u5"}},
		{"closed", prFixture{state: "CLOSED"}, allOpts(), []string{"pr 5 closed u5"}},
		{"merged filtered out", prFixture{state: "MERGED"}, only("checks"), nil},
		{"closed filtered out", prFixture{state: "CLOSED"}, only("merged"), nil},
		{"checks filtered out", prFixture{rollup: green}, only("merged"), nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := sandbox(t)
			e.put(t, "pr.json", c.pr.json())
			got, err := newPR("5", c.o).poll()
			if err != nil || !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %v err %v want %v", got, err, c.want)
			}
		})
	}
}

func TestPRWatcherLaterPolls(t *testing.T) {
	base := prFixture{rollup: pending, comments: 1, reviews: 1}
	cases := []struct {
		name string
		next prFixture
		o    opts
		want []string
	}{
		{"merged", prFixture{state: "MERGED", rollup: pending, comments: 1, reviews: 1}, allOpts(), []string{"pr 5 merged u5"}},
		{"closed", prFixture{state: "CLOSED", rollup: pending, comments: 1, reviews: 1}, allOpts(), []string{"pr 5 closed u5"}},
		{"push", prFixture{head: "fff9999000", rollup: pending, comments: 1, reviews: 1}, allOpts(), []string{"pr 5 push head=fff9999 u5"}},
		{"checks go green", prFixture{rollup: green, comments: 1, reviews: 1}, allOpts(), []string{"pr 5 checks=green u5"}},
		{"checks go red", prFixture{rollup: red, comments: 1, reviews: 1}, allOpts(), []string{"pr 5 checks=red failed=unit-tests u5"}},
		{"conflict", prFixture{mergeable: "CONFLICTING", rollup: pending, comments: 1, reviews: 1}, allOpts(), []string{"pr 5 conflict u5"}},
		{"review decision", prFixture{review: "APPROVED", rollup: pending, comments: 1, reviews: 1}, allOpts(), []string{"pr 5 review=approved u5"}},
		{"review without decision", prFixture{rollup: pending, comments: 1, reviews: 2}, allOpts(), []string{"pr 5 review=commented u5"}},
		{"comments", prFixture{rollup: pending, comments: 4, reviews: 1}, allOpts(), []string{"pr 5 comment +3 u5"}},
		{"no change", base, allOpts(), nil},
		{"filtered", prFixture{state: "MERGED", head: "x", mergeable: "CONFLICTING", review: "APPROVED", rollup: green, comments: 9, reviews: 9}, only("new"), nil},
		{"only comment", prFixture{state: "MERGED", head: "abc1234def", rollup: pending, comments: 2, reviews: 1}, only("comment"), []string{"pr 5 comment +1 u5"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := sandbox(t)
			e.put(t, "pr.json", base.json())
			w := newPR("5", c.o)
			if got, err := w.poll(); err != nil || len(got) != 0 {
				t.Fatalf("first poll: %v %v", got, err)
			}
			e.put(t, "pr.json", c.next.json())
			got, err := w.poll()
			if err != nil || !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %v err %v want %v", got, err, c.want)
			}
		})
	}
}

func TestPRWatcherError(t *testing.T) {
	e := sandbox(t)
	e.put(t, "fail-pr-view", "")
	if _, err := newPR("https://github.com/o/r/pull/5", allOpts()).poll(); err == nil {
		t.Error("want error")
	}
}

func TestPRArgs(t *testing.T) {
	if w := newPR("https://github.com/o/r/pull/12", allOpts()); w.ref != "https://github.com/o/r/pull/12" {
		t.Errorf("ref %v", w.ref)
	}
}

func TestSummarizeMore(t *testing.T) {
	cases := []struct {
		name   string
		in     []check
		state  string
		failed []string
	}{
		{"expected context", []check{{Context: "ci", State: "EXPECTED"}}, "pending", nil},
		{"pending context", []check{{Context: "ci", State: "PENDING"}}, "pending", nil},
		{"failed context", []check{{Context: "ci/legacy", State: "FAILURE"}}, "red", []string{"ci/legacy"}},
		{"nameless", []check{{Status: "COMPLETED", Conclusion: "CANCELLED"}}, "red", []string{"?"}},
		{"skipped and neutral", []check{{Name: "a", Conclusion: "SKIPPED"}, {Name: "b", Conclusion: "NEUTRAL"}}, "green", nil},
		{"pending beats red", []check{{Name: "a", Conclusion: "FAILURE"}, {Name: "b", Status: "QUEUED"}}, "pending", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := summarize(c.in)
			if s.state != c.state || !reflect.DeepEqual(s.failed, c.failed) {
				t.Errorf("got %+v", s)
			}
		})
	}
}

func TestHelpers(t *testing.T) {
	if got := repoOf("https://github.com/o/r/pull/3"); got != "o/r" {
		t.Errorf("repoOf %q", got)
	}
	if repoOf("5") != "" {
		t.Error("repoOf bare")
	}
	if idOf("https://github.com/o/r/runs/99", `/runs/(\d+)`) != "99" || idOf("42", `/runs/(\d+)`) != "42" {
		t.Error("idOf")
	}
	if dash("a  b\tc") != "a-b-c" {
		t.Error("dash")
	}
	if !ok("success") || !ok("skipped") || !ok("neutral") || ok("failure") || ok("") {
		t.Error("ok")
	}
	if !onlyBatches([]string{"batch:a", "batch:"}) || onlyBatches([]string{"batch:a", "pr:1"}) {
		t.Error("onlyBatches")
	}
}

func TestRunWatcher(t *testing.T) {
	cases := []struct {
		name, body string
		want       []string
	}{
		{"in progress", `{"status":"in_progress"}`, nil},
		{"success", `{"status":"completed","conclusion":"success","url":"r1","jobs":[{"name":"build","conclusion":"success"}]}`, []string{"run 9 success r1"}},
		{"failed jobs", `{"status":"completed","conclusion":"failure","url":"r1","jobs":[{"name":"build all","conclusion":"failure"},{"name":"lint","conclusion":"success"},{"name":"x","conclusion":""}]}`, []string{"run 9 failure failed=build-all r1"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := sandbox(t)
			e.put(t, "run.json", c.body)
			got, err := newRun("https://github.com/o/r/actions/runs/9").poll()
			if err != nil || !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %v err %v", got, err)
			}
		})
	}
	e := sandbox(t)
	e.put(t, "fail-run-view", "")
	if _, err := newRun("9").poll(); err == nil {
		t.Error("want error")
	}
}

func TestBranchWatcher(t *testing.T) {
	cases := []struct {
		name, body string
		want       []string
	}{
		{"no runs", `[]`, nil},
		{"latest sha success", `[
			{"status":"completed","conclusion":"success","name":"ci","headSha":"bbbbbbbbbb","createdAt":"2026-01-02"},
			{"status":"completed","conclusion":"failure","name":"old","headSha":"aaaaaaaaaa","createdAt":"2026-01-01"}]`,
			[]string{"branch feat success head=bbbbbbb"}},
		{"pending run on latest sha", `[
			{"status":"completed","conclusion":"success","name":"ci","headSha":"bbbbbbbbbb","createdAt":"2026-01-02"},
			{"status":"in_progress","name":"slow","headSha":"bbbbbbbbbb","createdAt":"2026-01-02"}]`, nil},
		{"failures listed", `[
			{"status":"completed","conclusion":"failure","name":"unit tests","headSha":"cccccccccc","createdAt":"2026-01-03"},
			{"status":"completed","conclusion":"success","name":"lint","headSha":"cccccccccc","createdAt":"2026-01-03"},
			{"status":"completed","conclusion":"cancelled","name":"e2e","headSha":"cccccccccc","createdAt":"2026-01-03"}]`,
			[]string{"branch feat failure failed=unit-tests,e2e head=ccccccc"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := sandbox(t)
			e.put(t, "runs.json", c.body)
			got, err := branchWatcher{"feat"}.poll()
			if err != nil || !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %v err %v", got, err)
			}
		})
	}
	e := sandbox(t)
	e.put(t, "fail-run-list", "")
	if _, err := (branchWatcher{"feat"}).poll(); err == nil {
		t.Error("want error")
	}
}

const prList = `[
 {"number":1,"url":"u1","headRefName":"mine","updatedAt":"t1","author":{"login":"me"},"labels":[]},
 {"number":2,"url":"u2","headRefName":"batchy","updatedAt":"t1","author":{"login":"other"},"labels":[]},
 {"number":3,"url":"u3","headRefName":"x","updatedAt":"t1","author":{"login":"other"},"labels":[{"name":"agent"}]},
 {"number":4,"url":"u4","headRefName":"y","updatedAt":"t1","author":{"login":"other"},"labels":[{"name":"misc"}]}]`

func TestListWatcherPRs(t *testing.T) {
	e := sandbox(t)
	writeCard(t, "b1", "a", `{"branch":"batchy"}`)
	e.put(t, "prs.json", prList)

	w, err := newList("pr", "agent", allOpts())
	if err != nil {
		t.Fatal(err)
	}
	if got, err := w.poll(); err != nil || got != nil {
		t.Fatalf("baseline: %v %v", got, err)
	}
	if !strings.HasSuffix(w.file, "o-r-prs-agent.json") {
		t.Errorf("file %s", w.file)
	}
	b, _ := os.ReadFile(w.file)
	var stored []item
	if err := json.Unmarshal(b, &stored); err != nil || len(stored) != 3 {
		t.Fatalf("stored baseline %s %v", b, err)
	}
	if got, _ := w.poll(); got != nil {
		t.Errorf("no change: %v", got)
	}

	w2, err := newList("pr", "agent", allOpts())
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(prList, `"headRefName":"mine","updatedAt":"t1"`, `"headRefName":"mine","updatedAt":"t2"`, 1)
	changed = strings.Replace(changed, `[`, `[{"number":9,"url":"u9","headRefName":"n","updatedAt":"t1","author":{"login":"me"},"labels":[]},`, 1)
	e.put(t, "prs.json", changed)
	got, err := w2.poll()
	if err != nil || !reflect.DeepEqual(got, []string{"pr 9 new u9", "pr 1 updated u1"}) {
		t.Errorf("got %v err %v", got, err)
	}
	if got, _ := w2.poll(); got != nil {
		t.Errorf("after save: %v", got)
	}

	w3, _ := newList("pr", "agent", only("new"))
	e.put(t, "prs.json", strings.Replace(changed, `"updatedAt":"t2"`, `"updatedAt":"t3"`, 1))
	if got, _ := w3.poll(); got != nil {
		t.Errorf("updated filtered out: %v", got)
	}
}

func TestListWatcherIssues(t *testing.T) {
	e := sandbox(t)
	e.put(t, "issues.json", `[{"number":1,"url":"i1","updatedAt":"t1"}]`)
	w, err := newList("issue", "", allOpts())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(w.file, "o-r-issues.json") {
		t.Errorf("file %s", w.file)
	}
	if got, _ := w.poll(); got != nil {
		t.Fatalf("baseline: %v", got)
	}
	e.put(t, "issues.json", `[{"number":1,"url":"i1","updatedAt":"t2"},{"number":2,"url":"i2","updatedAt":"t2"}]`)
	got, err := w.poll()
	if err != nil || !reflect.DeepEqual(got, []string{"issue 1 updated i1", "issue 2 new i2"}) {
		t.Errorf("got %v err %v", got, err)
	}
	e.put(t, "fail-issue-list", "")
	if _, err := w.poll(); err == nil {
		t.Error("want error")
	}
	e.put(t, "fail-repo-view", "")
	if _, err := newList("issue", "", allOpts()); err == nil {
		t.Error("want error from repo view")
	}
}

func TestListWatcherEmptyBaseline(t *testing.T) {
	e := sandbox(t)
	w, err := newList("issue", "bug", allOpts())
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := w.poll(); got != nil {
		t.Fatalf("baseline: %v", got)
	}
	e.put(t, "issues.json", `[{"number":1,"url":"i1","updatedAt":"t1"}]`)
	if got, _ := w.poll(); len(got) == 0 {
	}
}

func writeCard(t *testing.T, batch, id, body string) {
	t.Helper()
	dir := filepath.Join(os.Getenv("WARDEN_HOME"), batch)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Error(err)
		return
	}
	if err := os.WriteFile(filepath.Join(dir, id+".json"), []byte(body), 0o644); err != nil {
		t.Error(err)
	}
}

func stamp(d time.Duration) string {
	return time.Now().Add(d).UTC().Format("2006-01-02T15:04:05.000Z")
}

func TestBatchWatcher(t *testing.T) {
	sandbox(t)
	writeCard(t, "b1", "a", `{"state":"working","updated":"`+stamp(0)+`"}`)
	writeCard(t, "b1", "b", `{"state":"working"}`)
	w, err := newBatch("b1", false)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := w.poll(); got != nil {
		t.Fatalf("quiet: %v", got)
	}

	writeCard(t, "b1", "a", `{"state":"working","phase":"code","progress":"2/5","updated":"`+stamp(time.Minute)+`"}`)
	if got, _ := w.poll(); got != nil {
		t.Errorf("progress-only change woke the watcher: %v", got)
	}
	writeCard(t, "b1", "a", `{"state":"pr_open","phase":"pr","progress":"5/5","pr":"u","updated":"`+stamp(time.Minute)+`"}`)
	got, _ := w.poll()
	if len(got) != 1 || !strings.HasPrefix(got[0], "batch b1 a  verify  pr  5/5") {
		t.Errorf("verify: %q", got)
	}

	all, _ := newBatch("b1", true)
	writeCard(t, "b1", "a", `{"state":"working","updated":"`+stamp(time.Minute)+`"}`)
	if got, _ := all.poll(); len(got) != 1 {
		t.Errorf("--all: %q", got)
	}
}

func TestBatchWatcherStale(t *testing.T) {
	sandbox(t)
	writeCard(t, "b1", "a", `{"state":"working","updated":"`+stamp(0)+`"}`)
	w, err := newBatch("", false)
	if err != nil || w.name != "b1" {
		t.Fatalf("newest batch: %+v %v", w, err)
	}
	writeCard(t, "b1", "a", `{"state":"working","updated":"2020-01-01T00:00:00.000Z"}`)
	got, _ := w.poll()
	if !reflect.DeepEqual(got, []string{"batch b1 stale: a"}) {
		t.Errorf("got %q", got)
	}
}

func TestNewBatchErrors(t *testing.T) {
	sandbox(t)
	if _, err := newBatch("", false); err == nil || !strings.Contains(err.Error(), "no batches") {
		t.Errorf("no batches: %v", err)
	}
	if _, err := newBatch("zz", false); err == nil {
		t.Error("unknown batch: want error")
	}
	writeCard(t, "b1", "a", `{}`)
	w, _ := newBatch("b1", false)
	if err := os.RemoveAll(filepath.Join(os.Getenv("WARDEN_HOME"), "b1")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.poll(); err == nil {
		t.Error("vanished batch: want error")
	}
}

func TestExpandWatch(t *testing.T) {
	sandbox(t)
	cfg := os.Getenv("SMITHY_CONFIG")
	cases := []struct {
		name, config string
		in, want     []string
		watch        bool
	}{
		{"default scope", "", []string{"watch"}, []string{"prs"}, true},
		{"explicit prs", "", []string{"watch:prs"}, []string{"prs"}, true},
		{"explicit issues", "", []string{"watch:issues"}, []string{"issues:agent"}, true},
		{"all with label", `{"tend":{"label":"bot"}}`, []string{"watch:all"}, []string{"prs", "issues:bot", "actions", "base", "alerts"}, true},
		{"comma list", "", []string{"watch:actions,base"}, []string{"actions", "base"}, true},
		{"setting scope", `{"tend":{"watch":"issues"}}`, []string{"watch"}, []string{"issues:agent"}, true},
		{"mixed", "", []string{"pr:1", "watch:prs", "batch:x"}, []string{"pr:1", "prs", "batch:x"}, true},
		{"no watch", "", []string{"pr:1"}, []string{"pr:1"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.config == "" {
				os.Remove(cfg)
			} else if err := os.WriteFile(cfg, []byte(c.config), 0o644); err != nil {
				t.Fatal(err)
			}
			got, watch := expandWatch(c.in)
			if !reflect.DeepEqual(got, c.want) || watch != c.watch {
				t.Errorf("got %v %v want %v %v", got, watch, c.want, c.watch)
			}
		})
	}
}

func TestMainErrors(t *testing.T) {
	e := sandbox(t)
	if _, er, c := mainOut(t); c != 2 || !strings.Contains(er, "usage") {
		t.Errorf("no targets: %d %q", c, er)
	}
	if out, _, c := mainOut(t, "--help"); c != 0 || !strings.Contains(out, "usage") {
		t.Errorf("help: %d %q", c, out)
	}
	if _, er, c := mainOut(t, "pr:1", "--bogus"); c != 2 || !strings.Contains(er, "unknown option") {
		t.Errorf("bogus: %d %q", c, er)
	}
	if _, er, c := mainOut(t, "nope:1", "--max", "0"); c != 2 || !strings.Contains(er, "unknown target") {
		t.Errorf("unknown target: %d %q", c, er)
	}
	if _, _, c := mainOut(t, "batch:missing", "--max", "0"); c != 2 {
		t.Errorf("unknown batch: %d", c)
	}
	e.put(t, "fail-pr-view", "")
	if _, er, c := mainOut(t, "pr:1", "--max", "0"); c != 2 || !strings.Contains(er, "cannot read target") {
		t.Errorf("first poll error: %d %q", c, er)
	}
	e.rm("fail-pr-view")
	e.put(t, "signedout", "")
	if _, er, c := mainOut(t, "pr:1", "--max", "0"); c != 3 || !strings.Contains(er, "signed out") {
		t.Errorf("signed out: %d %q", c, er)
	}
	writeCard(t, "b1", "a", `{}`)
	if _, _, c := mainOut(t, "batch:b1", "--max", "0"); c != 4 {
		t.Errorf("batch needs no gh: %d", c)
	}
	t.Setenv("PATH", t.TempDir())
	if _, er, c := mainOut(t, "pr:1", "--max", "0"); c != 3 || !strings.Contains(er, "needs the GitHub CLI or GH_TOKEN") {
		t.Errorf("gh missing: %d %q", c, er)
	}
}

func TestMainEvents(t *testing.T) {
	e := sandbox(t)
	e.put(t, "pr.json", prFixture{rollup: green}.json())
	if o, _, c := mainOut(t, "pr:5", "--max", "0"); c != 0 || o != "pr 5 checks=green u5" {
		t.Errorf("green: %d %q", c, o)
	}
	e.put(t, "pr.json", prFixture{rollup: red}.json())
	if o, _, c := mainOut(t, "pr:5", "--max", "0"); c != 0 || o != "pr 5 checks=red failed=unit-tests u5" {
		t.Errorf("red: %d %q", c, o)
	}
	if o, _, c := mainOut(t, "pr:5", "--max", "0", "--events", "merged"); c != 4 || o != "heartbeat: nothing within 0m" {
		t.Errorf("events filter: %d %q", c, o)
	}
	e.put(t, "pr.json", prFixture{rollup: pending}.json())
	if o, _, c := mainOut(t, "pr:5", "--max", "0"); c != 4 || o != "heartbeat: nothing within 0m" {
		t.Errorf("pending: %d %q", c, o)
	}
	e.put(t, "run.json", `{"status":"completed","conclusion":"failure","url":"r1","jobs":[{"name":"build","conclusion":"failure"}]}`)
	if o, _, c := mainOut(t, "run:9", "--max", "0"); c != 0 || o != "run 9 failure failed=build r1" {
		t.Errorf("run: %d %q", c, o)
	}
}

func TestMainWakesOnChange(t *testing.T) {
	e := sandbox(t)
	e.put(t, "pr.json", prFixture{rollup: pending}.json())
	go func() {
		time.Sleep(200 * time.Millisecond)
		e.put(t, "pr.json", prFixture{state: "MERGED", rollup: pending}.json())
	}()
	o, _, c := mainOut(t, "pr:5", "--every", "0.05", "--max", "0.1")
	if c != 0 || o != "pr 5 merged u5" {
		t.Errorf("got %d %q", c, o)
	}
}

func TestMainEventsOption(t *testing.T) {
	e := sandbox(t)
	e.put(t, "pr.json", prFixture{rollup: pending}.json())
	go func() {
		time.Sleep(150 * time.Millisecond)
		e.put(t, "pr.json", prFixture{rollup: pending, comments: 2}.json())
	}()
	o, _, c := mainOut(t, "pr:5", "--every", "0.05", "--max", "0.1", "--events", "comment")
	if c != 0 || o != "pr 5 comment +2 u5" {
		t.Errorf("events: %d %q", c, o)
	}
}

func TestMainSettings(t *testing.T) {
	e := sandbox(t)
	if err := os.WriteFile(os.Getenv("SMITHY_CONFIG"), []byte(`{"watch":{"events":"merged","every":"0.05","max":"0"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	e.put(t, "pr.json", prFixture{rollup: green}.json())
	if o, _, c := mainOut(t, "pr:5"); c != 4 || o != "heartbeat: nothing within 0m" {
		t.Errorf("settings events: %d %q", c, o)
	}
	if o, _, c := mainOut(t, "pr:5", "--all"); c != 0 || o != "pr 5 checks=green u5" {
		t.Errorf("--all overrides events: %d %q", c, o)
	}
}

func TestMainWatchAndBatch(t *testing.T) {
	e := sandbox(t)
	one := `{"number":1,"url":"u1","headRefName":"m","updatedAt":"t1","author":{"login":"me"},"labels":[]}`
	two := `{"number":2,"url":"u2","headRefName":"m","updatedAt":"t1","author":{"login":"me"},"labels":[]}`
	e.put(t, "prs.json", "["+one+"]")
	if _, _, c := mainOut(t, "watch:prs", "--every", "0.05", "--max", "0"); c != 4 {
		t.Fatalf("baseline run: %d", c)
	}
	e.put(t, "prs.json", "["+one+","+two+"]")
	if o, _, c := mainOut(t, "watch:prs", "--every", "0.05", "--max", "0.1"); c != 0 || o != "pr 2 new u2" {
		t.Errorf("new pr: %d %q", c, o)
	}

	writeCard(t, "b1", "a", `{"state":"working"}`)
	go func() {
		time.Sleep(150 * time.Millisecond)
		writeCard(t, "b1", "a", `{"state":"failed","note":"boom","updated":"`+stamp(time.Minute)+`"}`)
	}()
	o, _, c := mainOut(t, "batch:b1", "--every", "0.05", "--max", "0.1")
	if c != 0 || !strings.Contains(o, "batch b1 a  stuck") || !strings.Contains(o, "boom") {
		t.Errorf("batch: %d %q", c, o)
	}
}

func TestMainListTargetError(t *testing.T) {
	e := sandbox(t)
	e.put(t, "fail-repo-view", "")
	if _, er, c := mainOut(t, "issues", "--max", "0"); c != 2 || !strings.Contains(er, "cannot read target") {
		t.Errorf("%d %q", c, er)
	}
}

type flaky struct{ fails, calls int }

func (f *flaky) poll() ([]string, error) {
	f.calls++
	if f.calls <= f.fails {
		return nil, fmt.Errorf("transient")
	}
	return []string{"ok"}, nil
}

func TestFirstPollRetriesTransientErrors(t *testing.T) {
	f := &flaky{fails: 2}
	lines, err := firstPoll(f, 3, 0)
	if err != nil || len(lines) != 1 || f.calls != 3 {
		t.Fatalf("lines=%v err=%v calls=%d", lines, err, f.calls)
	}
	g := &flaky{fails: 5}
	if _, err := firstPoll(g, 3, 0); err == nil || g.calls != 3 {
		t.Fatalf("want error after 3 tries, calls=%d", g.calls)
	}
}

func TestPRWatcherIgnoresPreviousHeadChecks(t *testing.T) {
	e := sandbox(t)
	oldRed := `[{"name":"unit","status":"COMPLETED","conclusion":"FAILURE","completedAt":"t1"}]`
	newRed := `[{"name":"unit","status":"COMPLETED","conclusion":"FAILURE","completedAt":"t2"}]`
	e.put(t, "pr.json", prFixture{rollup: pending}.json())
	w := newPR("5", allOpts())
	if got, err := w.poll(); err != nil || len(got) != 0 {
		t.Fatalf("first poll: %v %v", got, err)
	}
	e.put(t, "pr.json", prFixture{rollup: oldRed}.json())
	if got, _ := w.poll(); len(got) != 1 || !strings.Contains(got[0], "checks=red") {
		t.Fatalf("old head red: %v", got)
	}
	e.put(t, "pr.json", prFixture{head: "fff9999000", rollup: oldRed}.json())
	if got, _ := w.poll(); !reflect.DeepEqual(got, []string{"pr 5 push head=fff9999 u5"}) {
		t.Errorf("stale rollup after push: %v", got)
	}
	if got, _ := w.poll(); len(got) != 0 {
		t.Errorf("stale rollup still ignored: %v", got)
	}
	e.put(t, "pr.json", prFixture{head: "fff9999000", rollup: pending}.json())
	if got, _ := w.poll(); len(got) != 0 {
		t.Errorf("pending: %v", got)
	}
	e.put(t, "pr.json", prFixture{head: "fff9999000", rollup: newRed}.json())
	if got, _ := w.poll(); !reflect.DeepEqual(got, []string{"pr 5 checks=red failed=unit u5"}) {
		t.Errorf("current head red: %v", got)
	}
}

func TestPRWatcherStaleHoldsThroughEmptyRollup(t *testing.T) {
	e := sandbox(t)
	old := `[{"name":"unit","status":"COMPLETED","conclusion":"SUCCESS","completedAt":"t1"}]`
	e.put(t, "pr.json", prFixture{rollup: pending}.json())
	w := newPR("5", allOpts())
	w.poll()
	for _, step := range []prFixture{{rollup: old}, {head: "fff9999000", rollup: old}, {head: "fff9999000", rollup: "[]"}, {head: "fff9999000", rollup: old}} {
		e.put(t, "pr.json", step.json())
		got, err := w.poll()
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range got {
			if strings.Contains(l, "checks=") && step.head != "" {
				t.Errorf("stale rollup reported for the new head: %v", got)
			}
		}
	}
}

func TestPRWatcherLegacyStatusesAreNotHeldBack(t *testing.T) {
	e := sandbox(t)
	legacy := `[{"context":"ci/legacy","state":"SUCCESS"}]`
	e.put(t, "pr.json", prFixture{rollup: pending}.json())
	w := newPR("5", allOpts())
	w.poll()
	e.put(t, "pr.json", prFixture{rollup: legacy}.json())
	w.poll()
	e.put(t, "pr.json", prFixture{head: "fff9999000", rollup: legacy}.json())
	got, _ := w.poll()
	if !reflect.DeepEqual(got, []string{"pr 5 push head=fff9999 u5", "pr 5 checks=green u5"}) {
		t.Errorf("got %v", got)
	}
}
