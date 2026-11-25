//go:build harness

package daemon

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestWebShellAtRoot(t *testing.T) {
	_, hs := newTest(t, filepath.Join(t.TempDir(), "s.db"))
	res := do(t, "GET", hs.URL+"/", "", nil)
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || !strings.Contains(string(b), `id="app"`) {
		t.Fatalf("GET / on loopback without a token: %d %q", res.StatusCode, b)
	}
	if csp := res.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") || !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("CSP: %q", csp)
	}
	if res.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing nosniff")
	}
	if res := do(t, "GET", hs.URL+"/decisions", "", nil); res.StatusCode != 200 {
		t.Errorf("unknown path falls back to the shell: %d", res.StatusCode)
	}
	if res := do(t, "GET", hs.URL+"/assets/missing.js", "", nil); res.StatusCode != 404 {
		t.Errorf("missing asset: %d", res.StatusCode)
	}
	if res := do(t, "GET", hs.URL+"/v1/nope", "", nil); res.StatusCode != 401 {
		t.Errorf("API paths stay behind auth: %d", res.StatusCode)
	}
}

func TestWebShellHostCheck(t *testing.T) {
	_, hs := newTest(t, filepath.Join(t.TempDir(), "s.db"))
	r, _ := http.NewRequest("GET", hs.URL+"/", nil)
	r.Host = "evil.example.com"
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 401 {
		t.Errorf("rebound host: %d", res.StatusCode)
	}
	if res := do(t, "GET", hs.URL+"/", "", map[string]string{LoginHeader: "evil@example.com"}); res.StatusCode != 401 {
		t.Errorf("tailnet login not in allow: %d", res.StatusCode)
	}
	if res := do(t, "GET", hs.URL+"/", "", map[string]string{LoginHeader: "me@example.com"}); res.StatusCode != 200 {
		t.Errorf("allowed tailnet login: %d", res.StatusCode)
	}
}
