//go:build harness

package daemon

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/bench"
)

func (sv *Server) benchDir() string {
	if sv.BenchDir != "" {
		return sv.BenchDir
	}
	return bench.Dir()
}

// benchRuns lists the saved bench runs, newest first, without per-task results.
func (sv *Server) benchRuns(w http.ResponseWriter, r *http.Request) {
	rs, err := bench.Runs(sv.benchDir())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := []bench.Summary{}
	for _, x := range rs {
		out = append(out, bench.Summarize(x))
	}
	writeJSON(w, 200, out)
}

// benchCompare compares run b against run a; refs are ids, prefixes or latest~N, never paths.
func (sv *Server) benchCompare(w http.ResponseWriter, r *http.Request) {
	var runs [2]bench.Result
	for i, k := range []string{"a", "b"} {
		ref := r.URL.Query().Get(k)
		if ref == "" || strings.ContainsAny(ref, `/\`) {
			http.Error(w, "no such run", http.StatusNotFound)
			return
		}
		x, err := bench.Find(sv.benchDir(), ref)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		runs[i] = x
	}
	writeJSON(w, 200, bench.Compare(runs[0], runs[1]))
}

// BenchStatus is GET /v1/bench/status: the run the web started, while it runs, and how the last one ended.
type BenchStatus struct {
	Running bool   `json:"running"`
	Agent   string `json:"agent,omitempty"`
	Model   string `json:"model,omitempty"`
	Started string `json:"started,omitempty"`
	Error   string `json:"error,omitempty"`
	Log     string `json:"log,omitempty"`
}

var (
	argRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:/._@-]*$`)
)

func (sv *Server) benchLog() string { return filepath.Join(sv.benchDir(), "web-run.log") }

// benchExec runs `smithy <args>` with this binary, its output in the bench dir's web-run.log.
func (sv *Server) benchExec(args []string) error {
	if sv.BenchRun != nil {
		return sv.BenchRun(args)
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(sv.benchDir(), 0o755); err != nil {
		return err
	}
	f, err := os.Create(sv.benchLog())
	if err != nil {
		return err
	}
	defer f.Close()
	cmd := exec.CommandContext(sv.ctx(), self, args...)
	cmd.Stdout, cmd.Stderr = f, f
	return cmd.Run()
}

// benchStart is POST /v1/bench/run: `smithy bench run --agent <a> [--model <m>]` in the
// background, one at a time; its result lands in the runs list when it ends.
func (sv *Server) benchStart(w http.ResponseWriter, r *http.Request) {
	if code, msg := sameOrigin(r); code != 0 {
		writeJSON(w, code, map[string]string{"error": msg})
		return
	}
	var req struct {
		Agent string `json:"agent"`
		Model string `json:"model"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil || !argRe.MatchString(req.Agent) || (req.Model != "" && !argRe.MatchString(req.Model)) {
		writeJSON(w, 400, map[string]string{"error": "body must be {\"agent\":…,\"model\":…} with plain names"})
		return
	}
	sv.benchMu.Lock()
	if sv.benchNow.Running {
		sv.benchMu.Unlock()
		writeJSON(w, 409, map[string]string{"error": "a bench run is already going"})
		return
	}
	sv.benchNow = BenchStatus{Running: true, Agent: req.Agent, Model: req.Model, Started: time.Now().UTC().Format(time.RFC3339)}
	sv.benchMu.Unlock()
	args := []string{"bench", "run", "--agent", req.Agent}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	go func() {
		err := sv.benchExec(args)
		sv.benchMu.Lock()
		defer sv.benchMu.Unlock()
		sv.benchNow.Running = false
		if err != nil {
			sv.benchNow.Error = err.Error()
		}
	}()
	writeJSON(w, 202, map[string]string{"status": "started"})
}

func (sv *Server) benchStatus(w http.ResponseWriter, r *http.Request) {
	sv.benchMu.Lock()
	s := sv.benchNow
	sv.benchMu.Unlock()
	if b, err := os.ReadFile(sv.benchLog()); err == nil {
		if len(b) > 2000 {
			b = b[len(b)-2000:]
		}
		s.Log = string(b)
	}
	writeJSON(w, 200, s)
}
