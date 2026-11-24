package daemon

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestAuthorize(t *testing.T) {
	tok := "t0k" + "en-value"
	cases := []struct {
		name   string
		bearer string
		login  string
		allow  []string
		want   bool
	}{
		{"no credentials", "", "", nil, false},
		{"valid token", tok, "", nil, true},
		{"wrong token", "nope", "", nil, false},
		{"allowed login", "", "me@example.com", []string{"me@example.com"}, true},
		{"login case-insensitive", "", "Me@Example.com", []string{"me@example.com"}, true},
		{"login not allowed", "", "x@example.com", []string{"me@example.com"}, false},
		{"login with empty allow list", "", "me@example.com", nil, false},
		{"empty allow entry never matches", "", " ", []string{""}, false},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/v1/state", nil)
		r.Host = "127.0.0.1:7477"
		if c.bearer != "" {
			r.Header.Set("Authorization", "Bearer "+c.bearer)
		}
		if c.login != "" {
			r.Host = "box.tail1234.ts.net"
			r.Header.Set(LoginHeader, c.login)
		}
		if _, got := Authorize(r, tok, c.allow); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
	if _, ok := Authorize(httptest.NewRequest("GET", "/", nil), "", nil); ok {
		t.Error("an empty token must never authorize")
	}
}

func TestCheckLoopback(t *testing.T) {
	for _, a := range []string{"127.0.0.1:7477", "[::1]:7477", "localhost:1"} {
		if err := CheckLoopback(a); err != nil {
			t.Errorf("%s: %v", a, err)
		}
	}
	for _, a := range []string{"0.0.0.0:7477", ":7477", "192.168.1.2:80", "[::]:80", "example.com:80"} {
		if err := CheckLoopback(a); err == nil {
			t.Errorf("%s accepted", a)
		}
	}
}

func TestListenIsLoopback(t *testing.T) {
	ln, err := Listen(0)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if err := CheckLoopback(ln.Addr().String()); err != nil {
		t.Fatal(err)
	}
}

func TestToken(t *testing.T) {
	p := filepath.Join(t.TempDir(), "daemon.token")
	a, err := LoadToken(p, true)
	if err != nil || len(a) < 32 {
		t.Fatalf("token %q %v", a, err)
	}
	fi, _ := os.Stat(p)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
	b, _ := LoadToken(p, false)
	if a != b {
		t.Fatal("token changed on reload")
	}
	os.Chmod(p, 0o644)
	if _, err := LoadToken(p, false); err == nil {
		t.Fatal("a group- or world-readable token must be refused")
	}
}

func TestAllowList(t *testing.T) {
	got := AllowList(" a@x.com, ,B@y.com ")
	if len(got) != 2 || got[0] != "a@x.com" || got[1] != "b@y.com" {
		t.Fatalf("%q", got)
	}
}

func TestAuthorizeHost(t *testing.T) {
	tok := "t0k" + "en-value"
	cases := []struct {
		host, bearer, login string
		want                bool
	}{
		{"127.0.0.1:7477", tok, "", true},
		{"localhost:7477", tok, "", true},
		{"evil.example:7477", tok, "", false},
		{"box.tail1234.ts.net", "", "me@example.com", true},
		{"127.0.0.1:7477", "", "me@example.com", false},
		{"evil.example", "", "me@example.com", false},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/v1/state", nil)
		r.Host = c.host
		if c.bearer != "" {
			r.Header.Set("Authorization", "Bearer "+c.bearer)
		}
		if c.login != "" {
			r.Header.Set(LoginHeader, c.login)
		}
		if _, got := Authorize(r, tok, []string{"me@example.com"}); got != c.want {
			t.Errorf("%+v: got %v", c, got)
		}
	}
}
