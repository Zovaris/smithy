//go:build harness

package board

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/auto"
	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/linear"
	"github.com/Zovaris/smithy/internal/store"
)

func setup(t *testing.T, settings map[string]string) *Board {
	t.Helper()
	h := t.TempDir()
	t.Setenv("SMITHY_HOME", h)
	t.Setenv("SMITHY_CONFIG", filepath.Join(h, "config.json"))
	t.Setenv("LINEAR_API_KEY", "lin_api_SECRET")
	t.Chdir(t.TempDir())
	for k, v := range settings {
		if err := config.Set(config.UserFile(), k, v); err != nil {
			t.Fatal(err)
		}
	}
	st, err := store.Open(filepath.Join(h, "smithy.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(st)
}

func TestAddListShowSet(t *testing.T) {
	b := setup(t, nil)
	id, err := b.Add(Task{Title: "fix login", Body: "details", Priority: 2, Labels: []string{"auto"}})
	if err != nil || id != 1 {
		t.Fatalf("add %d %v", id, err)
	}
	got, err := b.Get(id)
	if err != nil || got.Title != "fix login" || got.State != "todo" || got.Labels[0] != "auto" {
		t.Fatalf("get %+v %v", got, err)
	}
	if err := b.Set(id, map[string]string{"state": "nope"}); err == nil {
		t.Fatal("bad state accepted")
	}
	if err := b.Set(id, map[string]string{"state": "in_progress", "pr": "https://x/pr/1", "branch": "feat/x"}); err != nil {
		t.Fatal(err)
	}
	got, _ = b.Get(id)
	if got.State != "in_progress" || got.Branch != "feat/x" || len(got.PRs) != 1 {
		t.Fatalf("after set %+v", got)
	}
	b.Set(id, map[string]string{"pr": "https://x/pr/1"})
	if got, _ = b.Get(id); len(got.PRs) != 1 {
		t.Fatalf("duplicate pr %v", got.PRs)
	}
	ts, _ := b.List("in_progress")
	if len(ts) != 1 {
		t.Fatalf("list %v", ts)
	}
	if _, err := b.Get(99); err == nil {
		t.Fatal("missing task found")
	}
}

func TestClaim(t *testing.T) {
	b := setup(t, nil)
	id, _ := b.Add(Task{Title: "t"})
	if err := b.Claim(id, "w1", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := b.Claim(id, "w2", time.Hour); err == nil {
		t.Fatal("second claim won")
	}
	if err := b.Claim(id, "w1", time.Hour); err != nil {
		t.Fatal("holder cannot renew:", err)
	}
	clock = func() time.Time { return time.Now().Add(2 * time.Hour) }
	defer func() { clock = time.Now }()
	if err := b.Claim(id, "w2", time.Hour); err != nil {
		t.Fatal("expired claim not taken:", err)
	}
	if err := b.Release(id, "w1"); err == nil {
		t.Fatal("non-holder released")
	}
	if err := b.Release(id, "w2"); err != nil {
		t.Fatal(err)
	}
}

func TestPollAndReport(t *testing.T) {
	b := setup(t, nil)
	blocker, _ := b.Add(Task{Title: "first"})
	waiting, _ := b.Add(Task{Title: "second", BlockedBy: []int64{blocker}})
	claimed, _ := b.Add(Task{Title: "taken"})
	b.Claim(claimed, "w", time.Hour)
	src := &Source{b: b}
	is, err := src.Poll(time.Time{})
	if err != nil || len(is) != 1 || is[0].Key != "1" || is[0].ID != "B-1" || is[0].Source != "board" {
		t.Fatalf("poll %+v %v", is, err)
	}
	if err := src.Report("B-1", "completed", "https://x/pr/9"); err != nil {
		t.Fatal(err)
	}
	if got, _ := b.Get(blocker); got.State != "review" || got.PRs[0] != "https://x/pr/9" {
		t.Fatalf("report %+v", got)
	}
	b.Set(blocker, map[string]string{"state": "done"})
	is, _ = src.Poll(time.Time{})
	if len(is) != 1 || is[0].Key != "2" {
		t.Fatalf("unblocked poll %+v (want %d)", is, waiting)
	}
	held, _ := b.Add(Task{Title: "from chat", Labels: []string{"x", IntakeLabel}})
	if is, _ = src.Poll(time.Time{}); slices.ContainsFunc(is, func(i auto.Issue) bool { return i.Key == strconv.FormatInt(held, 10) }) {
		t.Fatalf("an unreviewed intake task was offered to auto: %+v", is)
	}
	b.Set(held, map[string]string{"labels": "x"})
	if is, _ = src.Poll(time.Time{}); !slices.ContainsFunc(is, func(i auto.Issue) bool { return i.Key == strconv.FormatInt(held, 10) }) {
		t.Fatalf("a reviewed intake task is not offered: %+v", is)
	}
	if err := src.Report("B-2", "bogus", ""); err == nil {
		t.Fatal("unknown report state accepted")
	}
}

type req struct {
	Query     string
	Variables map[string]any
}

// fakeLinear answers by query shape and keeps the issue list mutable.
type fakeLinear struct {
	mu     sync.Mutex
	issues []map[string]any
	calls  []req
}

func (f *fakeLinear) serve(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var q req
		json.Unmarshal(raw, &q)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls = append(f.calls, q)
		switch {
		case strings.Contains(q.Query, "issues("):
			out, _ := json.Marshal(map[string]any{"data": map[string]any{"issues": map[string]any{
				"nodes": f.issues, "pageInfo": map[string]any{"hasNextPage": false}}}})
			w.Write(out)
		case strings.Contains(q.Query, "issue(id"):
			w.Write([]byte(`{"data":{"issue":{"id":"uuid-1","team":{"key":"TK"}}}}`))
		case strings.Contains(q.Query, "workflowStates"):
			w.Write([]byte(`{"data":{"workflowStates":{"nodes":[{"id":"s-started","type":"started","position":1},{"id":"s-done","type":"completed","position":1}]}}}`))
		default:
			w.Write([]byte(`{"data":{}}`))
		}
	}))
	t.Cleanup(srv.Close)
	old := linear.Endpoint
	linear.Endpoint = srv.URL
	t.Cleanup(func() { linear.Endpoint = old })
}

func (f *fakeLinear) mutations(name string) []req {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []req
	for _, c := range f.calls {
		if strings.Contains(c.Query, name+"(") && strings.HasPrefix(strings.TrimSpace(c.Query), "mutation") {
			out = append(out, c)
		}
	}
	return out
}

func issue(title, state, updated string) map[string]any {
	return map[string]any{"id": "uuid-1", "identifier": "TK-1", "title": title, "description": "b", "url": "https://linear.app/x/TK-1",
		"priority": 3, "state": map[string]any{"type": state}, "labels": map[string]any{"nodes": []any{map[string]any{"name": "auto"}}},
		"team": map[string]any{"key": "TK"}, "updatedAt": updated}
}

// Criterion 7: a Linear title edit and a harness state change on the same task both survive.
func TestSyncFieldOwnership(t *testing.T) {
	b := setup(t, map[string]string{"linear.team": "TK"})
	f := &fakeLinear{issues: []map[string]any{issue("Old title", "unstarted", "2026-11-24T10:00:00.000Z")}}
	f.serve(t)
	lin, err := linear.Open(auto.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	if err := b.Sync(lin, &log); err != nil {
		t.Fatal(err)
	}
	ts, _ := b.List("")
	if len(ts) != 1 || ts[0].Title != "Old title" || ts[0].LinearKey != "TK-1" || ts[0].Priority != 3 {
		t.Fatalf("import %+v", ts)
	}
	id := ts[0].ID
	if err := b.Set(id, map[string]string{"title": "local"}); err == nil {
		t.Fatal("title of a Linear task set locally")
	}
	b.Set(id, map[string]string{"state": "in_progress", "pr": "https://github.com/o/r/pull/7"})
	f.mu.Lock()
	f.issues = []map[string]any{issue("New title", "unstarted", "2026-11-24T11:00:00.000Z")}
	f.mu.Unlock()
	if err := b.Sync(lin, &log); err != nil {
		t.Fatal(err)
	}
	got, _ := b.Get(id)
	if got.Title != "New title" || got.State != "in_progress" {
		t.Fatalf("after sync: title %q state %q", got.Title, got.State)
	}
	up := f.mutations("issueUpdate")
	if len(up) != 1 || up[0].Variables["input"].(map[string]any)["stateId"] != "s-started" {
		t.Fatalf("state push %+v", up)
	}
	if att := f.mutations("attachmentLinkURL"); len(att) != 1 {
		t.Fatalf("pr push %+v", att)
	}
	// A second sync pushes nothing new and the cursor reads from the last updatedAt.
	if err := b.Sync(lin, &log); err != nil {
		t.Fatal(err)
	}
	if len(f.mutations("issueUpdate")) != 1 || len(f.mutations("attachmentLinkURL")) != 1 {
		t.Fatal("sync pushed twice")
	}
	f.mu.Lock()
	last := f.calls[len(f.calls)-1]
	f.mu.Unlock()
	filter, _ := json.Marshal(last.Variables["filter"])
	if !strings.Contains(string(filter), `"gte":"2026-11-24T11:00:00.000Z"`) || strings.Contains(string(filter), `"state"`) {
		t.Fatalf("cursor filter %s", filter)
	}
	if strings.Contains(log.String(), "SECRET") {
		t.Fatal("key in log")
	}
}

func TestSyncSkipsStaleAndNonWork(t *testing.T) {
	b := setup(t, map[string]string{"linear.team": "TK"})
	f := &fakeLinear{issues: []map[string]any{issue("Done elsewhere", "completed", "2026-11-24T10:00:00.000Z")}}
	f.serve(t)
	lin, _ := linear.Open(auto.Settings{})
	if err := b.Sync(lin, io.Discard); err != nil {
		t.Fatal(err)
	}
	if ts, _ := b.List(""); len(ts) != 0 {
		t.Fatalf("completed issue imported: %+v", ts)
	}
}

func TestCLI(t *testing.T) {
	setup(t, nil)
	var out, errw bytes.Buffer
	run := func(args ...string) int { out.Reset(); errw.Reset(); return Main(args, &out, &errw) }
	if c := run("add", "Ship it", "--priority", "2", "--labels=auto,x"); c != 0 || strings.TrimSpace(out.String()) != "B-1" {
		t.Fatalf("add %d %q %q", c, out.String(), errw.String())
	}
	if c := run("set", "B-1", "--state", "review", "--pr", "https://github.com/o/r/pull/1"); c != 0 {
		t.Fatalf("set %d %s", c, errw.String())
	}
	if c := run("claim", "1", "--by", "w1", "--ttl", "10m"); c != 0 {
		t.Fatalf("claim %d %s", c, errw.String())
	}
	if c := run("claim", "1", "--by", "w2"); c != 1 || !strings.Contains(errw.String(), "claimed by w1") {
		t.Fatalf("second claim %d %s", c, errw.String())
	}
	run("show", "B-1", "--json")
	var got Task
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || got.State != "review" || got.Priority != 2 || got.ClaimedBy != "w1" {
		t.Fatalf("show %s %v", out.String(), err)
	}
	if c := run("list", "--state", "review"); c != 0 || !strings.Contains(out.String(), "B-1  review") {
		t.Fatalf("list %q", out.String())
	}
	if c := run("sync"); c != 1 || !strings.Contains(errw.String(), "board.linear_sync") {
		t.Fatalf("sync off %d %s", c, errw.String())
	}
	if c := run("show"); c != 2 {
		t.Fatalf("show without id exit %d", c)
	}
}
