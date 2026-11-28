//go:build harness

package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/cards"
	"github.com/Zovaris/smithy/internal/events"
	"github.com/Zovaris/smithy/internal/layout"
	"github.com/Zovaris/smithy/internal/store"
)

// wired builds a server that routes answers back through the events files; a test that reads
// them sets its own SMITHY_HOME first so no other test's lines are counted.
func wired(t *testing.T, db string) *Server {
	t.Helper()
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	sv, err := New(st, testToken, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	sv.Route = RouteBack
	if err := sv.Resume(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return sv
}

func line(t *testing.T, f string, e events.Event) {
	t.Helper()
	b, _ := json.Marshal(e)
	fh, err := os.OpenFile(f, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	fh.Write(append(b, '\n'))
	fh.Close()
}

func tailed(t *testing.T, sv *Server) []events.Event {
	t.Helper()
	evs, _ := sv.st.Since(0)
	var out []events.Event
	for _, e := range evs {
		if e.Kind == "event" {
			var w events.Event
			json.Unmarshal(e.Data, &w)
			out = append(out, w)
		}
	}
	return out
}

func ids(es []events.Event) string {
	var s []string
	for _, e := range es {
		s = append(s, e.ID)
	}
	return strings.Join(s, ",")
}

func TestTailAppendsEachLineOnceAcrossRestarts(t *testing.T) {
	dir := t.TempDir()
	f, db := filepath.Join(dir, "events.jsonl"), filepath.Join(dir, "s.db")
	line(t, f, events.Event{Kind: "poll", ID: "a"})
	line(t, f, events.Event{Kind: "poll", ID: "b"})
	sv := wired(t, db)
	tl := &Tail{Path: f, Source: "tail:test"}
	if err := sv.TailOnce(tl); err != nil {
		t.Fatal(err)
	}
	sv.TailOnce(tl)
	fh, _ := os.OpenFile(f, os.O_APPEND|os.O_WRONLY, 0o600)
	fh.WriteString(`{"kind":"poll","id":"c"`)
	fh.Close()
	sv.TailOnce(tl)
	if got := ids(tailed(t, sv)); got != "a,b" {
		t.Fatalf("first pass: %q", got)
	}
	fh, _ = os.OpenFile(f, os.O_APPEND|os.O_WRONLY, 0o600)
	fh.WriteString("}\n")
	fh.Close()
	sv.st.Close()
	sv2 := wired(t, db)
	if err := sv2.TailOnce(&Tail{Path: f, Source: "tail:test"}); err != nil {
		t.Fatal(err)
	}
	if got := ids(tailed(t, sv2)); got != "a,b,c" {
		t.Fatalf("after restart: %q", got)
	}
}

func TestTailFollowsRotation(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "events.jsonl")
	sv := wired(t, filepath.Join(dir, "s.db"))
	tl := &Tail{Path: f, Source: "tail:test"}
	line(t, f, events.Event{Kind: "poll", ID: "a"})
	sv.TailOnce(tl)
	line(t, f, events.Event{Kind: "poll", ID: "b"})
	os.Rename(f, f+".1")
	line(t, f, events.Event{Kind: "poll", ID: "c"})
	sv.TailOnce(tl)
	sv.TailOnce(tl)
	if got := ids(tailed(t, sv)); got != "a,b,c" {
		t.Fatalf("got %q, want a,b,c", got)
	}
}

func TestSecretAskBecomesDecisionAndAnswerReachesProxy(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	dir := t.TempDir()
	sv := wired(t, filepath.Join(dir, "s.db"))
	ask := events.Event{Kind: "secret_ask", ID: "abc123", Detail: "GH_TOKEN POST api.github.com/x"}
	sv.Ingest(ask, ProxySource)
	sv.Ingest(ask, ProxySource)
	p := sv.Pending()
	if len(p) != 1 || p[0].ID != "secret:abc123" || strings.Join(p[0].Options, ",") != "approve,deny" {
		t.Fatalf("pending = %+v", p)
	}
	if o, err := sv.Answer("secret:abc123", "approve", "phone"); err != nil || !o.Won {
		t.Fatalf("answer: %+v %v", o, err)
	}
	sv.Answer("secret:abc123", "deny", "tui")
	es, _ := events.Read(time.Time{}, 0)
	n := 0
	for _, e := range es {
		if e.ID == "abc123" && (e.Kind == "secret_approved" || e.Kind == "secret_denied") {
			n++
			if e.Kind != "secret_approved" {
				t.Fatalf("routed %s", e.Kind)
			}
		}
	}
	if n != 1 {
		t.Fatalf("routed %d times, want 1", n)
	}
	sv.Ingest(events.Event{Kind: "secret_approved", ID: "abc123"}, ProxySource)
	if len(sv.Pending()) != 0 {
		t.Fatal("still pending")
	}
}

func TestProxyAnsweredElsewhereClosesDecisionWithoutRouting(t *testing.T) {
	sv := wired(t, filepath.Join(t.TempDir(), "s.db"))
	sv.Ingest(events.Event{Kind: "secret_ask", ID: "zz9"}, ProxySource)
	sv.Ingest(events.Event{Kind: "secret_denied", ID: "zz9", Detail: "timed out"}, ProxySource)
	if len(sv.Pending()) != 0 {
		t.Fatal("decision still pending")
	}
	sv.Ingest(events.Event{Kind: "secret_ask", ID: "zz9"}, ProxySource)
	if len(sv.Pending()) != 0 {
		t.Fatal("answered decision re-asked")
	}
}

func TestReadyCardBecomesMergeDecisionRoutedToCard(t *testing.T) {
	sv := wired(t, filepath.Join(t.TempDir(), "s.db"))
	if _, err := cards.Set("b1", "i1", []string{"state=pr_open", "pr=https://github.com/o/r/pull/9"}); err != nil {
		t.Fatal(err)
	}
	sv.Ingest(events.Event{Kind: "stage", Batch: "b1", Item: "i1", PR: "https://github.com/o/r/pull/9", Detail: "ready"}, "tail:events")
	p := sv.Pending()
	if len(p) != 1 || p[0].ID != "merge:b1/i1" || !strings.Contains(p[0].Question, "pull/9") {
		t.Fatalf("pending = %+v", p)
	}
	if _, err := sv.Answer("merge:b1/i1", "merge", "me"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(layout.WardenHome(), "b1", "i1.json"))
	var c map[string]any
	json.Unmarshal(b, &c)
	if c["answer"] != "merge" || c["answer_id"] != "merge:b1/i1" || c["answered_by"] != "me" {
		t.Fatalf("card = %v", c)
	}
	sv.Ingest(events.Event{Kind: "stage", Batch: "b1", Item: "i1", Detail: "ready"}, "tail:events")
	sv.Ingest(events.Event{Kind: "stage", Batch: "b1", Item: "i2", Detail: "ready"}, "tail:events")
	if len(sv.Pending()) != 0 {
		t.Fatalf("pending = %+v", sv.Pending())
	}
}

func TestDecideCardBecomesDecision(t *testing.T) {
	sv := wired(t, filepath.Join(t.TempDir(), "s.db"))
	if _, err := cards.Set("b2", "i3", []string{"state=needs_decision", "decision_key=queue", "question=Postgres or Redis?", `options=["postgres","redis"]`, "impact=low"}); err != nil {
		t.Fatal(err)
	}
	sv.Ingest(events.Event{Kind: "stage", Batch: "b2", Item: "i3", Detail: "needs_decision"}, "tail:events")
	p := sv.Pending()
	if len(p) != 1 || p[0].ID != "decide:b2/i3/queue" || p[0].Question != "Postgres or Redis?" || strings.Join(p[0].Options, ",") != "postgres,redis" {
		t.Fatalf("pending = %+v", p)
	}
	if _, err := sv.Answer("decide:b2/i3/queue", "redis", "me"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(layout.WardenHome(), "b2", "i3.json"))
	var c map[string]any
	json.Unmarshal(b, &c)
	if c["answer"] != "redis" || c["state"] != "needs_decision" {
		t.Fatalf("card = %v", c)
	}
}

func TestUnroutedAnswerIsRoutedOnResume(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	db := filepath.Join(t.TempDir(), "s.db")
	sv := wired(t, db)
	sv.Route = func(Decision, Outcome) error { return os.ErrPermission }
	sv.Ingest(events.Event{Kind: "secret_ask", ID: "r1"}, ProxySource)
	sv.Answer("secret:r1", "deny", "me")
	sv.st.Close()
	sv2 := wired(t, db)
	sv2.st.Close()
	wired(t, db)
	es, _ := events.Read(time.Time{}, 0)
	n := 0
	for _, e := range es {
		if e.ID == "r1" && e.Kind == "secret_denied" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("routed %d times, want 1", n)
	}
}

func TestDecisionIDMatchesNotify(t *testing.T) {
	if got := events.DecisionID(events.Event{Kind: "stage", Batch: "b", Item: "i", Detail: "ready"}, ""); got != "merge:b/i" {
		t.Fatal(got)
	}
}

func TestSecretKindsOnlyFromTheProxyLog(t *testing.T) {
	sv := wired(t, filepath.Join(t.TempDir(), "s.db"))
	sv.Ingest(events.Event{Kind: "secret_ask", ID: "f1", Detail: "harmless"}, "tail:events")
	if len(sv.Pending()) != 0 {
		t.Fatal("secret_ask accepted from events.jsonl")
	}
	sv.Ingest(events.Event{Kind: "secret_ask", ID: "f2"}, ProxySource)
	sv.Ingest(events.Event{Kind: "secret_denied", ID: "f2"}, "tail:events")
	if len(sv.Pending()) != 1 {
		t.Fatal("secret_denied from events.jsonl closed a proxy ask")
	}
}

func TestQuestionAndOptionsAreCleaned(t *testing.T) {
	sv := wired(t, filepath.Join(t.TempDir(), "s.db"))
	cards.Set("b3", "i4", []string{"state=needs_decision", "decision_key=k", "question=ok\u001b[2Jhidden\nline", `options=["a\u001b]0;x","b"]`, "impact=low"})
	sv.Ingest(events.Event{Kind: "stage", Batch: "b3", Item: "i4", Detail: "needs_decision"}, "tail:events")
	p := sv.Pending()
	if len(p) != 1 || strings.ContainsAny(p[0].Question+strings.Join(p[0].Options, ""), "\x1b\n") {
		t.Fatalf("pending = %+v", p)
	}
}

func TestSecretAskThatCannotBeRecordedIsDenied(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	sv := wired(t, filepath.Join(t.TempDir(), "s.db"))
	sv.st.Close()
	sv.Ingest(events.Event{Kind: "secret_ask", ID: "lost1"}, ProxySource)
	es, _ := events.Read(time.Time{}, 0)
	for _, e := range es {
		if e.ID == "lost1" && e.Kind == "secret_denied" && strings.Contains(e.Detail, "could not record") {
			return
		}
	}
	t.Fatalf("the proxy is left waiting: %+v", es)
}
