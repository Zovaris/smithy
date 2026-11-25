//go:build harness

package web

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandler(t *testing.T) {
	h := Handler()
	for _, p := range []string{"/", "/board"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), `id="app"`) || w.Header().Get("Cache-Control") != "no-cache" {
			t.Errorf("%s: %d %q", p, w.Code, w.Header().Get("Cache-Control"))
		}
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	r.URL.Path = "/../../etc/passwd"
	h.ServeHTTP(w, r)
	if strings.Contains(w.Body.String(), "root:") {
		t.Error("path traversal")
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/", nil))
	if w.Code != 405 {
		t.Errorf("POST: %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/assets/nope.js", nil))
	if w.Code != 404 {
		t.Errorf("missing asset: %d", w.Code)
	}
}

func TestAssetsAreImmutable(t *testing.T) {
	ents, err := dist.ReadDir("dist/assets")
	if err != nil || len(ents) == 0 {
		t.Fatalf("dist/assets is empty (run npm run build in web/): %v", err)
	}
	w := httptest.NewRecorder()
	Handler().ServeHTTP(w, httptest.NewRequest("GET", "/assets/"+ents[0].Name(), nil))
	if w.Code != 200 || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("%s: %d %q", ents[0].Name(), w.Code, w.Header().Get("Cache-Control"))
	}
}
