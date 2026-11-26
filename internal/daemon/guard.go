//go:build harness

package daemon

import (
	"encoding/json"
	"net/http"

	"github.com/Zovaris/smithy/internal/guard"
)

// guardView is GET /v1/guard: the guard's effective lists and the suggested fetch allowlist the
// settings screens (TUI, web) offer to add in one action.
func (sv *Server) guardView(w http.ResponseWriter, r *http.Request) {
	rules, _ := guard.Load()
	fetch, _ := guard.LoadFetch()
	var sug []map[string]any
	_ = json.Unmarshal([]byte(guard.SuggestedJSON()), &sug)
	writeJSON(w, 200, map[string]any{"enabled": rules.Enabled, "block": rules.Block, "allow": rules.Allow,
		"fetch_block": fetch.Block, "fetch_allow": fetch.Allow, "suggested": sug})
}

// guardAddSuggested is POST /v1/guard/suggested: adds every suggested host to guard.fetch_allow.
func (sv *Server) guardAddSuggested(w http.ResponseWriter, r *http.Request) {
	if !localOnly(w, r) {
		return
	}
	if code, msg := sameOrigin(r); code != 0 {
		writeJSON(w, code, map[string]string{"error": msg})
		return
	}
	n, err := guard.AddSuggested()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "could not save guard.fetch_allow"})
		return
	}
	writeJSON(w, 200, map[string]int{"added": n})
}
