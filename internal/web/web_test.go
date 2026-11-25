//go:build harness

package web

import (
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"
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
	if !Built() {
		t.Skip("web UI not built (npm run build in web/)")
	}
	ents, err := dist.ReadDir("dist/app/assets")
	if err != nil || len(ents) == 0 {
		t.Fatalf("dist/app/assets is empty: %v", err)
	}
	w := httptest.NewRecorder()
	Handler().ServeHTTP(w, httptest.NewRequest("GET", "/assets/"+ents[0].Name(), nil))
	if w.Code != 200 || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("%s: %d %q", ents[0].Name(), w.Code, w.Header().Get("Cache-Control"))
	}
}

func TestHandlerPlaceholderAndBuild(t *testing.T) {
	placeholder := fstest.MapFS{"dist/placeholder.html": {Data: []byte("web UI not built")}}
	real := fstest.MapFS{
		"dist/placeholder.html":    {Data: []byte("web UI not built")},
		"dist/app/index.html":      {Data: []byte(`<div id="app"></div>`)},
		"dist/app/assets/index.js": {Data: []byte("x")},
	}
	get := func(fsys fstest.MapFS, p string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		handler(fsys).ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		return w
	}
	if built(placeholder) || !built(real) {
		t.Fatal("built() wrong")
	}
	for _, p := range []string{"/", "/board"} {
		if w := get(placeholder, p); w.Code != 200 || !strings.Contains(w.Body.String(), "web UI not built") {
			t.Errorf("placeholder %s: %d %q", p, w.Code, w.Body.String())
		}
		if w := get(real, p); w.Code != 200 || !strings.Contains(w.Body.String(), `id="app"`) {
			t.Errorf("real %s: %d %q", p, w.Code, w.Body.String())
		}
	}
	if w := get(real, "/assets/index.js"); w.Code != 200 || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("asset: %d", w.Code)
	}
	if w := get(placeholder, "/assets/index.js"); w.Code != 404 {
		t.Errorf("placeholder asset: %d", w.Code)
	}
}

// The release workflow sets SMITHY_REQUIRE_WEB_BUILD so a binary never ships the placeholder.
func TestReleaseEmbedsBuild(t *testing.T) {
	if os.Getenv("SMITHY_REQUIRE_WEB_BUILD") == "" {
		t.Skip("only in the release workflow")
	}
	if !Built() {
		t.Fatal("embedded web UI is the placeholder: run npm ci && npm run build in web/ first")
	}
}
