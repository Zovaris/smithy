package decide

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func server(t *testing.T, status int, body string, got *map[string]any) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		b, _ := io.ReadAll(r.Body)
		if got != nil {
			json.Unmarshal(b, got)
		}
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(s.Close)
	return s
}

func TestSpanAsk(t *testing.T) {
	var req map[string]any
	s := server(t, 200, `{"model":"span-01-free","results":[{"id":"q1","p_present":0.9,"p_absent":0.1},{"id":"q2","p_present":0.2}]}`, &req)
	c := &Client{URL: s.URL + "/api/v1/scores", Model: "span-01-free", Key: "k", Timeout: time.Second}
	p, err := c.Ask("ctx", impactQs)
	if err != nil {
		t.Fatal(err)
	}
	if Impact(p) != "high" || p["q2"] != 0.2 {
		t.Fatalf("p = %v", p)
	}
	if req["model"] != "span-01-free" || len(req["behaviors"].([]any)) != 2 {
		t.Fatalf("request = %v", req)
	}
}

func TestSystemOneAsk(t *testing.T) {
	var req map[string]any
	s := server(t, 200, `{"answers":{"q1":{"type":"noul","noul":0.1},"q2":{"type":"noul","noul":0.7}}}`, &req)
	c := &Client{URL: s.URL + "/v1/systemone", Model: "kev-latest", Key: "k", Timeout: time.Second}
	p, err := c.Ask("ctx", impactQs)
	if err != nil {
		t.Fatal(err)
	}
	if Impact(p) != "medium" {
		t.Fatalf("p = %v", p)
	}
	q := req["questions"].(map[string]any)["q1"].(map[string]any)
	if q["type"] != "noul" || req["state"] != "ctx" {
		t.Fatalf("request = %v", req)
	}
}

func TestAskFailsOnErrorStatusAndMissingAnswer(t *testing.T) {
	s := server(t, 402, `{"detail":"needs credits"}`, nil)
	c := &Client{URL: s.URL + "/scores", Key: "k", Timeout: time.Second}
	if _, err := c.Ask("x", impactQs); err == nil || !strings.Contains(err.Error(), "credits") {
		t.Fatalf("err = %v", err)
	}
	s2 := server(t, 200, `{"results":[{"id":"q1","p_present":0.9}]}`, nil)
	c.URL = s2.URL + "/scores"
	if _, err := c.Ask("x", impactQs); err == nil {
		t.Fatal("want error for a missing answer")
	}
}

func TestAskTimesOut(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(300 * time.Millisecond) }))
	defer s.Close()
	c := &Client{URL: s.URL + "/scores", Key: "k", Timeout: 50 * time.Millisecond}
	if _, err := c.Ask("x", impactQs); err == nil {
		t.Fatal("want timeout")
	}
}

func TestImpactNeverBelowThresholds(t *testing.T) {
	cases := map[string]map[string]float64{
		"low":    {"q1": 0.1, "q2": 0.4},
		"medium": {"q1": 0.49, "q2": 0.5},
		"high":   {"q1": 0.5, "q2": 0.0},
	}
	for want, p := range cases {
		if got := Impact(p); got != want {
			t.Errorf("%v → %s, want %s", p, got, want)
		}
	}
}

func TestRulesReadsBulletsDedupedAndCapped(t *testing.T) {
	d := t.TempDir()
	f := filepath.Join(d, "S.md")
	os.WriteFile(f, []byte("# Standards\n- Go standard library only.\n- short\n* Never skip a failing test.\n- Go standard library only.\ntext\n"), 0o644)
	got := Rules([]string{f, filepath.Join(d, "missing.md")}, 30)
	if len(got) != 2 || got[0] != "Go standard library only." {
		t.Fatalf("rules = %q", got)
	}
	if n := len(Rules([]string{f}, 1)); n != 1 {
		t.Fatalf("cap: %d", n)
	}
}

func setup(t *testing.T, url string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SMITHY_HOME", home)
	cfg := filepath.Join(home, "config.json")
	t.Setenv("SMITHY_CONFIG", cfg)
	t.Setenv("DECIDE_TEST_KEY", "k")
	t.Chdir(t.TempDir())
	if url != "" {
		os.WriteFile(cfg, []byte(`{"decide":{"url":"`+url+`","model":"m","key_env":"DECIDE_TEST_KEY","timeout":"500"}}`), 0o644)
	}
}

func TestOffByDefaultIsSilent(t *testing.T) {
	setup(t, "")
	if _, err := FromConfig(); err != ErrOff {
		t.Fatalf("err = %v", err)
	}
	if code := Main([]string{"impact", "q?"}); code != 3 {
		t.Fatalf("exit = %d", code)
	}
	if v, ok := Raise("q?", "low"); ok || v != "low" {
		t.Fatalf("raise = %s %v", v, ok)
	}
}

func TestRaiseNeverLowers(t *testing.T) {
	s := server(t, 200, `{"answers":{"q1":{"noul":0.1},"q2":{"noul":0.9}}}`, nil)
	setup(t, s.URL+"/v1/systemone")
	if v, ok := Raise("q?", "low"); !ok || v != "medium" {
		t.Fatalf("low → %s %v", v, ok)
	}
	if v, ok := Raise("q?", "high"); ok || v != "high" {
		t.Fatalf("high → %s %v", v, ok)
	}
}

func TestDisabledAfterThreeFailuresInARow(t *testing.T) {
	s := server(t, 500, `boom`, nil)
	setup(t, s.URL+"/v1/systemone")
	for i := 0; i < 3; i++ {
		if _, err := FromConfig(); err != nil {
			t.Fatalf("call %d: off too early: %v", i, err)
		}
		Raise("q?", "low")
	}
	if _, err := FromConfig(); err != ErrOff {
		t.Fatalf("want disabled, err = %v", err)
	}
}

func TestSuccessResetsFailures(t *testing.T) {
	fail := true
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			w.WriteHeader(500)
			return
		}
		io.WriteString(w, `{"answers":{"q1":{"noul":0.1},"q2":{"noul":0.1}}}`)
	}))
	defer s.Close()
	setup(t, s.URL+"/v1/systemone")
	Raise("q?", "low")
	Raise("q?", "low")
	fail = false
	Raise("q?", "low")
	fail = true
	Raise("q?", "low")
	Raise("q?", "low")
	if _, err := FromConfig(); err != nil {
		t.Fatalf("disabled after non-consecutive failures: %v", err)
	}
}

func TestRefusesPlainHTTPToARemoteHost(t *testing.T) {
	setup(t, "http://example.com/v1/systemone?key=secret")
	_, err := FromConfig()
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("err = %v", err)
	}
}
