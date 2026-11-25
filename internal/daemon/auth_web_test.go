package daemon

import (
	"net/http/httptest"
	"testing"
)

func TestShellAllowed(t *testing.T) {
	allow := []string{"me@example.com"}
	cases := []struct {
		host, login string
		want        bool
	}{
		{"127.0.0.1:7477", "", true},
		{"localhost:7477", "", true},
		{"[::1]:7477", "", true},
		{"evil.example.com", "", false},
		{"box.tail1.ts.net", "", false},
		{"box.tail1.ts.net", "other@example.com", false},
		{"box.tail1.ts.net", "Me@Example.com", true},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.Host = c.host
		if c.login != "" {
			r.Header.Set(LoginHeader, c.login)
		}
		if got := ShellAllowed(r, allow); got != c.want {
			t.Errorf("%s %q: %v", c.host, c.login, got)
		}
	}
}

func TestWebURL(t *testing.T) {
	if got := (Info{Addr: "127.0.0.1:7477"}).WebURL("abc"); got != "http://127.0.0.1:7477/#token=abc" {
		t.Error(got)
	}
}
