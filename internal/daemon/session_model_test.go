//go:build harness

package daemon

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/store"
)

func TestSessionModelAndEffort(t *testing.T) {
	sv, url, made := sessionTest(t, func(ctx context.Context, msg string, h []Exchange, e Emit) (string, error) {
		return "ok", nil
	})
	code, m := post(t, url+"/v1/sessions", `{"agent":"claude","dir":"`+t.TempDir()+`","message":"hi"}`)
	if code != 201 {
		t.Fatalf("start: %d %v", code, m)
	}
	id := m["id"].(string)
	waitStatus(t, sv, id, StatusDone)

	res := do(t, "PUT", url+"/v1/sessions/"+id+"/model", `{"model":"opus","effort":"low"}`, auth)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("set model: %d", res.StatusCode)
	}
	s := waitStatus(t, sv, id, StatusDone)
	if s.Model != "opus" || s.Effort != "low" {
		t.Fatalf("session not updated: %+v", s)
	}
	if code, _ := post(t, url+"/v1/sessions/"+id+"/messages", `{"text":"again"}`); code != 202 {
		t.Fatalf("message: %d", code)
	}
	waitStatus(t, sv, id, StatusDone)
	if len(*made) != 2 {
		t.Errorf("a model change starts a new runner for the next turn, made %v", *made)
	}
	if !strings.Contains(strings.Join(kinds(t, sv, id), " "), KindSessionModel) {
		t.Errorf("no %s event", KindSessionModel)
	}

	for _, body := range []string{`{"model":"-x"}`, `{"model":"opus","effort":"max"}`} {
		res := do(t, "PUT", url+"/v1/sessions/"+id+"/model", body, auth)
		res.Body.Close()
		if res.StatusCode != 400 {
			t.Errorf("%s: want 400, got %d", body, res.StatusCode)
		}
	}
	res = do(t, "PUT", url+"/v1/sessions/nope/model", `{"model":"opus"}`, auth)
	res.Body.Close()
	if res.StatusCode != 404 {
		t.Errorf("unknown session: %d", res.StatusCode)
	}
}

func TestSessionEffortOnlyForAgentsThatTakeIt(t *testing.T) {
	sv, _, _ := sessionTest(t, func(ctx context.Context, msg string, h []Exchange, e Emit) (string, error) { return "", nil })
	s, err := sv.StartSessionWith("opencode", "opencode/big-pickle", t.TempDir(), "", "me")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sv.SetSessionModel(s.ID, "opencode/beam-free", "low"); err == nil {
		t.Error("opencode takes no effort flag: want an error")
	}
	got, err := sv.SetSessionModel(s.ID, "opencode/beam-free", "")
	if err != nil || got.Model != "opencode/beam-free" {
		t.Fatalf("model change: %+v %v", got, err)
	}
}

func TestSessionModelSurvivesRestart(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "s.db")
	st, _ := store.Open(path)
	sv, _ := New(st, testToken, nil, nil)
	sv.Runners = func(string, string, string, string) (Runner, error) {
		return &fakeRunner{fn: func(ctx context.Context, msg string, h []Exchange, e Emit) (string, error) { return "", nil }}, nil
	}
	s, err := sv.StartSession("claude", t.TempDir(), "", "me")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sv.SetSessionModel(s.ID, "haiku", "medium"); err != nil {
		t.Fatal(err)
	}
	st.Close()
	st2, _ := store.Open(path)
	defer st2.Close()
	sv2, err := New(st2, testToken, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range sv2.Sessions() {
		if x.ID == s.ID && (x.Model != "haiku" || x.Effort != "medium") {
			t.Errorf("after restart: %+v", x)
		}
	}
}

func TestCLIRunnerPassesEffort(t *testing.T) {
	r, err := DefaultRunner("claude", "opus", t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r.(effortSetter).SetEffort("low")
	if env := r.(*cliRunner).env(); !containsStr(env, "AGENT_EFFORT=low") {
		t.Errorf("env misses AGENT_EFFORT=low: %v", env)
	}
}

func TestStartSessionWithEffort(t *testing.T) {
	sv, url, _ := sessionTest(t, func(ctx context.Context, msg string, h []Exchange, e Emit) (string, error) { return "ok", nil })
	code, m := post(t, url+"/v1/sessions", `{"agent":"claude","model":"haiku","effort":"low","dir":"`+t.TempDir()+`","message":"hi"}`)
	if code != 201 || m["effort"] != "low" {
		t.Fatalf("start with effort: %d %v", code, m)
	}
	waitStatus(t, sv, m["id"].(string), StatusDone)
	if code, m := post(t, url+"/v1/sessions", `{"agent":"opencode","effort":"low","dir":"`+t.TempDir()+`","message":"hi"}`); code != 400 {
		t.Errorf("opencode takes no effort: %d %v", code, m)
	}
	if code, _ := post(t, url+"/v1/sessions", `{"agent":"claude","effort":"max","dir":"`+t.TempDir()+`","message":"hi"}`); code != 400 {
		t.Errorf("effort over the cap: %d", code)
	}
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
