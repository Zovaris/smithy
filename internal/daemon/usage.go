//go:build harness

package daemon

import (
	"net/http"
	"time"

	"github.com/Zovaris/smithy/internal/events"
)

// tokenUsage is GET /v1/usage?session=<id>: the token use smithy stats reads from the events
// log, for the session, today, the last 7 days and all time, per agent and model.
func (sv *Server) tokenUsage(w http.ResponseWriter, r *http.Request) {
	es, err := events.Read(time.Time{}, 0)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, events.Tally(es, time.Now(), r.URL.Query().Get("session")))
}
