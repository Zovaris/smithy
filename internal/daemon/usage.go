//go:build harness

package daemon

import (
	"context"
	"net/http"
	"net/url"
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

// Usage is GET /v1/usage: the token report for session ("" for none).
func (c *Client) Usage(ctx context.Context, session string) (events.Report, error) {
	var out events.Report
	err := c.call(ctx, "GET", "/v1/usage?session="+url.QueryEscape(session), nil, &out)
	return out, err
}
