//go:build harness

package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/connect"
	"github.com/Zovaris/smithy/internal/events"
	"github.com/Zovaris/smithy/internal/store"
)

func ev(t *testing.T, kind, session string, data any) store.Event {
	b, _ := json.Marshal(data)
	return store.Event{Kind: kind, Session: session, Data: b}
}

func TestNoteOf(t *testing.T) {
	n, ok := NoteOf(ev(t, "event", "", events.Event{Kind: "stage", Batch: "b", Item: "v1", Detail: "blocked"}))
	if !ok || n.Kind != "blocked" || n.Session != "b/v1" || n.Key == "" {
		t.Fatalf("blocked: %+v %v", n, ok)
	}
	for _, d := range []string{"ready", "needs_decision"} {
		if _, ok := NoteOf(ev(t, "event", "", events.Event{Kind: "stage", Batch: "b", Item: "v1", Detail: d})); ok {
			t.Fatalf("stage %s is sent as its decision, not twice", d)
		}
	}
	n, ok = NoteOf(ev(t, KindAsked, "", Decision{ID: "decide:b/v1/queue", Question: "Redis?", Options: []string{"yes", "no"}}))
	if !ok || n.Kind != "decision" || n.Session != "b/v1" || n.Decision != "decide:b/v1/queue" || len(n.Options) != 2 || !strings.Contains(n.Text, "Redis?") {
		t.Fatalf("decision: %+v", n)
	}
	if n.Multi {
		t.Fatalf("single-select decision sent as multi: %+v", n)
	}
	if n, _ = NoteOf(ev(t, KindAsked, "", Decision{ID: "ask:b/v1/1", Question: "Which?", Options: []string{"a", "b"}, Multi: true})); !n.Multi {
		t.Fatalf("multi-select decision lost Multi: %+v", n)
	}
	n, _ = NoteOf(ev(t, KindAsked, "", Decision{ID: "merge:b/v1", Question: "Merge?", Options: []string{"merge", "hold"}}))
	if n.Kind != "pr_ready" || n.Session != "b/v1" {
		t.Fatalf("merge: %+v", n)
	}
	n, _ = NoteOf(ev(t, KindAsked, "", Decision{ID: "secret:abc", Question: "Allow?"}))
	if n.Kind != "decision" || n.Session != "" {
		t.Fatalf("secret: %+v", n)
	}
	n, ok = NoteOf(ev(t, "event", "", events.Event{Kind: "merged", Batch: "b", Item: "v1", PR: "https://github.com/o/r/pull/3", Detail: "merged"}))
	if !ok || n.Kind != "pr_merged" || n.Session != "b/v1" || n.URL == "" {
		t.Fatalf("merged: %+v", n)
	}
	n, ok = NoteOf(ev(t, "event", "", events.Event{Kind: "ci", Batch: "b", Item: "v1", PR: "https://github.com/o/r/pull/3", Detail: "red"}))
	if !ok || n.Kind != "ci_red" || n.Key == "" {
		t.Fatalf("ci red: %+v", n)
	}
	if _, ok := NoteOf(ev(t, "event", "", events.Event{Kind: "ci", Batch: "b", Item: "v1", Detail: "green"})); ok {
		t.Fatal("green ci is no note")
	}
	if _, ok := NoteOf(ev(t, "log", "", map[string]string{"line": "x"})); ok {
		t.Fatal("log lines are not notes")
	}
}

type rec struct {
	mu   sync.Mutex
	sent []string
	upd  []string
}

func (r *rec) Name() string                                          { return "rec" }
func (r *rec) Thread(_ context.Context, s, _ string) (string, error) { return "t" + s, nil }
func (r *rec) Send(_ context.Context, th string, n connect.Note, _ []connect.Button) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, n.Kind)
	return "1", nil
}
func (r *rec) Update(_ context.Context, ref string, n connect.Note) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.upd = append(r.upd, n.Text)
	return nil
}
func (r *rec) Receive(context.Context) ([]connect.Inbound, error) { return nil, nil }

func TestNotesOnceFromCursorAndResolve(t *testing.T) {
	sv, _ := newTest(t, filepath.Join(t.TempDir(), "s.db"))
	sv.Append("event", "", events.Event{Kind: "stage", Batch: "b", Item: "old", Detail: "blocked"})
	r := &rec{}
	h := &connect.Hub{Conns: []connect.Connector{r}, File: filepath.Join(t.TempDir(), "c.json")}
	ctx := context.Background()
	if err := sv.NotesOnce(ctx, h); err != nil {
		t.Fatal(err)
	}
	if len(r.sent) != 0 {
		t.Fatal("history before the first run is not replayed")
	}
	sv.Ask("decide:b/v1/k", "Pick?", []string{"a", "b"})
	sv.Append("event", "", events.Event{Kind: "stage", Batch: "b", Item: "v2", Detail: "failed"})
	sv.Answer("decide:b/v1/k", "a", "web")
	sv.NotesOnce(ctx, h)
	sv.NotesOnce(ctx, h)
	if strings.Join(r.sent, ",") != "decision,blocked" || len(r.upd) != 1 || !strings.Contains(r.upd[0], "a") {
		t.Fatalf("sent %v upd %v", r.sent, r.upd)
	}
}

func TestInputGoesToSession(t *testing.T) {
	sv, _ := newTest(t, filepath.Join(t.TempDir(), "s.db"))
	if err := sv.Input("b/v1", "use redis", "telegram:5"); err != nil {
		t.Fatal(err)
	}
	evs, _ := sv.st.Since(0)
	last := evs[len(evs)-1]
	if last.Kind != KindInput || last.Session != "b/v1" || !strings.Contains(string(last.Data), "use redis") || !strings.Contains(string(last.Data), `"via":"chat"`) {
		t.Fatalf("got %+v", last)
	}
}

func TestWeeklyNoteMondayOnceKeyedByWeek(t *testing.T) {
	mon := time.Date(2026, 11, 23, 9, 30, 0, 0, time.UTC)
	n, ok := WeeklyNote(nil, mon)
	if !ok || n.Kind != "report" || n.Key != "report|2026-11-16" || n.Session != "" || !strings.Contains(n.Text, "Nothing shipped") {
		t.Fatalf("note = %+v ok=%v", n, ok)
	}
	if _, ok := WeeklyNote(nil, mon.Add(24*time.Hour)); ok {
		t.Fatal("sent on Tuesday")
	}
}
