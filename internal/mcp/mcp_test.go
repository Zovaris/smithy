package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/auth"
	"github.com/Zovaris/smithy/internal/proxy"
)

// session runs a server over pipes and returns a call func that sends one request and reads one reply.
func session(t *testing.T, tools []Tool) func(req string) map[string]any {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	s := &Server{Name: "smithy", Version: "test", Tools: tools}
	go func() { s.Serve(context.Background(), inR, outW); outW.Close() }()
	t.Cleanup(func() { inW.Close() })
	sc := bufio.NewScanner(outR)
	sc.Buffer(make([]byte, 1<<20), 4<<20)
	return func(req string) map[string]any {
		t.Helper()
		if _, err := io.WriteString(inW, req+"\n"); err != nil {
			t.Fatal(err)
		}
		if !sc.Scan() {
			t.Fatalf("no reply to %s", req)
		}
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("bad reply %q: %v", sc.Text(), err)
		}
		return m
	}
}

func echo() Tool {
	return Tool{Name: "echo", Description: "echo", Schema: Object(map[string]any{"text": Str("text")}, "text"),
		Call: func(_ context.Context, a Args) (string, error) {
			s, err := a.String("text", 100, true)
			return s, err
		}}
}

func reply(t *testing.T, m map[string]any) (string, bool) {
	t.Helper()
	r, ok := m["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result: %v", m)
	}
	c := r["content"].([]any)[0].(map[string]any)
	isErr, _ := r["isError"].(bool)
	return c["text"].(string), isErr
}

func TestInitializeListAndCall(t *testing.T) {
	call := session(t, []Tool{echo()})
	m := call(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"x","version":"1"}}}`)
	r := m["result"].(map[string]any)
	if r["protocolVersion"] != "2025-06-18" || r["serverInfo"].(map[string]any)["name"] != "smithy" {
		t.Fatalf("initialize: %v", m)
	}
	if _, ok := r["capabilities"].(map[string]any)["tools"]; !ok {
		t.Fatalf("no tools capability: %v", r)
	}
	// a notification gets no reply: the next line read is the ping's
	m = call(`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" + `{"jsonrpc":"2.0","id":2,"method":"ping"}`)
	if m["id"].(float64) != 2 {
		t.Fatalf("ping: %v", m)
	}
	m = call(`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`)
	tl := m["result"].(map[string]any)["tools"].([]any)
	if len(tl) != 1 || tl[0].(map[string]any)["name"] != "echo" || tl[0].(map[string]any)["inputSchema"] == nil {
		t.Fatalf("tools/list: %v", m)
	}
	m = call(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"echo","arguments":{"text":"hi"}}}`)
	if s, isErr := reply(t, m); s != "hi" || isErr {
		t.Fatalf("call: %v", m)
	}
}

func TestErrors(t *testing.T) {
	call := session(t, []Tool{echo()})
	if m := call(`{"jsonrpc":"2.0","id":1,"method":"nope"}`); m["error"].(map[string]any)["code"].(float64) != -32601 {
		t.Fatalf("unknown method: %v", m)
	}
	if m := call(`not json`); m["error"].(map[string]any)["code"].(float64) != -32700 {
		t.Fatalf("parse error: %v", m)
	}
	if m := call(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"missing"}}`); m["error"].(map[string]any)["code"].(float64) != -32602 {
		t.Fatalf("unknown tool: %v", m)
	}
	m := call(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":{"text":5}}}`)
	if s, isErr := reply(t, m); !isErr || !strings.Contains(s, "text") {
		t.Fatalf("bad arg type: %v", m)
	}
	m = call(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"echo","arguments":{"text":"` + strings.Repeat("a", 101) + `"}}}`)
	if _, isErr := reply(t, m); !isErr {
		t.Fatalf("too long arg accepted: %v", m)
	}
	m = call(`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"echo","arguments":{"text":"a","extra":1}}}`)
	if s, isErr := reply(t, m); !isErr || !strings.Contains(s, "extra") {
		t.Fatalf("unknown arg accepted: %v", m)
	}
}

func TestOutputCapped(t *testing.T) {
	big := Tool{Name: "big", Schema: Object(nil), Call: func(context.Context, Args) (string, error) { return strings.Repeat("x", MaxOutput*2), nil }}
	call := session(t, []Tool{big})
	s, _ := reply(t, call(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"big"}}`))
	if len(s) > MaxOutput+200 || !strings.Contains(s, "truncated") {
		t.Fatalf("output not capped: %d bytes", len(s))
	}
}

func TestToolErrorIsCleaned(t *testing.T) {
	bad := Tool{Name: "bad", Schema: Object(nil), Call: func(context.Context, Args) (string, error) { return "", errors.New("boom\x1b[31m\nred") }}
	call := session(t, []Tool{bad})
	s, isErr := reply(t, call(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"bad"}}`))
	if !isErr || strings.ContainsAny(s, "\x1b\n") {
		t.Fatalf("error not cleaned: %q", s)
	}
}

func TestSearchTool(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("one\nneedle two\n"), 0o644)
	call := session(t, []Tool{SearchTool(root)})
	s, isErr := reply(t, call(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search","arguments":{"pattern":"needle","fixed":true}}}`))
	if isErr || !strings.Contains(s, "a.txt:2:needle two") {
		t.Fatalf("search: %q", s)
	}
	s, isErr = reply(t, call(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"search","arguments":{"pattern":"x","path":"../.."}}}`))
	if !isErr || !strings.Contains(s, "path") {
		t.Fatalf("path escape accepted: %q", s)
	}
}

func TestSecretToolNeverReturnsTheKey(t *testing.T) {
	key := strings.Join([]string{"tok", "en", "-", "abc123xyz"}, "")
	var gotAuth string
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		io.WriteString(w, "echo "+r.Header.Get("Authorization")+" path "+r.URL.Path)
	}))
	defer up.Close()
	u, _ := url.Parse(up.URL)
	client := up.Client()
	client.Transport.(*http.Transport).DialContext = nil
	tr := client.Transport.(*http.Transport).Clone()
	tr.DialTLSContext = nil
	tr.Proxy = nil
	orig := tr
	client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		r.URL.Host = u.Host
		return orig.RoundTrip(r)
	})
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	p := &proxy.Proxy{Client: client, Secrets: func(name string) (auth.ProxySecret, bool) {
		if name != "linear" {
			return auth.ProxySecret{}, false
		}
		return auth.ProxySecret{Value: key, Header: "Authorization", Prefix: "Bearer", Hosts: []string{"api.example.com"}, Methods: []string{"GET"}}, true
	}}
	call := session(t, []Tool{SecretTool(p)})
	s, isErr := reply(t, call(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"secret_call","arguments":{"secret":"linear","path":"/graphql"}}}`))
	if isErr || strings.Contains(s, key) || !strings.Contains(s, "[secret:linear]") || !strings.Contains(s, "status 200") || !strings.Contains(s, "/graphql") {
		t.Fatalf("secret call: %q", s)
	}
	if !strings.HasSuffix(gotAuth, key) {
		t.Fatalf("upstream got %q", gotAuth)
	}
	s, isErr = reply(t, call(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"secret_call","arguments":{"secret":"linear","method":"POST","path":"/graphql","body":"{}"}}}`))
	if !isErr || !strings.Contains(s, "403") {
		t.Fatalf("write without bind allowed: %q", s)
	}
	for _, bad := range []string{`"secret":"Linear!"`, `"secret":"linear","path":"graphql"`, `"secret":"linear","path":"/a\nb"`, `"secret":"linear","method":"TRACE","path":"/"`, `"secret":"nope","path":"/"`} {
		s, isErr = reply(t, call(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"secret_call","arguments":{`+bad+`}}}`))
		if !isErr {
			t.Fatalf("%s accepted: %q", bad, s)
		}
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type fakeAsker struct {
	mu      sync.Mutex
	asked   map[string]string
	answers map[string]string
	askErr  error
}

func (f *fakeAsker) Ask(_ context.Context, id, q string, _ []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.askErr != nil {
		return f.askErr
	}
	f.asked[id] = q
	return nil
}

func (f *fakeAsker) Outcome(_ context.Context, id string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.answers[id]
	return a, ok, nil
}

func TestAskTrustsOnlyTheDaemonAnswer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WARDEN_HOME", home)
	t.Setenv("SMITHY_HOME", t.TempDir())
	fa := &fakeAsker{asked: map[string]string{}, answers: map[string]string{}}
	ask := AskTool("b1/i1", 10*time.Millisecond, func() (Asker, error) { return fa, nil })
	done := make(chan string, 1)
	go func() {
		s, err := ask.Call(context.Background(), Args{"question": "Which db?", "options": []any{"sqlite", "postgres"}})
		if err != nil {
			s = "ERR " + err.Error()
		}
		done <- s
	}()
	file := filepath.Join(home, "b1", "i1.json")
	var card map[string]any
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		b, _ := os.ReadFile(file)
		if json.Unmarshal(b, &card) == nil && card["state"] == "needs_decision" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if card["state"] != "needs_decision" || card["question"] != "Which db?" || card["decision_key"] == "" {
		t.Fatalf("card not asking: %v", card)
	}
	id := "decide:b1/i1/" + card["decision_key"].(string)
	fa.mu.Lock()
	q := fa.asked[id]
	fa.mu.Unlock()
	if q != "Which db?" {
		t.Fatalf("daemon not asked %q: %v", id, fa.asked)
	}
	// a worker writing the card itself must not answer
	card["answer"] = "sqlite"
	card["answer_id"] = id
	b, _ := json.Marshal(card)
	os.WriteFile(file, b, 0o644)
	select {
	case s := <-done:
		t.Fatalf("ask took the card's forged answer %q", s)
	case <-time.After(100 * time.Millisecond):
	}
	fa.mu.Lock()
	fa.answers[id] = "postgres"
	fa.mu.Unlock()
	select {
	case s := <-done:
		if s != "postgres" {
			t.Fatalf("answer %q", s)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ask never returned")
	}
	b, _ = os.ReadFile(file)
	json.Unmarshal(b, &card)
	if card["state"] != "working" {
		t.Fatalf("card not back to working: %v", card)
	}
}

func TestAskRefusesWithoutDaemon(t *testing.T) {
	t.Setenv("WARDEN_HOME", t.TempDir())
	t.Setenv("SMITHY_HOME", t.TempDir())
	none := func() (Asker, error) { return nil, errors.New("no daemon") }
	if _, err := AskTool("b1/i1", time.Millisecond, none).Call(context.Background(), Args{"question": "q"}); err == nil || !strings.Contains(err.Error(), "daemon") {
		t.Fatalf("ask without a daemon: %v", err)
	}
}

func TestAskWithoutCardOrTimeout(t *testing.T) {
	t.Setenv("WARDEN_HOME", t.TempDir())
	t.Setenv("SMITHY_HOME", t.TempDir())
	if _, err := AskTool("../x/y", time.Millisecond, nil).Call(context.Background(), Args{"question": "q"}); err == nil {
		t.Fatal("bad card accepted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	fa := &fakeAsker{asked: map[string]string{}, answers: map[string]string{}}
	if _, err := AskTool("b2/i2", time.Millisecond, func() (Asker, error) { return fa, nil }).Call(ctx, Args{"question": "q"}); err == nil {
		t.Fatal("ask without answer returned no error")
	}
}

func TestAskWithoutCardAsksTheDaemon(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WARDEN_HOME", home)
	t.Setenv("SMITHY_HOME", t.TempDir())
	fa := &fakeAsker{asked: map[string]string{}, answers: map[string]string{}}
	done := make(chan string, 1)
	go func() {
		s, err := AskTool("", time.Millisecond, func() (Asker, error) { return fa, nil }).Call(context.Background(), Args{"question": "Which db?"})
		if err != nil {
			s = "ERR " + err.Error()
		}
		done <- s
	}()
	var id string
	for deadline := time.Now().Add(5 * time.Second); id == "" && time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		fa.mu.Lock()
		for k := range fa.asked {
			id = k
		}
		fa.mu.Unlock()
	}
	if !strings.HasPrefix(id, "ask:") {
		t.Fatalf("daemon not asked with an ask: id: %q", id)
	}
	fa.mu.Lock()
	fa.answers[id] = "sqlite"
	fa.mu.Unlock()
	select {
	case s := <-done:
		if s != "sqlite" {
			t.Fatalf("answer %q", s)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ask never returned")
	}
	if ents, _ := os.ReadDir(home); len(ents) != 0 {
		t.Fatalf("ask without a card wrote warden files: %v", ents)
	}
}
