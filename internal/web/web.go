//go:build harness

// Package web embeds the built web shell (web/, built by Vite into dist) for the daemon to serve
// at /. The build lands in dist/app and is not committed (docs/adr/0011); without it the
// committed dist/placeholder.html is served, so `go build` needs no Node.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var dist embed.FS

// CSP allows only the daemon's own origin; inline styles cover the header's per-cell colours.
const CSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

// Built reports whether the real web build is embedded rather than the placeholder.
func Built() bool { return built(dist) }

func built(fsys fs.FS) bool {
	_, err := fs.Stat(fsys, "dist/app/index.html")
	return err == nil
}

// Handler serves the web build: hashed assets as immutable, any other path as index.html
// (client routing). Without a build every page is the placeholder.
func Handler() http.Handler { return handler(dist) }

func handler(fsys fs.FS) http.Handler {
	sub, _ := fs.Sub(fsys, "dist/app")
	index := "index.html"
	if !built(fsys) {
		sub, _ = fs.Sub(fsys, "dist")
		index = "placeholder.html"
	}
	files := http.FileServerFS(sub)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", CSP)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			h.Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		p := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if strings.HasPrefix(p, "assets/") {
			if _, err := fs.Stat(sub, p); err != nil {
				http.NotFound(w, r)
				return
			}
			h.Set("Cache-Control", "public, max-age=31536000, immutable")
			files.ServeHTTP(w, r)
			return
		}
		h.Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, sub, index)
	})
}
