//go:build harness

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Zovaris/smithy/internal/remote"
	"github.com/Zovaris/smithy/internal/text"
)

const (
	shellTimeout  = 2 * time.Minute
	shellMaxBytes = 256 << 10
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

type cappedBuf struct {
	b   []byte
	cut bool
}

func (c *cappedBuf) Write(p []byte) (int, error) {
	if n := shellMaxBytes - len(c.b); n < len(p) {
		c.b, c.cut = append(c.b, p[:max(n, 0)]...), true
		return len(p), nil
	}
	c.b = append(c.b, p...)
	return len(p), nil
}

// sessionShell is POST /v1/sessions/{id}/shell {cmd}: the TUI's `!cmd`, run with the user's
// shell in the session folder; the output comes back here and never reaches the agent.
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
	ctx, cancel := context.WithTimeout(r.Context(), shellTimeout)
	defer cancel()
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/sh"
	}
	cmd := exec.CommandContext(ctx, sh, "-c", req.Cmd)
	cmd.Dir = dir
	var out cappedBuf
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	res := map[string]any{"output": string(out.b), "code": 0}
	if out.cut {
		res["output"] = string(out.b) + "\n… output cut at 256 KiB"
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		res["code"] = ee.ExitCode()
	} else if err != nil {
		res["code"], res["error"] = -1, text.Clean(err.Error())
	}
	writeJSON(w, 200, res)
}

var (
	pasteMu  sync.Mutex
	pasteDir string
)

var imageExt = map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp"}

// saveImage is POST /v1/images {data: base64}: a pasted or dropped image saved 0600 in a temp
// folder; the path goes into the next message the way the TUI's ctrl+v attaches one.
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
	pasteMu.Lock()
	defer pasteMu.Unlock()
	if pasteDir == "" {
		d, err := os.MkdirTemp("", "smithy-paste-")
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": text.Clean(err.Error())})
			return
		}
		pasteDir = d
	}
	p := filepath.Join(pasteDir, fmt.Sprintf("paste-%d%s", time.Now().UnixNano(), ext))
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
