//go:build harness

package chat

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

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
	for _, s := range []string{"Settings", "Look", "Transcript", "Sidebar", "Theme", "Serve", "Advanced", "‹ "} {
		if !strings.Contains(out, s) {
			t.Fatalf("%q missing:\n%s", s, out)
		}
	}
	m = typeIn(m, "theme")
	if out := screen(m); !strings.Contains(out, "1 shown") || strings.Contains(out, "Advanced") {
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

func TestTabTitleIsShortAndDropsOpenings(t *testing.T) {
	for in, want := range map[string]string{
		"please read the file go.mod and tell me which Charm libraries it uses": "Read the file go.mod…",
		"fix login": "Fix login",
		"can you supercalifragilisticexpialidocious-thing": "Supercalifr…cious-thing",
	} {
		if got := tabTitle(in); got != want || ansi.StringWidth(got) > 24 {
			t.Errorf("tabTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConfigAdvancedOpensAndBooleansReadOnOff(t *testing.T) {
	m := newChatTest(0)
	m.width, m.height = 140, 40
	m.cfg = &fakeSettings{}
	m = cpress(typeIn(m, "/config"), ckey("enter"))
	if strings.Contains(screen(m), "Paused") {
		t.Fatal("Advanced starts collapsed")
	}
	rows := m.configRows()
	m.pal.sel = len(rows) - 1
	m = cpress(m, ckey("enter"))
	if out := screen(m); !strings.Contains(out, "Paused") || !strings.Contains(out, "off") || strings.Contains(out, "false") {
		t.Fatalf("Advanced open, booleans on/off:\n%s", out)
	}
}

func TestStreamedPartsBecomeTheAnswerOnce(t *testing.T) {
	m := newChatTest(0)
	m.width, m.height = 140, 40
	m.current = "s1"
	m.push(chatMsg{who: "you", text: "go"})
	m.push(chatMsg{who: "daemon", text: "Reading it."})
	m.push(chatMsg{who: "daemon", text: "It uses four libraries."})
	if !m.adoptReply("s1", "Reading it.\n\nIt uses four libraries.") {
		t.Fatal("the reply equals the streamed parts")
	}
	if out := screen(m); strings.Count(out, "four libraries") != 1 {
		t.Fatalf("answer shown once:\n%s", out)
	}
	if m.msgs[1].who != "agent" || m.adoptReply("s1", "something else") {
		t.Fatal("parts become the agent's; a different reply is not adopted")
	}
}

// notes is every system note the open session holds, one per line: the empty screen shows only
// the latest on the status line.
func notes(m chatView) string {
	var out []string
	for _, c := range m.msgs {
		if c.kind == "system" || c.who == "daemon" && c.tool == nil && c.sh == nil {
			out = append(out, c.text)
		}
	}
	return strings.Join(out, "\n")
}

func TestHomeSplitsWideAndStacksNarrow(t *testing.T) {
	for w, split := range map[int]bool{80: false, 120: false, 200: true} {
		m := newChatTest(0)
		m.width, m.height = w, 40
		out := screen(m)
		if got := strings.Contains(out, "Pick up where you left off"); got != split {
			t.Errorf("%d cols: split %v, want %v:\n%s", w, got, split, out)
		}
	}
}

func TestAgentTabsOffShowAChipThatOpensTheAgents(t *testing.T) {
	m := newChatTest(0)
	m.width, m.height = 120, 30
	started(&m, "s-1", "fix the login form")
	started(&m, "s-2", "write release notes")
	m.open("s-1")
	lines := strings.Split(screen(m), "\n")
	if !strings.Contains(lines[0], "agents 2") || strings.Contains(lines[1], "Write release notes") {
		t.Fatalf("tabs off: chip in the header, no tab strip:\n%s", strings.Join(lines[:3], "\n"))
	}
	if !strings.Contains(screen(m), "ctrl+n/p sessions") {
		t.Fatal("the footer names ctrl+n/p for sessions")
	}
	m = cpress(m, tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	if m.current != "s-2" {
		t.Fatalf("ctrl+n still switches sessions, at %q", m.current)
	}
	x := strings.Index(lines[0], "agents 2")
	m = click(m, ansi.StringWidth(lines[0][:x])+1, 0)
	if m.overlay != overlayAgents {
		t.Fatal("a click on the chip opens the agents panel")
	}
}

func TestAskPanelLeavesTheInputWholeWithAGap(t *testing.T) {
	m, _ := askScene()
	m.setPending([]api.Decision{{ID: "d1", Question: "Ship it?", Options: []string{"yes", "no"}}})
	m = chatStep(m, ckey("enter"))
	lines := strings.Split(screen(m), "\n")
	in := -1
	for i, l := range lines {
		if strings.Contains(l, "for commands") {
			in = i
		}
	}
	comp := m.composer(m.tw())
	if in < 3 || strings.TrimSpace(lines[in-2]) != "" || !strings.Contains(strings.Join(lines[in-5:in-2], ""), "esc dismiss") {
		t.Fatalf("one blank row between the ask panel and the input:\n%s", strings.Join(lines[max(in-6, 0):], "\n"))
	}
	if len(comp) < 4 || strings.TrimSpace(lines[in-1]) == "" || strings.TrimSpace(lines[in+1]) == "" {
		t.Fatalf("the input keeps its rows:\n%s", strings.Join(lines[in-1:], "\n"))
	}
}
