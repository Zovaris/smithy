//go:build harness

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/apiloop"
	"github.com/Zovaris/smithy/internal/store"
	"github.com/Zovaris/smithy/internal/usage"
)

type fakeRunner struct {
	fn     func(ctx context.Context, msg string, h []Exchange, e Emit) (string, error)
	mu     sync.Mutex
	closed int
}

func (f *fakeRunner) Turn(ctx context.Context, msg string, h []Exchange, e Emit) (string, error) {
	return f.fn(ctx, msg, h, e)
}
func (f *fakeRunner) Close() { f.mu.Lock(); f.closed++; f.mu.Unlock() }

func sessionTest(t *testing.T, fn func(ctx context.Context, msg string, h []Exchange, e Emit) (string, error)) (*Server, string, *[]string) {
	t.Helper()
	t.Setenv("SMITHY_HOME", t.TempDir())
	sv, hs := newTest(t, filepath.Join(t.TempDir(), "s.db"))
	var made []string
	var mu sync.Mutex
	sv.Runners = func(agent, dir, sdir string) (Runner, error) {
		mu.Lock()
		made = append(made, agent+"@"+dir)
		mu.Unlock()
		return &fakeRunner{fn: fn}, nil
	}
	return sv, hs.URL, &made
}

func post(t *testing.T, url, body string) (int, map[string]any) {
	t.Helper()
	res := do(t, "POST", url, body, auth)
	defer res.Body.Close()
	var m map[string]any
	b, _ := io.ReadAll(res.Body)
	json.Unmarshal(b, &m)
	return res.StatusCode, m
}

func waitStatus(t *testing.T, sv *Server, id, want string) Session {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, s := range sv.Sessions() {
			if s.ID == id && s.Status == want {
				return s
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("session %s never reached %s: %+v", id, want, sv.Sessions())
	return Session{}
}

func kinds(t *testing.T, sv *Server, id string) []string {
	t.Helper()
	evs, err := sv.st.Since(0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range evs {
		if e.Session == id {
			out = append(out, e.Kind)
		}
	}
	return out
}

func TestSessionStartMessageList(t *testing.T) {
	dir := t.TempDir()
	sv, url, made := sessionTest(t, func(ctx context.Context, msg string, h []Exchange, e Emit) (string, error) {
		e.Text("hel")
		e.Text("lo " + msg)
		e.Tool(ToolEvent{Name: "shell", Args: json.RawMessage(`{"command":"ls"}`), Output: "a.go"})
		e.Usage(usage.Usage{Input: 10, Output: 5})
		return "hello " + msg, nil
	})
	code, m := post(t, url+"/v1/sessions", `{"agent":"claude","dir":"`+dir+`","message":"fix the bug"}`)
	if code != 201 || m["status"] != "working" || m["title"] != "fix the bug" {
		t.Fatalf("start: %d %v", code, m)
	}
	id := m["id"].(string)
	waitStatus(t, sv, id, StatusDone)
	got := strings.Join(kinds(t, sv, id), " ")
	want := "session_started session_message session_status session_text session_text session_tool session_usage session_message session_status"
	if got != want {
		t.Errorf("events:\n got %s\nwant %s", got, want)
	}
	if code, _ := post(t, url+"/v1/sessions/"+id+"/messages", `{"text":"and the test"}`); code != 202 {
		t.Fatalf("message: %d", code)
	}
	s := waitStatus(t, sv, id, StatusDone)
	if s.Usage == nil || s.Usage.Input != 20 {
		t.Errorf("usage adds up per session: %+v", s.Usage)
	}
	res := do(t, "GET", url+"/v1/sessions", "", auth)
	var l SessionList
	json.NewDecoder(res.Body).Decode(&l)
	if len(l.Sessions) != 1 || l.Sessions[0].ID != id || l.LastAgent != "claude" {
		t.Errorf("list: %+v", l)
	}
	if len(*made) != 1 {
		t.Errorf("one runner for both turns: %v", *made)
	}
	code, m = post(t, url+"/v1/sessions", `{"dir":"`+dir+`"}`)
	if code != 201 || m["agent"] != "claude" || m["status"] != "done" {
		t.Errorf("start without agent or message: %d %v", code, m)
	}
}

func TestSessionHistoryReachesRunner(t *testing.T) {
	var got [][]Exchange
	var mu sync.Mutex
	sv, url, _ := sessionTest(t, func(ctx context.Context, msg string, h []Exchange, e Emit) (string, error) {
		mu.Lock()
		got = append(got, h)
		mu.Unlock()
		return "re:" + msg, nil
	})
	_, m := post(t, url+"/v1/sessions", `{"agent":"codex","dir":"`+t.TempDir()+`","message":"one"}`)
	id := m["id"].(string)
	waitStatus(t, sv, id, StatusDone)
	post(t, url+"/v1/sessions/"+id+"/messages", `{"text":"two"}`)
	waitStatus(t, sv, id, StatusDone)
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 || len(got[0]) != 0 || len(got[1]) != 2 || got[1][0].Text != "one" || got[1][1] != (Exchange{"agent", "re:one"}) {
		t.Errorf("history: %+v", got)
	}
}

func TestSessionBadRequests(t *testing.T) {
	_, url, made := sessionTest(t, func(context.Context, string, []Exchange, Emit) (string, error) { return "", nil })
	dir := t.TempDir()
	for _, body := range []string{
		`{"agent":"claude","dir":"relative/path"}`,
		`{"agent":"claude","dir":"` + filepath.Join(dir, "missing") + `"}`,
		`{"agent":"claude; rm -rf /","dir":"` + dir + `"}`,
		`{"agent":"api:nope/x","dir":"` + dir + `"}`,
		`{"agent":"api:openai/$(id)","dir":"` + dir + `"}`,
		`{"agent":"--help","dir":"` + dir + `"}`,
		`not json`,
	} {
		if code, m := post(t, url+"/v1/sessions", body); code != 400 {
			t.Errorf("%s: %d %v", body, code, m)
		}
	}
	if len(*made) != 0 {
		t.Errorf("a bad request made a runner: %v", *made)
	}
	if code, _ := post(t, url+"/v1/sessions/s-nope/messages", `{"text":"x"}`); code != 404 {
		t.Errorf("unknown session: %d", code)
	}
	if code, _ := post(t, url+"/v1/sessions/s-nope/stop", `{}`); code != 404 {
		t.Errorf("stop unknown: %d", code)
	}
	r := do(t, "POST", url+"/v1/sessions", `{"agent":"claude","dir":"`+dir+`"}`, map[string]string{"Authorization": auth["Authorization"], "Content-Type": "text/plain"})
	if r.StatusCode != 415 {
		t.Errorf("form post: %d", r.StatusCode)
	}
	r = do(t, "POST", url+"/v1/sessions", `{"agent":"claude","dir":"`+dir+`"}`, map[string]string{"Authorization": auth["Authorization"], "Origin": "https://evil.example"})
	if r.StatusCode != 403 {
		t.Errorf("foreign origin: %d", r.StatusCode)
	}
	if r := do(t, "POST", url+"/v1/sessions", `{"agent":"claude","dir":"`+dir+`"}`, nil); r.StatusCode != 401 {
		t.Errorf("no token: %d", r.StatusCode)
	}
}

func TestValidAgent(t *testing.T) {
	for _, a := range []string{"", "claude", "codex", "acp:gemini", "api:anthropic/claude-sonnet-4-5", "api:openrouter/meta/llama-3:free", "api:ollama/qwen2.5-coder:7b"} {
		if err := ValidAgent(a); err != nil {
			t.Errorf("%q: %v", a, err)
		}
	}
	for _, a := range []string{"Claude", "-x", "a b", "acp:", "api:", "api:anthropic", "api:anthropic/a b", "claude\n", "../x", "acp:acp:x"} {
		if ValidAgent(a) == nil {
			t.Errorf("%q passed", a)
		}
	}
}

func TestSessionStopThenResume(t *testing.T) {
	started := make(chan struct{}, 4)
	sv, url, made := sessionTest(t, func(ctx context.Context, msg string, h []Exchange, e Emit) (string, error) {
		if msg == "block" {
			started <- struct{}{}
			<-ctx.Done()
			return "partial", ctx.Err()
		}
		return "ok", nil
	})
	_, m := post(t, url+"/v1/sessions", `{"agent":"claude","dir":"`+t.TempDir()+`","message":"block"}`)
	id := m["id"].(string)
	<-started
	if code, _ := post(t, url+"/v1/sessions/"+id+"/stop", `{}`); code != 200 {
		t.Fatalf("stop: %d", code)
	}
	s := waitStatus(t, sv, id, StatusDone)
	if s.Detail != "stopped" {
		t.Errorf("detail: %q", s.Detail)
	}
	if code, _ := post(t, url+"/v1/sessions/"+id+"/messages", `{"text":"go on"}`); code != 202 {
		t.Fatalf("message after stop: %d", code)
	}
	waitStatus(t, sv, id, StatusDone)
	if len(*made) != 2 {
		t.Errorf("a stopped session gets a new runner: %v", *made)
	}
	if k := kinds(t, sv, id); !strings.Contains(strings.Join(k, " "), "session_message") {
		t.Errorf("events: %v", k)
	}
}

func TestSessionFailure(t *testing.T) {
	sv, url, _ := sessionTest(t, func(context.Context, string, []Exchange, Emit) (string, error) {
		return "", errors.New("no claude on PATH")
	})
	_, m := post(t, url+"/v1/sessions", `{"agent":"claude","dir":"`+t.TempDir()+`","message":"x"}`)
	s := waitStatus(t, sv, m["id"].(string), StatusFailed)
	if s.Detail != "no claude on PATH" {
		t.Errorf("detail: %q", s.Detail)
	}
}

func TestSessionPermissionIsADecision(t *testing.T) {
	answer := make(chan string, 1)
	sv, url, _ := sessionTest(t, func(ctx context.Context, msg string, h []Exchange, e Emit) (string, error) {
		a, err := e.Ask(ctx, "Let the agent run shell rm -rf build?", []string{"allow", "deny"})
		answer <- a
		return "answered " + a, err
	})
	_, m := post(t, url+"/v1/sessions", `{"agent":"claude","dir":"`+t.TempDir()+`","message":"clean"}`)
	id := m["id"].(string)
	s := waitStatus(t, sv, id, StatusNeedsYou)
	if !strings.Contains(s.Detail, "rm -rf build") {
		t.Errorf("detail: %q", s.Detail)
	}
	p := sv.Pending()
	if len(p) != 1 || p[0].ID != "perm:"+id+"/1" {
		t.Fatalf("pending: %+v", p)
	}
	if session(p[0].ID) != id {
		t.Errorf("chat thread of %s: %q", p[0].ID, session(p[0].ID))
	}
	if code, _ := post(t, url+"/v1/decisions", `{"id":"`+p[0].ID+`","answer":"allow"}`); code != 200 {
		t.Fatalf("answer: %d", code)
	}
	if a := <-answer; a != "allow" {
		t.Errorf("runner got %q", a)
	}
	waitStatus(t, sv, id, StatusDone)
	evs, _ := sv.st.Since(0)
	for _, e := range evs {
		if e.Kind == KindAsked && e.Session != id {
			t.Errorf("the decision event carries no session: %+v", e)
		}
	}
}

func TestSessionsSurviveRestart(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "s.db")
	st, _ := store.Open(path)
	sv, _ := New(st, testToken, nil, nil)
	hold := make(chan struct{})
	sv.Runners = func(string, string, string) (Runner, error) {
		return &fakeRunner{fn: func(ctx context.Context, msg string, h []Exchange, e Emit) (string, error) {
			if msg == "ask" {
				e.Ask(ctx, "ok?", []string{"allow", "deny"})
			}
			<-hold
			return "", nil
		}}, nil
	}
	a, err := sv.StartSession("claude", t.TempDir(), "ask", "me")
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, sv, a.ID, StatusNeedsYou)
	st.Close()
	st2, _ := store.Open(path)
	defer st2.Close()
	sv2, err := New(st2, testToken, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ss := sv2.Sessions()
	if len(ss) != 1 || ss[0].Status != StatusFailed || !strings.Contains(ss[0].Detail, "restarted") || ss[0].Title != "ask" {
		t.Errorf("after restart: %+v", ss)
	}
	if p := sv2.Pending(); len(p) != 0 {
		t.Errorf("a dead session's permission stays pending: %+v", p)
	}
	close(hold)
}

func TestChatReplyReachesSession(t *testing.T) {
	got := make(chan string, 1)
	sv, url, _ := sessionTest(t, func(ctx context.Context, msg string, h []Exchange, e Emit) (string, error) {
		got <- msg
		return "", nil
	})
	_, m := post(t, url+"/v1/sessions", `{"agent":"claude","dir":"`+t.TempDir()+`"}`)
	id := m["id"].(string)
	if err := sv.Input(id, "use redis", "tg:42"); err != nil {
		t.Fatal(err)
	}
	if msg := <-got; msg != "use redis" {
		t.Errorf("turn got %q", msg)
	}
}

func TestSessionLimits(t *testing.T) {
	hold := make(chan struct{})
	defer close(hold)
	sv, url, _ := sessionTest(t, func(ctx context.Context, msg string, h []Exchange, e Emit) (string, error) {
		select {
		case <-hold:
		case <-ctx.Done():
		}
		return "", nil
	})
	dir := t.TempDir()
	for i := 0; i < MaxSessions; i++ {
		if _, err := sv.StartSession("claude", dir, "", "me"); err != nil {
			t.Fatal(err)
		}
	}
	if code, _ := post(t, url+"/v1/sessions", `{"agent":"claude","dir":"`+dir+`"}`); code != 429 {
		t.Errorf("over the cap: %d", code)
	}
	id := sv.Sessions()[0].ID
	for i := 0; i < MaxQueued+1; i++ {
		sv.SendSession(id, "x", "me")
	}
	if code, _ := post(t, url+"/v1/sessions/"+id+"/messages", `{"text":"x"}`); code != 429 {
		t.Errorf("queue full: %d", code)
	}
	if code, _ := post(t, url+"/v1/sessions/"+id+"/messages", `{"text":"`+strings.Repeat("a", MaxMessage+1)+`"}`); code != 400 {
		t.Errorf("huge message: %d", code)
	}
}

func TestCLIPrompt(t *testing.T) {
	if CLIPrompt(nil, "hi") != "hi" {
		t.Error("first turn is the message alone")
	}
	p := CLIPrompt([]Exchange{{"user", "a"}, {"agent", "b"}}, "c")
	if !strings.Contains(p, "User: a\n\nYou: b") || !strings.HasSuffix(p, "c") {
		t.Errorf("prompt: %q", p)
	}
	long := CLIPrompt([]Exchange{{"user", strings.Repeat("x", MaxTranscript*2)}}, "c")
	if len(long) > MaxTranscript+500 {
		t.Errorf("transcript not capped: %d", len(long))
	}
}

type fakeTools struct{ refuse func(c apiloop.ToolCall) bool }

func (f fakeTools) Specs() []apiloop.ToolSpec { return nil }
func (f fakeTools) Permit(c apiloop.ToolCall) (bool, string) {
	if f.refuse(c) {
		return false, "refused"
	}
	return true, ""
}
func (f fakeTools) Call(context.Context, apiloop.ToolCall) (string, error) { return "", nil }

type askOnly struct {
	Emit
	asked []string
	reply string
}

func (a *askOnly) Ask(ctx context.Context, q string, o []string) (string, error) {
	a.asked = append(a.asked, q)
	return a.reply, nil
}

func TestAskToolsKeepsSecretsHard(t *testing.T) {
	e := &askOnly{reply: "allow"}
	at := &askTools{Tools: fakeTools{func(apiloop.ToolCall) bool { return true }},
		hard: fakeTools{func(c apiloop.ToolCall) bool { return c.Name == "secret" }}, ctx: context.Background(), e: e}
	if ok, _ := at.Permit(apiloop.ToolCall{Name: "shell", Args: json.RawMessage(`{"command":"rm x"}`)}); !ok || len(e.asked) != 1 {
		t.Errorf("a refused call is asked and the allow kept: %v %v", ok, e.asked)
	}
	if ok, _ := at.Permit(apiloop.ToolCall{Name: "secret"}); ok || len(e.asked) != 1 {
		t.Errorf("a secret read was asked or allowed: %v %v", ok, e.asked)
	}
	e.reply = "deny"
	if ok, _ := at.Permit(apiloop.ToolCall{Name: "shell"}); ok {
		t.Error("deny allowed")
	}
}

var _ = http.StatusOK

func TestSessionClient(t *testing.T) {
	_, url, _ := sessionTest(t, func(context.Context, string, []Exchange, Emit) (string, error) { return "ok", nil })
	c := &Client{Base: url, Token: testToken}
	ctx := context.Background()
	s, err := c.StartSession(ctx, "claude", t.TempDir(), "")
	if err != nil || s.ID == "" {
		t.Fatal(s, err)
	}
	if err := c.SendSession(ctx, s.ID, "hi"); err != nil {
		t.Fatal(err)
	}
	if err := c.StopSession(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	l, err := c.Sessions(ctx)
	if err != nil || len(l.Sessions) != 1 {
		t.Fatal(l, err)
	}
	if _, err := c.StartSession(ctx, "bad agent", "/", ""); err == nil || !strings.Contains(err.Error(), "agent:") {
		t.Errorf("the daemon's message reaches the caller: %v", err)
	}
}

func TestEmitDiff(t *testing.T) {
	sv, url, _ := sessionTest(t, func(context.Context, string, []Exchange, Emit) (string, error) { return "", nil })
	_ = url
	dir := t.TempDir()
	git := func(args ...string) {
		c := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=a@b", "-c", "user.name=a"}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatal(string(out))
		}
	}
	git("init", "-q")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644)
	git("add", ".")
	git("commit", "-qm", "init")
	s, _ := sv.StartSession("claude", dir, "", "me")
	sv.emitDiff(s.ID, s.Dir)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("two\n"), 0o644)
	sv.emitDiff(s.ID, s.Dir)
	sv.emitDiff(s.ID, s.Dir)
	evs, _ := sv.st.Since(0)
	var diffs []string
	for _, e := range evs {
		if e.Kind == KindSessionDiff {
			diffs = append(diffs, string(e.Data))
		}
	}
	if len(diffs) != 1 || !strings.Contains(diffs[0], "+two") || !strings.Contains(diffs[0], "a.txt") {
		t.Errorf("diffs: %v", diffs)
	}
}

const fakeAgent = `#!/bin/sh
log=$5; prompt=$6
printf 'cli=%s proto=%s cwd=%s ' "$AGENT_CLI" "$AGENT_PROTOCOL" "$3" > "$log"
if grep -q block "$prompt"; then trap 'echo term > "$log.term"; kill $!; exit 0' TERM; sleep 30 & wait; fi
cat "$prompt" >> "$log"
echo '{"input_tokens":3,"output_tokens":4}' > "$log.usage.json"
`

func TestCLIRunnerTurn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script stand-in")
	}
	exe := filepath.Join(t.TempDir(), "smithy")
	os.WriteFile(exe, []byte(fakeAgent), 0o755)
	dir, sdir := t.TempDir(), t.TempDir()
	r := &cliRunner{exe: exe, agent: "acp:gemini", dir: dir, sdir: sdir}
	e := &recEmit{}
	reply, err := r.Turn(context.Background(), "second", []Exchange{{"user", "first"}, {"agent", "ok"}}, e)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "cli=gemini proto=acp cwd="+dir) || !strings.Contains(reply, "User: first") || !strings.HasSuffix(reply, "second") {
		t.Errorf("reply: %q", reply)
	}
	if strings.Join(e.text, "") != reply || e.usage.Input != 3 {
		t.Errorf("streamed %q usage %+v", e.text, e.usage)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(300 * time.Millisecond); cancel() }()
	start := time.Now()
	if _, err := r.Turn(ctx, "block", nil, e); !errors.Is(err, context.Canceled) || time.Since(start) > 5*time.Second {
		t.Errorf("stop: %v after %s", err, time.Since(start))
	}
	if _, err := os.Stat(filepath.Join(sdir, "turn-2.log.term")); err != nil {
		t.Error("a stop sends SIGTERM first, so smithy agent can stop the CLI's own group")
	}
}

type recEmit struct {
	mu    sync.Mutex
	text  []string
	usage usage.Usage
}

func (r *recEmit) Text(t string)                                         { r.mu.Lock(); r.text = append(r.text, t); r.mu.Unlock() }
func (r *recEmit) Tool(ToolEvent)                                        {}
func (r *recEmit) Usage(u usage.Usage)                                   { r.usage = u }
func (r *recEmit) Ask(context.Context, string, []string) (string, error) { return "deny", nil }
