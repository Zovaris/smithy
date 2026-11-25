//go:build harness

package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/store"
)

const testToken = "test-" + "token-abcdef"

func newTest(t *testing.T, path string) (*Server, *httptest.Server) {
	t.Helper()
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	sv, err := New(st, testToken, []string{"me@example.com"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	hs := httptest.NewServer(sv.Handler())
	t.Cleanup(func() { hs.Close(); st.Close() })
	return sv, hs
}

func do(t *testing.T, method, url, body string, hdr map[string]string) *http.Response {
	t.Helper()
	r, _ := http.NewRequest(method, url, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	if r.Header.Get(LoginHeader) != "" {
		r.Host = "box.tail1234.ts.net"
	}
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

var auth = map[string]string{"Authorization": "Bearer " + testToken}

func TestUnauthorized(t *testing.T) {
	_, hs := newTest(t, filepath.Join(t.TempDir(), "s.db"))
	for _, p := range []string{"/v1/state", "/v1/events", "/v1/decisions"} {
		res := do(t, "GET", hs.URL+p, "", nil)
		if res.StatusCode != 401 {
			t.Errorf("%s: %d", p, res.StatusCode)
		}
		res = do(t, "GET", hs.URL+p, "", map[string]string{LoginHeader: "evil@example.com"})
		if res.StatusCode != 401 {
			t.Errorf("%s with a foreign login: %d", p, res.StatusCode)
		}
	}
	if res := do(t, "GET", hs.URL+"/v1/state", "", map[string]string{LoginHeader: "me@example.com"}); res.StatusCode != 200 {
		t.Errorf("allowed login: %d", res.StatusCode)
	}
}

func readSSE(t *testing.T, res *http.Response, n int) []sse {
	t.Helper()
	var out []sse
	sc := bufio.NewScanner(res.Body)
	cur := sse{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for sc.Scan() {
			l := sc.Text()
			switch {
			case strings.HasPrefix(l, "id: "):
				fmt.Sscan(l[4:], &cur.id)
			case strings.HasPrefix(l, "event: "):
				cur.kind = l[7:]
			case l == "" && cur.kind != "":
				out = append(out, cur)
				cur = sse{}
				if len(out) == n {
					return
				}
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("got %d of %d events", len(out), n)
	}
	return out
}

type sse struct {
	id   int64
	kind string
}

func TestEventsSinceSurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.db")
	sv, hs := newTest(t, path)
	for i := 0; i < 3; i++ {
		if _, err := sv.Append("log", "", map[string]string{"line": fmt.Sprint(i)}); err != nil {
			t.Fatal(err)
		}
	}
	hs.Close()
	sv.st.Close()

	sv2, hs2 := newTest(t, path)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, _ := http.NewRequestWithContext(ctx, "GET", hs2.URL+"/v1/events?since=1", nil)
	r.Header.Set("Authorization", "Bearer "+testToken)
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		sv2.Append("live", "", nil)
	}()
	got := readSSE(t, res, 3)
	want := []int64{2, 3, 4}
	for i, e := range got {
		if e.id != want[i] {
			t.Fatalf("got ids %v, want %v", got, want)
		}
	}
	if got[2].kind != "live" {
		t.Fatalf("live event: %v", got[2])
	}
}

func TestLastEventIDWins(t *testing.T) {
	sv, hs := newTest(t, filepath.Join(t.TempDir(), "s.db"))
	for i := 0; i < 4; i++ {
		sv.Append("log", "", nil)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, _ := http.NewRequestWithContext(ctx, "GET", hs.URL+"/v1/events?since=0", nil)
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("Last-Event-ID", "3")
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	if got := readSSE(t, res, 1); got[0].id != 4 {
		t.Fatalf("resume from Last-Event-ID: %v", got)
	}
}

func TestDecisionFirstAnswerWins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.db")
	sv, hs := newTest(t, path)
	if err := sv.Ask("d1", "merge?", []string{"yes", "no"}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make([]Outcome, 8)
	codes := make([]int, 8)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ans := []string{"yes", "no"}[i%2]
			res := do(t, "POST", hs.URL+"/v1/decisions", `{"id":"d1","answer":"`+ans+`"}`, auth)
			codes[i] = res.StatusCode
			json.NewDecoder(res.Body).Decode(&results[i])
		}(i)
	}
	wg.Wait()
	wins := 0
	for i, o := range results {
		if o.Won {
			wins++
			if codes[i] != 200 {
				t.Errorf("winner status %d", codes[i])
			}
		} else if codes[i] != 409 {
			t.Errorf("loser status %d", codes[i])
		}
		if o.Answer != results[0].Answer || o.By != "local" {
			t.Fatalf("outcomes differ: %+v vs %+v", o, results[0])
		}
	}
	if wins != 1 {
		t.Fatalf("%d winners", wins)
	}
	hs.Close()
	sv.st.Close()
	_, hs2 := newTest(t, path)
	res := do(t, "POST", hs2.URL+"/v1/decisions", `{"id":"d1","answer":"other"}`, auth)
	var o Outcome
	json.NewDecoder(res.Body).Decode(&o)
	if res.StatusCode != 409 || o.Won || o.Answer != results[0].Answer {
		t.Fatalf("after restart: %d %+v", res.StatusCode, o)
	}
}

func TestDecisionCSRF(t *testing.T) {
	sv, hs := newTest(t, filepath.Join(t.TempDir(), "s.db"))
	sv.Ask("d3", "pick", []string{"a", "b"})
	body := `{"id":"d3","answer":"a"}`
	with := func(kv ...string) map[string]string {
		h := map[string]string{"Authorization": auth["Authorization"]}
		for i := 0; i < len(kv); i += 2 {
			h[kv[i]] = kv[i+1]
		}
		return h
	}
	for name, h := range map[string]map[string]string{
		"form content type": with("Content-Type", "text/plain"),
		"cross-site fetch":  with("Sec-Fetch-Site", "cross-site"),
		"foreign origin":    with("Origin", "https://evil.example"),
	} {
		if res := do(t, "POST", hs.URL+"/v1/decisions", body, h); res.StatusCode != 403 && res.StatusCode != 415 {
			t.Errorf("%s: %d, want refused", name, res.StatusCode)
		}
	}
	if res := do(t, "POST", hs.URL+"/v1/decisions", body, with("Origin", hs.URL, "Sec-Fetch-Site", "same-origin")); res.StatusCode != 200 {
		t.Fatalf("same origin: %d", res.StatusCode)
	}
}

func TestDecisionValidation(t *testing.T) {
	sv, hs := newTest(t, filepath.Join(t.TempDir(), "s.db"))
	sv.Ask("d2", "pick", []string{"a", "b"})
	cases := map[string]int{
		`{"id":"missing","answer":"a"}`: 404,
		`{"id":"d2","answer":"c"}`:      400,
		`not json`:                      400,
		`{"id":"d2"}`:                   400,
	}
	for body, want := range cases {
		if res := do(t, "POST", hs.URL+"/v1/decisions", body, auth); res.StatusCode != want {
			t.Errorf("%s: %d want %d", body, res.StatusCode, want)
		}
	}
	if res := do(t, "POST", hs.URL+"/v1/decisions", `{"id":"d2","answer":"a"}`, map[string]string{LoginHeader: "me@example.com"}); res.StatusCode != 200 {
		t.Fatalf("login answer: %d", res.StatusCode)
	} else {
		var o Outcome
		json.NewDecoder(res.Body).Decode(&o)
		if o.By != "me@example.com" {
			t.Fatalf("by %q", o.By)
		}
	}
}

func TestState(t *testing.T) {
	sv, hs := newTest(t, filepath.Join(t.TempDir(), "s.db"))
	sv.Ask("d3", "q", []string{"a"})
	res := do(t, "GET", hs.URL+"/v1/state", "", auth)
	var st State
	if err := json.NewDecoder(res.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	if st.LastSeq < 1 || len(st.Pending) != 1 || st.Pending[0].ID != "d3" {
		t.Fatalf("%+v", st)
	}
}
