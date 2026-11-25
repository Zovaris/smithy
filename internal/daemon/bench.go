//go:build harness

package daemon

import (
	"net/http"
	"strings"

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
