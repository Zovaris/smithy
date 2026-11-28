//go:build harness

package daemon

import (
	"context"
	"testing"

	"github.com/Zovaris/smithy/internal/events"
	"github.com/Zovaris/smithy/internal/usage"
)

func TestUsageReportsSessionTokensByAgentAndModel(t *testing.T) {
	_, url, _ := sessionTest(t, func(_ context.Context, _ string, _ []Exchange, e Emit) (string, error) {
		e.Usage(usage.Usage{Input: 7, Output: 3})
		return "ok", nil
	})
	c := &Client{Base: url, Token: testToken}
	s, err := c.StartSession(context.Background(), "claude", t.TempDir(), "hi")
	if err != nil {
		t.Fatal(err)
	}
	var rep events.Report
	waitFor(t, func() bool {
		rep, err = c.Usage(context.Background(), s.ID)
		return err == nil && rep.Session.Runs == 1
	})
	if rep.Session.Total.Input != 7 || rep.Session.Agents["claude"].Output != 3 || rep.All.Total.Input < 7 {
		t.Fatalf("report %+v", rep)
	}
}
