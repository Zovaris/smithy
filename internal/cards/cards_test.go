package cards

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func setup(t *testing.T) string {
	t.Helper()
	h := t.TempDir()
	t.Setenv("WARDEN_HOME", h)
	t.Setenv("WARDEN_STALE_MIN", "")
	return h
}

func capture(t *testing.T, f func() int) (string, string, int) {
	t.Helper()
	oo, oe := os.Stdout, os.Stderr
	ro, wo, _ := os.Pipe()
	re, we, _ := os.Pipe()
	os.Stdout, os.Stderr = wo, we
	code := f()
	os.Stdout, os.Stderr = oo, oe
	wo.Close()
	we.Close()
	so, _ := io.ReadAll(ro)
	se, _ := io.ReadAll(re)
	return string(so), string(se), code
}

func mustSet(t *testing.T, batch, id string, pairs ...string) map[string]any {
	t.Helper()
	c, err := Set(batch, id, pairs)
	if err != nil {
		t.Fatalf("Set %v: %v", pairs, err)
	}
	return c
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func stamp(d time.Duration) string {
	return time.Now().Add(-d).UTC().Format(time.RFC3339)
}

func TestHome(t *testing.T) {
	t.Setenv("WARDEN_HOME", "/w")
	if Home() != "/w" {
		t.Fatal(Home())
	}
	t.Setenv("WARDEN_HOME", "")
	t.Setenv("SMITHY_HOME", "/s")
	if Home() != filepath.Join("/s", "warden") {
		t.Fatal(Home())
	}
}

func TestSetBasics(t *testing.T) {
	h := setup(t)
	c := mustSet(t, "b1", "a", "state=working", "phase=build", "n=3", "obj={\"x\":1}", "list=[1,2]", "flag=true", "text=hello world")
	if c["id"] != "a" || c["state"] != "working" {
		t.Fatalf("%v", c)
	}
	if c["n"] != float64(3) || c["flag"] != true || c["text"] != "hello world" {
		t.Fatalf("value parsing: %v", c)
	}
	if !reflect.DeepEqual(c["list"], []any{float64(1), float64(2)}) {
		t.Fatalf("list: %v", c["list"])
	}
	if c["obj"].(map[string]any)["x"] != float64(1) {
		t.Fatalf("obj: %v", c["obj"])
	}
	u, _ := c["updated"].(string)
	if _, err := time.Parse("2006-01-02T15:04:05.000Z", u); err != nil {
		t.Fatalf("updated %q: %v", u, err)
	}
	var disk map[string]any
	b, err := os.ReadFile(filepath.Join(h, "b1", "a.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &disk); err != nil || disk["phase"] != "build" {
		t.Fatalf("disk: %v %v", disk, err)
	}
	if matches, _ := filepath.Glob(filepath.Join(h, "b1", "*.tmp")); len(matches) != 0 {
		t.Fatalf("tmp left: %v", matches)
	}
}

func TestSetMergeAndNull(t *testing.T) {
	setup(t)
	mustSet(t, "b", "a", "state=working", "phase=build", "note=x")
	c := mustSet(t, "b", "a", "phase=test", "note=")
	if c["state"] != "working" || c["phase"] != "test" {
		t.Fatalf("merge: %v", c)
	}
	if v, ok := c["note"]; !ok || v != nil {
		t.Fatalf("empty value should be null: %v", c)
	}
	c = mustSet(t, "b", "a", `q="quoted"`, "eq=a=b")
	if c["q"] != "quoted" || c["eq"] != "a=b" {
		t.Fatalf("%v", c)
	}
}

func TestSetUpdatedAdvances(t *testing.T) {
	setup(t)
	c1 := mustSet(t, "b", "a", "state=working")
	time.Sleep(5 * time.Millisecond)
	c2 := mustSet(t, "b", "a", "phase=x")
	if c2["updated"].(string) <= c1["updated"].(string) {
		t.Fatalf("%v <= %v", c2["updated"], c1["updated"])
	}
}

func TestSetErrors(t *testing.T) {
	tests := []struct {
		name         string
		batch, id    string
		prior        [][]string
		pairs        []string
		wantErr      string
		wantNoCreate bool
	}{
		{name: "bad batch", batch: "../x", id: "a", pairs: []string{"state=working"}, wantErr: "must match"},
		{name: "bad id", batch: "b", id: "a/b", pairs: []string{"state=working"}, wantErr: "must match"},
		{name: "empty id", batch: "b", id: "", pairs: nil, wantErr: "must match"},
		{name: "not key=value", batch: "b", id: "a", pairs: []string{"novalue"}, wantErr: "not key=value"},
		{name: "empty key", batch: "b", id: "a", pairs: []string{"=v"}, wantErr: "not key=value"},
		{name: "invalid state", batch: "b", id: "a", pairs: []string{"state=bogus"}, wantErr: "state must be one of"},
		{name: "needs_decision no fields", batch: "b", id: "a", pairs: []string{"state=needs_decision"}, wantErr: "needs_decision requires decision_key"},
		{name: "needs_decision no question", batch: "b", id: "a", pairs: []string{"state=needs_decision", "decision_key=k", "impact=low"}, wantErr: "needs_decision requires question"},
		{name: "needs_decision no impact", batch: "b", id: "a", pairs: []string{"state=needs_decision", "decision_key=k", "question=q"}, wantErr: "needs_decision requires impact"},
		{name: "needs_decision bad impact", batch: "b", id: "a", pairs: []string{"state=needs_decision", "decision_key=k", "question=q", "impact=huge"}, wantErr: "impact must be"},
		{
			name: "open decision blocks state change", batch: "b", id: "a",
			prior:   [][]string{{"state=needs_decision", "decision_key=k", "question=q", "impact=low"}},
			pairs:   []string{"state=working"},
			wantErr: "decision k is still open",
		},
		{
			name: "open decision blocks any update on stateless card", batch: "b", id: "a",
			prior:   [][]string{{"state=needs_decision", "decision_key=k", "question=q", "impact=low"}},
			pairs:   []string{"state="},
			wantErr: "decision k is still open",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := setup(t)
			for _, p := range tc.prior {
				mustSet(t, tc.batch, tc.id, p...)
			}
			before, _ := os.ReadFile(filepath.Join(h, tc.batch, tc.id+".json"))
			_, err := Set(tc.batch, tc.id, tc.pairs)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want %q", err, tc.wantErr)
			}
			after, _ := os.ReadFile(filepath.Join(h, tc.batch, tc.id+".json"))
			if string(before) != string(after) {
				t.Fatal("card changed despite error")
			}
		})
	}
}

func TestSetDecisionLifecycle(t *testing.T) {
	setup(t)
	for _, impact := range []string{"low", "medium", "high"} {
		mustSet(t, "b", "a", "state=needs_decision", "decision_key=d1", "question=which?", "options=[\"x\",\"y\"]", "recommendation=x", "impact="+impact)
	}
	c := mustSet(t, "b", "a", "state=working", "decision_key=")
	for _, k := range []string{"decision_key", "question", "options", "recommendation", "impact"} {
		if _, ok := c[k]; ok {
			t.Fatalf("%s should be deleted: %v", k, c)
		}
	}
	if c["state"] != "working" {
		t.Fatalf("%v", c)
	}
	mustSet(t, "b", "a", "state=blocked")
}

func TestSetNeedsDecisionKeepsFieldsWhenReasserted(t *testing.T) {
	setup(t)
	mustSet(t, "b", "a", "state=needs_decision", "decision_key=k", "question=q", "impact=low")
	c := mustSet(t, "b", "a", "state=needs_decision", "decision_key=k", "question=q2", "impact=high")
	if c["question"] != "q2" || c["impact"] != "high" {
		t.Fatalf("%v", c)
	}
}

func TestSetStateAllowed(t *testing.T) {
	for _, st := range States {
		if st == "needs_decision" {
			continue
		}
		t.Run(st, func(t *testing.T) {
			setup(t)
			if c := mustSet(t, "b", "a", "state="+st, "pr=https://github.com/o/r/pull/1"); c["state"] != st {
				t.Fatal(c)
			}
		})
	}
}

func TestSetCorruptCardStartsFresh(t *testing.T) {
	h := setup(t)
	writeFile(t, filepath.Join(h, "b", "a.json"), "{not json")
	c := mustSet(t, "b", "a", "state=working")
	if c["id"] != "a" {
		t.Fatalf("%v", c)
	}
}

func TestSetDirError(t *testing.T) {
	h := setup(t)
	writeFile(t, filepath.Join(h, "b"), "a file, not a dir")
	if _, err := Set("b", "a", []string{"state=working"}); err == nil {
		t.Fatal("want error")
	}
}

func TestStage(t *testing.T) {
	tests := []struct {
		name  string
		card  map[string]any
		item  map[string]any
		stale string
		want  string
	}{
		{"hold wins over everything", map[string]any{"state": "working"}, map[string]any{"hold": "wait for infra"}, "", "held"},
		{"hold with no card", nil, map[string]any{"hold": "x"}, "", "held"},
		{"queued", nil, map[string]any{}, "", "queued"},
		{"queued nil item", nil, nil, "", "queued"},
		{"starting", nil, map[string]any{"session": "s1"}, "", "starting"},
		{"waiting", map[string]any{"state": "waiting"}, nil, "", "waiting"},
		{"needs_decision", map[string]any{"state": "needs_decision"}, nil, "", "decide"},
		{"blocked", map[string]any{"state": "blocked"}, nil, "", "stuck"},
		{"failed", map[string]any{"state": "failed"}, nil, "", "stuck"},
		{"pr_open", map[string]any{"state": "pr_open"}, nil, "", "verify"},
		{"ready", map[string]any{"state": "ready"}, nil, "", "verify"},
		{"merged", map[string]any{"state": "merged"}, nil, "", "merged"},
		{"reported", map[string]any{"state": "reported"}, nil, "", "reported"},
		{"dropped", map[string]any{"state": "dropped"}, nil, "", "dropped"},
		{"working fresh", map[string]any{"state": "working", "updated": stamp(time.Minute)}, nil, "", "working"},
		{"working stale default", map[string]any{"state": "working", "updated": stamp(2 * time.Hour)}, nil, "", "stuck"},
		{"working under default threshold", map[string]any{"state": "working", "updated": stamp(30 * time.Minute)}, nil, "", "working"},
		{"stale env lowers threshold", map[string]any{"state": "working", "updated": stamp(10 * time.Minute)}, nil, "5", "stuck"},
		{"stale env raises threshold", map[string]any{"state": "working", "updated": stamp(2 * time.Hour)}, nil, "600", "working"},
		{"stale env invalid falls back", map[string]any{"state": "working", "updated": stamp(2 * time.Hour)}, nil, "abc", "stuck"},
		{"no state no updated", map[string]any{}, nil, "", "working"},
		{"bad updated ignored", map[string]any{"updated": "yesterday"}, nil, "", "working"},
		{"waiting never stale", map[string]any{"state": "waiting", "updated": stamp(5 * time.Hour)}, nil, "", "waiting"},
		{"merged never stale", map[string]any{"state": "merged", "updated": stamp(5 * time.Hour)}, nil, "", "merged"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("WARDEN_STALE_MIN", tc.stale)
			if got := Stage(tc.card, tc.item); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestRows(t *testing.T) {
	h := setup(t)
	b := filepath.Join(h, "b1")
	writeFile(t, filepath.Join(b, "batch.json"), `{"batch":"b1","items":[{"id":"a"},{"id":"b","hold":"later"},{"id":"c","session":"s"},{"id":"d"},{"noid":true},"junk"]}`)
	mustSet(t, "b1", "a", "state=pr_open", "phase=verify", "progress=3/5", "pr=u", "ci=green", "note=a note")
	mustSet(t, "b1", "b", "state=working", "note=ignored by hold")
	mustSet(t, "b1", "d", "state=needs_decision", "decision_key=k", "question=pick one?", "note=n", "impact=low")
	mustSet(t, "b1", "extra", "state=working")
	writeFile(t, filepath.Join(b, "broken.json"), "{oops")
	writeFile(t, filepath.Join(b, "readme.txt"), "x")

	rows, err := Rows("b1")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	byID := map[string]Row{}
	for _, r := range rows {
		ids = append(ids, r.ID)
		byID[r.ID] = r
	}
	if want := []string{"a", "b", "c", "d", "broken", "extra"}; len(ids) != len(want) {
		t.Fatalf("ids = %v", ids)
	} else if !reflect.DeepEqual(ids[:4], want[:4]) {
		t.Fatalf("ids = %v", ids)
	}
	a := byID["a"]
	if a.Stage != "verify" || a.State != "pr_open" || a.Phase != "verify" || a.Progress != "3/5" || a.PR != "u" || a.CI != "green" || a.Text != "a note" {
		t.Fatalf("a = %+v", a)
	}
	if a.Updated == nil || a.Age != "0m" {
		t.Fatalf("a updated/age: %v %q", a.Updated, a.Age)
	}
	if r := byID["b"]; r.Stage != "held" || r.Text != "hold: later" {
		t.Fatalf("b = %+v", r)
	}
	if r := byID["c"]; r.Stage != "starting" || r.State != "-" || r.Phase != "-" || r.Age != "-" || r.Updated != nil || r.Text != "" {
		t.Fatalf("c = %+v", r)
	}
	if r := byID["d"]; r.Stage != "decide" || r.Text != "pick one?" {
		t.Fatalf("question should beat note: %+v", r)
	}
	if r := byID["extra"]; r.Stage != "working" {
		t.Fatalf("extra = %+v", r)
	}
	if r := byID["broken"]; r.Stage != "queued" || r.State != "-" {
		t.Fatalf("broken = %+v", r)
	}
}

func TestRowsAgeRounding(t *testing.T) {
	h := setup(t)
	writeFile(t, filepath.Join(h, "b", "a.json"), `{"state":"working","updated":"`+stamp(90*time.Second)+`"}`)
	rows, _ := Rows("b")
	if rows[0].Age != "2m" {
		t.Fatalf("age = %q", rows[0].Age)
	}
}

func TestRowsUpdatedNotRFC3339(t *testing.T) {
	h := setup(t)
	writeFile(t, filepath.Join(h, "b", "a.json"), `{"state":"working","updated":"soon"}`)
	rows, _ := Rows("b")
	if rows[0].Updated == nil || *rows[0].Updated != "soon" || rows[0].Age != "-" {
		t.Fatalf("%+v", rows[0])
	}
}

func TestRowsNoBatch(t *testing.T) {
	h := setup(t)
	_, err := Rows("nope")
	if err == nil || !strings.Contains(err.Error(), "no batch nope in "+h) {
		t.Fatalf("err = %v", err)
	}
}

func TestRowsWithoutPlan(t *testing.T) {
	setup(t)
	mustSet(t, "b", "x", "state=ready")
	rows, err := Rows("b")
	if err != nil || len(rows) != 1 || rows[0].Stage != "verify" {
		t.Fatalf("%v %+v", err, rows)
	}
}

func TestBatchesNewestFirst(t *testing.T) {
	h := setup(t)
	if got := Batches(); len(got) != 0 {
		t.Fatalf("%v", got)
	}
	now := time.Now()
	for i, n := range []string{"old", "mid", "new"} {
		d := filepath.Join(h, n)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		ts := now.Add(time.Duration(i-5) * time.Hour)
		if err := os.Chtimes(d, ts, ts); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(h, "stray.txt"), "x")
	if got := Batches(); !reflect.DeepEqual(got, []string{"new", "mid", "old"}) {
		t.Fatalf("%v", got)
	}
}

func TestTable(t *testing.T) {
	tests := []struct {
		name string
		in   [][]string
		want string
	}{
		{"empty", nil, ""},
		{"single row", [][]string{{"a", "b"}}, "a  b"},
		{"pads all but last", [][]string{{"id", "stage", "text"}, {"longer", "x", "free text here"}}, "id      stage  text\nlonger  x      free text here"},
		{"one column", [][]string{{"a"}, {"bbb"}}, "a\nbbb"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Table(tc.in); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestBoard(t *testing.T) {
	h := setup(t)
	writeFile(t, filepath.Join(h, "b1", "batch.json"), `{"items":[{"id":"a"},{"id":"bb","hold":"h"}]}`)
	mustSet(t, "b1", "a", "state=working", "phase=build", "progress=1/2")

	out, err := Board("b1", false)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out, "\n")
	if lines[0] != "batch b1" || len(lines) != 4 {
		t.Fatalf("%q", out)
	}
	if !strings.HasPrefix(lines[1], "item  stage") || !strings.HasSuffix(lines[1], "question / note") {
		t.Fatalf("head: %q", lines[1])
	}
	if !strings.HasPrefix(lines[2], "a     working  working") || !strings.HasSuffix(lines[3], "hold: h") {
		t.Fatalf("rows: %q", lines)
	}

	js, err := Board("b1", true)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Batch string
		Items []Row
	}
	if err := json.Unmarshal([]byte(js), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Batch != "b1" || len(parsed.Items) != 2 || parsed.Items[0].Stage != "working" || parsed.Items[1].Stage != "held" {
		t.Fatalf("%+v", parsed)
	}
	if !strings.Contains(js, `"updated": null`) {
		t.Fatalf("held item should have null updated: %s", js)
	}

	if _, err := Board("zzz", false); err == nil {
		t.Fatal("want error")
	}
	if _, err := Board("zzz", true); err == nil {
		t.Fatal("want error")
	}
}

func TestMainUsage(t *testing.T) {
	setup(t)
	tests := [][]string{nil, {"bogus"}, {"set"}, {"set", "b", "a"}, {"get"}, {"get", "b"}, {"get", "b", "a", "x"}}
	for _, args := range tests {
		so, se, code := capture(t, func() int { return Main(args) })
		if code != 2 || !strings.Contains(se, "usage: smithy cards") || so != "" {
			t.Fatalf("%v: code %d out %q err %q", args, code, so, se)
		}
	}
}

func TestMainSetGet(t *testing.T) {
	setup(t)
	so, _, code := capture(t, func() int { return Main([]string{"set", "b1", "a", "state=working", "phase=build"}) })
	if code != 0 {
		t.Fatal(code)
	}
	var c map[string]any
	if err := json.Unmarshal([]byte(so), &c); err != nil || c["phase"] != "build" || strings.Contains(strings.TrimSpace(so), "\n") {
		t.Fatalf("%q %v", so, err)
	}
	so, _, code = capture(t, func() int { return Main([]string{"get", "b1", "a"}) })
	if code != 0 || !strings.Contains(so, `"phase": "build"`) {
		t.Fatalf("%d %q", code, so)
	}
	_, se, code := capture(t, func() int { return Main([]string{"get", "b1", "missing"}) })
	if code != 1 || !strings.Contains(se, "no card missing") {
		t.Fatalf("%d %q", code, se)
	}
	_, se, code = capture(t, func() int { return Main([]string{"set", "b1", "a", "state=bogus"}) })
	if code != 1 || !strings.Contains(se, "state must be one of") {
		t.Fatalf("%d %q", code, se)
	}
}

func TestMainBoard(t *testing.T) {
	h := setup(t)
	_, se, code := capture(t, func() int { return Main([]string{"board"}) })
	if code != 1 || !strings.Contains(se, "no batches in "+h) {
		t.Fatalf("%d %q", code, se)
	}
	mustSet(t, "b1", "a", "state=working")
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(h, "b1"), old, old); err != nil {
		t.Fatal(err)
	}
	mustSet(t, "b2", "z", "state=ready")

	so, _, code := capture(t, func() int { return Main([]string{"board"}) })
	if code != 0 || !strings.HasPrefix(so, "batch b2\n") {
		t.Fatalf("newest batch by default: %d %q", code, so)
	}
	so, _, code = capture(t, func() int { return Main([]string{"board", "b1"}) })
	if code != 0 || !strings.HasPrefix(so, "batch b1\n") {
		t.Fatalf("%d %q", code, so)
	}
	so, _, code = capture(t, func() int { return Main([]string{"board", "--json", "b1"}) })
	if code != 0 || !strings.Contains(so, `"batch": "b1"`) {
		t.Fatalf("%d %q", code, so)
	}
	so, _, code = capture(t, func() int { return Main([]string{"board", "b1", "--json"}) })
	if code != 0 || !strings.Contains(so, `"items"`) {
		t.Fatalf("%d %q", code, so)
	}
	_, se, code = capture(t, func() int { return Main([]string{"board", "nope"}) })
	if code != 1 || !strings.Contains(se, "no batch nope") {
		t.Fatalf("%d %q", code, se)
	}
}

func TestMainBoardChangedSince(t *testing.T) {
	setup(t)
	mustSet(t, "b1", "a", "state=working")

	_, _, code := capture(t, func() int { return Main([]string{"board", "b1", "--changed-since", "2999-01-01T00:00:00.000Z"}) })
	if code != 3 {
		t.Fatalf("nothing changed: %d", code)
	}
	so, _, code := capture(t, func() int { return Main([]string{"board", "b1", "--changed-since", "2000-01-01T00:00:00.000Z"}) })
	if code != 0 || !strings.HasPrefix(so, "batch b1") {
		t.Fatalf("changed: %d %q", code, so)
	}
	_, se, code := capture(t, func() int { return Main([]string{"board", "nope", "--changed-since", "2000-01-01T00:00:00.000Z"}) })
	if code != 1 || se == "" {
		t.Fatalf("%d %q", code, se)
	}
}

func TestMainLiveMissingBatch(t *testing.T) {
	setup(t)
	_, se, code := capture(t, func() int { return Main([]string{"live", "nope", "--every", "0"}) })
	if code != 1 || !strings.Contains(se, "no batch nope") {
		t.Fatalf("%d %q", code, se)
	}
}

func TestRaiseImpactHookOnlyRaisesNewQuestions(t *testing.T) {
	t.Setenv("WARDEN_HOME", t.TempDir())
	calls := 0
	RaiseImpact = func(q, imp string) (string, bool) { calls++; return "high", imp != "high" }
	defer func() { RaiseImpact = nil }()
	c, err := Set("b", "i", []string{"state=needs_decision", "decision_key=k", "question=drop the table?", "impact=low"})
	if err != nil || c["impact"] != "high" || c["impact_by"] != "decide" {
		t.Fatalf("card = %v err = %v", c, err)
	}
	if _, err := Set("b", "i", []string{"phase=x"}); err != nil || calls != 1 {
		t.Fatalf("hook ran without a new question: calls=%d err=%v", calls, err)
	}
}

func TestRaiseImpactRechecksALoweredImpact(t *testing.T) {
	t.Setenv("WARDEN_HOME", t.TempDir())
	RaiseImpact = func(q, imp string) (string, bool) { return "high", imp != "high" }
	defer func() { RaiseImpact = nil }()
	Set("b", "j", []string{"state=needs_decision", "decision_key=k", "question=q", "impact=high"})
	c, _ := Set("b", "j", []string{"impact=low"})
	if c["impact"] != "high" {
		t.Fatalf("impact lowered: %v", c["impact"])
	}
}

func TestPROpenNeedsAPR(t *testing.T) {
	t.Setenv("WARDEN_HOME", t.TempDir())
	if _, err := Set("b", "p", []string{"state=pr_open"}); err == nil {
		t.Fatal("pr_open without pr accepted")
	}
	if _, err := Set("b", "p", []string{"state=pr_open", "pr=https://github.com/o/r/pull/1"}); err != nil {
		t.Fatal(err)
	}
}
