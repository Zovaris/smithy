//go:build harness

package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Zovaris/smithy/internal/api"
	"github.com/Zovaris/smithy/internal/events"
	"github.com/Zovaris/smithy/internal/usage"
)

func TestConfigGroupsFiltersAndChangesAValue(t *testing.T) {
	f := &fakeSettings{}
	m := newChatTest(0)
	m.width, m.height = 140, 40
	m.cfg = f
	m = cpress(typeIn(m, "/config"), ckey("enter"))
	if m.overlay != overlayConfig {
		t.Fatal("/config opens the settings modal")
	}
	out := screen(m)
	for _, s := range []string{"Settings", "Look", "chat.transcript", "chat.sidebar", "ui.theme", "Serve", "auto.paused", "‹ "} {
		if !strings.Contains(out, s) {
			t.Fatalf("%q missing:\n%s", s, out)
		}
	}
	m = typeIn(m, "theme")
	if out := screen(m); !strings.Contains(out, "1 shown") || strings.Contains(out, "auto.paused") {
		t.Fatalf("filter keeps only the theme:\n%s", out)
	}
	m = cpress(m, ckey("right"))
	if m.th.Name != "frost" {
		t.Fatalf("right moves the theme on and re-themes the chat, at %q", m.th.Name)
	}
	if out := screen(m); !strings.Contains(out, "the color theme") {
		t.Fatalf("the focused setting's meaning shows:\n%s", out)
	}
}

func TestConfigLocalRowsChangeTheChat(t *testing.T) {
	m := newChatTest(0)
	m.width, m.height = 140, 40
	m = cpress(typeIn(m, "/config sidebar"), ckey("enter"))
	m = cpress(m, ckey("right"))
	if !m.sideOff {
		t.Fatal("chat.sidebar right hides the sidebar")
	}
}

func TestUsageNamesRunsWithoutAnAgent(t *testing.T) {
	m := newChatTest(0)
	m.width, m.height = 160, 50
	c := 2.0
	u := usage.Usage{Input: 10, Output: 20, CacheRead: 300, CostUSD: &c}
	tk := events.Tokens{Total: u, Runs: 3, Agents: map[string]usage.Usage{"unknown": u}, Models: map[string]usage.Usage{"": u}}
	days := make([]events.DayUse, events.DaysShown)
	for i := range days {
		days[i] = events.DayUse{Day: "2026-11-01", Cost: float64(i % 4)}
	}
	r := events.Report{Today: tk, Week: tk, Month: tk, All: tk, Days: days}
	m.overlay, m.usage, m.pal.sel = overlayUsage, &r, 1
	out := ansi.Strip(screen(m))
	for _, s := range []string{"earlier runs, agent not recorded", "earlier runs, model not recorded", "Spend per day", "$2.00", "spent", "cache read"} {
		if !strings.Contains(out, s) {
			t.Fatalf("%q missing:\n%s", s, out)
		}
	}
	if strings.Contains(out, "unknown") {
		t.Fatalf("still says unknown:\n%s", out)
	}
}

func TestAskPanelDocksAboveTheInput(t *testing.T) {
	m, _ := askScene()
	m.setPending([]api.Decision{{ID: "d1", Question: "Ship it?", Options: []string{"yes — merge now", "no"}}})
	m = chatStep(m, ckey("enter"))
	lines := strings.Split(screen(m), "\n")
	q, in := -1, -1
	for i, l := range lines {
		if strings.Contains(l, "Questions") {
			q = i
		}
		if strings.Contains(l, "for commands") {
			in = i
		}
	}
	if q < 0 || in < 0 || q > in {
		t.Fatalf("panel above the input:\n%s", screen(m))
	}
	out := screen(m)
	for _, s := range []string{"merge now", "Type your own answer", "esc dismiss"} {
		if !strings.Contains(out, s) {
			t.Fatalf("%q missing:\n%s", s, out)
		}
	}
}
