//go:build harness

package daemon

import (
	"context"
	"strings"
	"sync"
	"testing"
)

func TestGoalContinuesUntilMet(t *testing.T) {
	var mu sync.Mutex
	var msgs []string
	sv, url, _ := sessionTest(t, func(ctx context.Context, msg string, h []Exchange, e Emit) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		msgs = append(msgs, msg)
		if len(msgs) == 3 {
			return "all tests pass\nGOAL MET: go test ./... ok", nil
		}
		return "still working", nil
	})
	code, m := post(t, url+"/v1/sessions", `{"agent":"claude","dir":"`+t.TempDir()+`"}`)
	if code != 201 {
		t.Fatalf("start: %d %v", code, m)
	}
	id := m["id"].(string)
	res := do(t, "PUT", url+"/v1/sessions/"+id+"/goal", `{"goal":"make the tests pass"}`, auth)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("set goal: %d", res.StatusCode)
	}
	if code, _ := post(t, url+"/v1/sessions/"+id+"/messages", `{"text":"go"}`); code != 202 {
		t.Fatalf("message: %d", code)
	}
	waitFor(t, func() bool { s, _ := sv.session(id); return s.GoalState == GoalMet })
	s := waitStatus(t, sv, id, StatusDone)
	mu.Lock()
	defer mu.Unlock()
	if len(msgs) != 3 {
		t.Fatalf("want 3 turns (1 sent, 2 continued), got %d: %q", len(msgs), msgs)
	}
	for _, m := range msgs {
		if !strings.Contains(m, "make the tests pass") || !strings.Contains(m, "GOAL MET:") {
			t.Errorf("turn lacks the goal and its done criteria: %q", m)
		}
	}
	if s.Goal != "make the tests pass" || s.GoalTurns != 2 {
		t.Errorf("session goal state: %+v", s)
	}
	if !strings.Contains(strings.Join(kinds(t, sv, id), " "), KindSessionGoal) {
		t.Errorf("no %s event", KindSessionGoal)
	}
}

func TestGoalStopsAtTurnCapAndOnQuestion(t *testing.T) {
	n := 0
	var mu sync.Mutex
	sv, url, _ := sessionTest(t, func(ctx context.Context, msg string, h []Exchange, e Emit) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		n++
		if strings.Contains(msg, "ask me") {
			return "GOAL BLOCKED: which database?", nil
		}
		return "not yet", nil
	})
	sv.GoalMaxTurns = 2
	code, m := post(t, url+"/v1/sessions", `{"agent":"claude","dir":"`+t.TempDir()+`"}`)
	if code != 201 {
		t.Fatalf("start: %d", code)
	}
	id := m["id"].(string)
	if _, err := sv.SetSessionGoal(id, "ship it"); err != nil {
		t.Fatal(err)
	}
	if err := sv.SendSession(id, "go", "me"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { s, _ := sv.session(id); return s.GoalState == GoalCapped })
	waitStatus(t, sv, id, StatusDone)
	mu.Lock()
	if n != 3 {
		t.Errorf("cap 2: want 1+2 turns, got %d", n)
	}
	n = 0
	mu.Unlock()

	if _, err := sv.SetSessionGoal(id, "ask me first"); err != nil {
		t.Fatal(err)
	}
	if err := sv.SendSession(id, "go", "me"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { s, _ := sv.session(id); return s.GoalState == GoalAsked })
	waitStatus(t, sv, id, StatusDone)
	mu.Lock()
	if n != 1 {
		t.Errorf("a question stops the loop: got %d turns", n)
	}
	mu.Unlock()

	before := len(kinds(t, sv, id))
	if _, err := sv.SetSessionGoal(id, "ask me first"); err != nil {
		t.Fatal(err)
	}
	if _, err := sv.SetSessionGoal(id, "ask me first"); err != nil || len(kinds(t, sv, id)) != before+1 {
		t.Errorf("the same goal again records nothing: %v", err)
	}
	s, err := sv.SetSessionGoal(id, "")
	if err != nil || s.Goal != "" || s.GoalState != "" {
		t.Errorf("clear: %+v %v", s, err)
	}
	if _, err := sv.SetSessionGoal("nope", "x"); err == nil {
		t.Error("unknown session accepted")
	}
	if _, err := sv.SetSessionGoal(id, strings.Repeat("x", MaxGoal+1)); err == nil {
		t.Error("oversized goal accepted")
	}
}

func TestGoalSurvivesRestart(t *testing.T) {
	sv, url, _ := sessionTest(t, func(ctx context.Context, msg string, h []Exchange, e Emit) (string, error) { return "", nil })
	code, m := post(t, url+"/v1/sessions", `{"agent":"claude","dir":"`+t.TempDir()+`"}`)
	if code != 201 {
		t.Fatalf("start: %d", code)
	}
	id := m["id"].(string)
	if _, err := sv.SetSessionGoal(id, "keep me"); err != nil {
		t.Fatal(err)
	}
	fresh := &Server{sessions: map[string]*live{}, decisions: map[string]*Decision{}, routed: map[string]bool{}}
	evs, _ := sv.st.Since(0)
	for _, e := range evs {
		fresh.apply(e)
	}
	if fresh.sessions[id].s.Goal != "keep me" {
		t.Errorf("goal not replayed: %+v", fresh.sessions[id].s)
	}
}
