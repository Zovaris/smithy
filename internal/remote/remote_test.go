package remote

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func tmpStore(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "servers.json")
}

func TestStoreAddUseRemove(t *testing.T) {
	f := tmpStore(t)
	if err := Add(f, Server{Name: "home", URL: "http://10.0.0.2:7478/", Token: "t1"}); err != nil {
		t.Fatal(err)
	}
	if err := Add(f, Server{Name: "mac", URL: "https://mac.tail1.ts.net"}); err != nil {
		t.Fatal(err)
	}
	if err := Add(f, Server{Name: "local", URL: "http://x"}); err == nil {
		t.Fatal("the name local is reserved")
	}
	if err := Add(f, Server{Name: "bad name", URL: "http://x"}); err == nil {
		t.Fatal("names are one word")
	}
	if err := Add(f, Server{Name: "x", URL: "ftp://x"}); err == nil {
		t.Fatal("http(s) urls only")
	}
	if err := Use(f, "mac"); err != nil {
		t.Fatal(err)
	}
	if err := Use(f, "nope"); err == nil {
		t.Fatal("unknown server")
	}
	l, err := Load(f)
	if err != nil {
		t.Fatal(err)
	}
	if l.Default != "mac" || len(l.Servers) != 2 || l.Servers[0].URL != "http://10.0.0.2:7478" {
		t.Fatalf("got %+v", l)
	}
	fi, _ := os.Stat(f)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v: the file holds tokens", fi.Mode().Perm())
	}
	if err := Remove(f, "mac"); err != nil {
		t.Fatal(err)
	}
	l, _ = Load(f)
	if l.Default != "" || len(l.Servers) != 1 {
		t.Fatalf("removing the default clears it: %+v", l)
	}
	if err := Add(f, Server{Name: "home", URL: "http://10.0.0.3:7478", Token: "t2"}); err != nil {
		t.Fatal(err)
	}
	l, _ = Load(f)
	if len(l.Servers) != 1 || l.Servers[0].Token != "t2" {
		t.Fatalf("adding a known name replaces it: %+v", l)
	}
}

func TestLoadRefusesOpenFile(t *testing.T) {
	f := tmpStore(t)
	os.WriteFile(f, []byte(`{"servers":[]}`), 0o644)
	if _, err := Load(f); err == nil {
		t.Fatal("a servers file readable by others is refused")
	}
}

func TestResolve(t *testing.T) {
	f := tmpStore(t)
	Add(f, Server{Name: "home", URL: "http://10.0.0.2:7478", Token: "t"})
	Add(f, Server{Name: "mac", URL: "https://mac.tail1.ts.net"})
	cases := []struct{ flag, env, def, want string }{
		{"", "", "", "local"},
		{"", "", "mac", "mac"},
		{"", "home", "mac", "home"},
		{"mac", "home", "", "mac"},
		{"local", "home", "mac", "local"},
		{"http://1.2.3.4:9", "", "", "http://1.2.3.4:9"},
	}
	for _, c := range cases {
		Use(f, c.def)
		if c.def == "" {
			Use(f, "local")
		}
		s, err := Resolve(f, c.flag, c.env)
		if err != nil {
			t.Fatalf("%+v: %v", c, err)
		}
		got := s.Name
		if got == "" {
			got = s.URL
		}
		if got != c.want {
			t.Fatalf("%+v: got %+v", c, s)
		}
	}
	if _, err := Resolve(f, "ghost", ""); err == nil {
		t.Fatal("an unknown name is an error, not a silent local")
	}
}

const tsStatus = `{"Self":{"DNSName":"me.tail1.ts.net.","TailscaleIPs":["100.64.0.1"],"Online":true},
"Peer":{
 "k1":{"HostName":"homebox","DNSName":"homebox.tail1.ts.net.","TailscaleIPs":["100.64.0.2","fd7a::2"],"Online":true,"OS":"linux"},
 "k2":{"HostName":"phone","DNSName":"phone.tail1.ts.net.","TailscaleIPs":["100.64.0.3"],"Online":false,"OS":"iOS"},
 "k3":{"HostName":"mac","DNSName":"mac.tail1.ts.net.","TailscaleIPs":["100.64.0.4"],"Online":true,"OS":"macOS"}}}`

func TestParseTailscale(t *testing.T) {
	ps, err := ParseTailscale([]byte(tsStatus))
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 2 || ps[0].Host != "homebox" || ps[0].DNS != "homebox.tail1.ts.net" || ps[0].IP != "100.64.0.2" || ps[1].Host != "mac" {
		t.Fatalf("online peers only, sorted, trailing dot trimmed, ipv4 first: %+v", ps)
	}
	if _, err := ParseTailscale([]byte("nope")); err == nil {
		t.Fatal("bad json")
	}
}

func helloServer(t *testing.T, version string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/hello" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(Hello{Smithy: true, Version: version})
	}))
}

func TestDiscoverProbesInParallelAndCaches(t *testing.T) {
	good := helloServer(t, "0.23.0")
	defer good.Close()
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	}))
	defer slow.Close()
	var runs atomic.Int32
	d := Discoverer{
		Cache:   filepath.Join(t.TempDir(), "peers.json"),
		TTL:     time.Minute,
		Timeout: 300 * time.Millisecond,
		Status: func(context.Context) ([]byte, error) {
			runs.Add(1)
			return []byte(tsStatus), nil
		},
		Candidates: func(p Peer) []string {
			if p.Host == "homebox" {
				return []string{slow.URL, good.URL}
			}
			return []string{slow.URL}
		},
	}
	start := time.Now()
	found, err := d.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("probes run in parallel with a timeout, took %v", time.Since(start))
	}
	if len(found) != 1 || found[0].Host != "homebox" || found[0].URL != good.URL || found[0].Version != "0.23.0" {
		t.Fatalf("only peers that answer as smithy: %+v", found)
	}
	again, _ := d.Discover(context.Background())
	if runs.Load() != 1 || len(again) != 1 {
		t.Fatalf("a fresh cache skips tailscale: runs=%d %+v", runs.Load(), again)
	}
}

func TestDiscoverWithoutTailscale(t *testing.T) {
	d := Discoverer{Cache: filepath.Join(t.TempDir(), "p.json"), Status: func(context.Context) ([]byte, error) {
		return nil, errors.New("exec: tailscale: not found")
	}}
	if _, err := d.Discover(context.Background()); err == nil || !strings.Contains(err.Error(), "tailscale") {
		t.Fatalf("got %v", err)
	}
}

func TestProbeStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/hello":
			json.NewEncoder(w).Encode(Hello{Smithy: true, Version: "0.23.0"})
		case "/v1/setup":
			if r.Header.Get("Authorization") != "Bearer tok" {
				http.Error(w, "no", http.StatusUnauthorized)
				return
			}
			w.Write([]byte(`{"agents":[{"name":"claude","ready":true,"kind":"cli"},{"name":"opencode","ready":false,"kind":"cli"}]}`))
		case "/v1/sessions":
			w.Write([]byte(`{"sessions":[{"id":"s1","agent":"claude","title":"fix it","status":"running"}]}`))
		}
	}))
	defer srv.Close()
	st := Probe(context.Background(), Server{Name: "home", URL: srv.URL, Token: "tok"}, time.Second)
	if !st.Online || st.Version != "0.23.0" || len(st.Agents) != 1 || st.Agents[0] != "claude" || len(st.Sessions) != 1 || st.Sessions[0].Title != "fix it" {
		t.Fatalf("got %+v", st)
	}
	st = Probe(context.Background(), Server{Name: "x", URL: "http://127.0.0.1:1"}, 200*time.Millisecond)
	if st.Online || st.Err == "" {
		t.Fatalf("offline: %+v", st)
	}
}

func TestPairClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Code, Name string }
		json.NewDecoder(r.Body).Decode(&b)
		if r.URL.Path != "/v1/pair" || b.Code != "ABCD-2345" || r.Header.Get("Content-Type") != "application/json" {
			http.Error(w, `{"error":"bad code"}`, http.StatusForbidden)
			return
		}
		w.Write([]byte(`{"token":"devtok","version":"0.23.0"}`))
	}))
	defer srv.Close()
	tok, err := Pair(context.Background(), srv.URL, "abcd2345", "laptop")
	if err != nil || tok != "devtok" {
		t.Fatalf("code is normalised: %q %v", tok, err)
	}
	if _, err := Pair(context.Background(), srv.URL, "WRONG-0000", "laptop"); err == nil {
		t.Fatal("a refused code is an error")
	}
}

func TestCmd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/pair":
			w.Write([]byte(`{"token":"smd_x"}`))
		case "/v1/hello":
			w.Write([]byte(`{"smithy":true,"version":"0.23.0"}`))
		default:
			w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()
	f := tmpStore(t)
	run := func(args ...string) (int, string) {
		var out, errw strings.Builder
		code := Cmd(args, Env{File: f, Out: &out, Err: &errw, Host: func() string { return "laptop" }})
		return code, out.String() + errw.String()
	}
	if c, o := run("add", "home", srv.URL, "--code", "abcd-2345"); c != 0 || !strings.Contains(o, "paired") {
		t.Fatalf("%d %s", c, o)
	}
	l, _ := Load(f)
	if l.Servers[0].Token != "smd_x" {
		t.Fatalf("%+v", l)
	}
	if c, o := run("use", "home"); c != 0 || !strings.Contains(o, "home") {
		t.Fatalf("%d %s", c, o)
	}
	c, o := run("list")
	if c != 0 || !strings.Contains(o, "* home") || !strings.Contains(o, "online 0.23.0") || !strings.Contains(o, "  local") {
		t.Fatalf("%d %s", c, o)
	}
	if strings.Contains(o, "smd_x") {
		t.Fatal("list never prints tokens")
	}
	if c, _ := run("rm", "home"); c != 0 {
		t.Fatal(c)
	}
	if c, _ := run("bogus"); c != 2 {
		t.Fatal(c)
	}
}

func TestPrivateURL(t *testing.T) {
	for u, want := range map[string]bool{"https://x.example": true, "http://127.0.0.1:7478": true, "http://100.64.0.2:7478": true,
		"http://box.tail1.ts.net": true, "http://192.168.1.5:7478": false, "http://example.com": false} {
		if PrivateURL(u) != want {
			t.Fatalf("%s: want %v", u, want)
		}
	}
}
