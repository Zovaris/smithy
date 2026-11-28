//go:build harness

package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Zovaris/smithy/internal/layout"
	"github.com/Zovaris/smithy/internal/remote"
	"github.com/Zovaris/smithy/internal/text"
)

const (
	shellTimeout  = 2 * time.Minute
	imageMaxBytes = 32 << 20
)

// writeGuard is localOnly plus sameOrigin: what writes files or runs commands on this machine.
func writeGuard(w http.ResponseWriter, r *http.Request) bool {
	if !localOnly(w, r) {
		return false
	}
	if code, msg := sameOrigin(r); code != 0 {
		writeJSON(w, code, map[string]string{"error": msg})
		return false
	}
	return true
}

// sessionShell is POST /v1/sessions/{id}/shell {cmd}: a `!cmd`, run with the user's shell in the
// session folder. The run comes back here and lands in the transcript as a session_shell event,
// so every client and a reopened chat show it; it never reaches the agent.
func (sv *Server) sessionShell(w http.ResponseWriter, r *http.Request) {
	if !writeGuard(w, r) {
		return
	}
	var req struct {
		Cmd string `json:"cmd"`
	}
	if !readBody(w, r, &req, `{"cmd":…}`) {
		return
	}
	sv.mu.Lock()
	l := sv.sessions[r.PathValue("id")]
	dir := ""
	if l != nil {
		dir = l.s.Dir
	}
	sv.mu.Unlock()
	if l == nil {
		sessionErr(w, errNoSession)
		return
	}
	if strings.TrimSpace(req.Cmd) == "" {
		writeJSON(w, 400, map[string]string{"error": "cmd is empty"})
		return
	}
	run := RunShell(r.Context(), dir, req.Cmd)
	run.ID = fmt.Sprintf("sh-%d", time.Now().UnixNano())
	if _, err := sv.Append(KindSessionShell, r.PathValue("id"), run); err != nil {
		writeJSON(w, 500, map[string]string{"error": text.Clean(err.Error())})
		return
	}
	writeJSON(w, 200, run)
}

type pastes struct {
	mu  sync.Mutex
	dir string
}

// PasteRoot holds one folder of pasted images per daemon run.
func PasteRoot() string { return filepath.Join(layout.Home(), "pastes") }

// ClosePastes removes this daemon run's pasted images.
func (sv *Server) ClosePastes() {
	sv.pastes.mu.Lock()
	defer sv.pastes.mu.Unlock()
	if sv.pastes.dir != "" {
		_ = os.RemoveAll(sv.pastes.dir)
		sv.pastes.dir = ""
	}
}

// SweepPastes removes paste folders under root untouched for maxAge, left by a daemon that did
// not exit cleanly.
func SweepPastes(root string, maxAge time.Duration) {
	ents, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range ents {
		fi, err := e.Info()
		if err == nil && e.IsDir() && time.Since(fi.ModTime()) > maxAge {
			_ = os.RemoveAll(filepath.Join(root, e.Name()))
		}
	}
}

var imageExt = map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp"}

// saveImage is POST /v1/images {data: base64}: a pasted or dropped image saved 0600 in this daemon run's
// folder under PasteRoot; the path goes into the next message the way the TUI's ctrl+v attaches one.
func (sv *Server) saveImage(w http.ResponseWriter, r *http.Request) {
	if !writeGuard(w, r) {
		return
	}
	var req struct {
		Data []byte `json:"data"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, imageMaxBytes/3*4+4<<10)).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": `body must be {"data":base64}, at most 32 MiB`})
		return
	}
	ext := imageExt[http.DetectContentType(req.Data)]
	if ext == "" {
		writeJSON(w, 400, map[string]string{"error": "not a png, jpeg, gif or webp image"})
		return
	}
	sv.pastes.mu.Lock()
	defer sv.pastes.mu.Unlock()
	if sv.pastes.dir == "" {
		err := os.MkdirAll(PasteRoot(), 0o700)
		if err == nil {
			sv.pastes.dir, err = os.MkdirTemp(PasteRoot(), "run-")
		}
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": text.Clean(err.Error())})
			return
		}
	}
	p := filepath.Join(sv.pastes.dir, fmt.Sprintf("paste-%d%s", time.Now().UnixNano(), ext))
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		_, err = f.Write(req.Data)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": text.Clean(err.Error())})
		return
	}
	writeJSON(w, 200, map[string]string{"path": p})
}

// addServer is POST /v1/servers {name, url, code}: `smithy servers add --code` from the web,
// pairing with that daemon and saving its device token in servers.json.
func (sv *Server) addServer(w http.ResponseWriter, r *http.Request) {
	if !writeGuard(w, r) {
		return
	}
	var req struct{ Name, URL, Code string }
	if !readBody(w, r, &req, `{"name":…,"url":…,"code":…}`) {
		return
	}
	if sv.ServersFile == "" {
		writeJSON(w, 503, map[string]string{"error": "saved servers are off"})
		return
	}
	u, err := remote.CleanURL(req.URL)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	me, _ := os.Hostname()
	if me == "" {
		me = "web"
	}
	tok, err := remote.Pair(r.Context(), u, req.Code, me)
	if err == nil {
		err = remote.Add(sv.ServersFile, remote.Server{Name: req.Name, URL: u, Token: tok})
	}
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": text.Clean(err.Error())})
		return
	}
	writeJSON(w, 200, map[string]string{"name": strings.ToLower(strings.TrimSpace(req.Name)), "url": u})
}

// removeServer is DELETE /v1/servers/{name}.
func (sv *Server) removeServer(w http.ResponseWriter, r *http.Request) {
	if !localOnly(w, r) {
		return
	}
	if sv.ServersFile == "" {
		writeJSON(w, 503, map[string]string{"error": "saved servers are off"})
		return
	}
	if err := remote.Remove(sv.ServersFile, r.PathValue("name")); err != nil {
		writeJSON(w, 404, map[string]string{"error": text.Clean(err.Error())})
		return
	}
	writeJSON(w, 200, map[string]bool{"found": true})
}
