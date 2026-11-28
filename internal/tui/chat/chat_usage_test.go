//go:build harness

package chat

import (
	"context"
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/events"
	"github.com/Zovaris/smithy/internal/usage"
)

type fakeUsage struct {
	fakeSettings
	session string
}

func (f *fakeUsage) Usage(_ context.Context, session string) (events.Report, error) {
	f.session = session
	c := 1.25
	u := usage.Usage{Input: 12000, Output: 3400, CacheRead: 56000, CostUSD: &c}
	t := events.Tokens{Total: u, Runs: 2, Agents: map[string]usage.Usage{"claude": u}, Models: map[string]usage.Usage{"haiku": u}}
	return events.Report{Session: t, Today: t, Week: t, All: t}, nil
}

func TestUsageOpensAModalWithPeriodsAgentsAndModels(t *testing.T) {
	f := &fakeUsage{}
	m := newChatTest(0)
	m.height = 50
	m.cfg = f
	m.current = "s1"
	m = cpress(typeIn(m, "/usage"), ckey("enter"))
	if f.session != "s1" {
		t.Fatalf("asked for session %q", f.session)
	}
	out := screen(m)
	for _, s := range []string{"Usage", "THIS SESSION", "TODAY", "LAST 7 DAYS", "ALL TIME", "claude", "haiku", "12.0k", "3.4k", "56.0k", "$1.25"} {
		if !strings.Contains(out, s) {
			t.Fatalf("%q missing:\n%s", s, out)
		}
	}
}

func TestUsageWithoutReportsSaysSo(t *testing.T) {
	p := usagePanel(events.Report{})
	if !strings.Contains(strings.Join(p.lines, "\n"), "no token use recorded") {
		t.Fatalf("%v", p.lines)
	}
}
