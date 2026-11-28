//go:build harness

package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/daemon"
	"github.com/Zovaris/smithy/internal/usage"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

func headerTest() chatView {
	m := newChatTest(0)
	m.width, m.height = 160, 40
	m.dir, m.effort, m.current, m.branch = "/work/proj", "medium", "s-1", "main"
	m.setupSt = &daemon.Setup{Default: daemon.AgentChoice{Agent: "claude", Model: "haiku"}}
	m.clock = func() time.Time { return time.Date(2026, 11, 25, 9, 7, 0, 0, time.Local) }
	return m
}

func TestHeaderIsNameProjectStatePillAndClock(t *testing.T) {
	m := headerTest()
	c := 0.12
	m = chatStep(m, sev(1, daemon.KindSessionUsage, "s-1", usage.Usage{Input: 1000, Output: 234, CostUSD: &c}))
	head := m.header(160)
	if len(head) != headRows || headRows != 2 {
		t.Fatalf("header is %d rows (headRows %d), want a 2-row bar", len(head), headRows)
	}
	bar := ansi.Strip(head[0])
	for _, want := range []string{"smithy", "proj", "idle", "09:07"} {
		if !strings.Contains(bar, want) {
			t.Errorf("header lacks %q: %q", want, bar)
		}
	}
	for _, gone := range []string{"haiku", "medium", "tok", "$"} {
		if strings.Contains(bar, gone) {
			t.Errorf("header repeats %q shown elsewhere: %q", gone, bar)
		}
	}
	if !strings.HasSuffix(strings.TrimRight(bar, " "), "09:07") {
		t.Errorf("clock is not in the corner: %q", bar)
	}
}

func TestEachFactShowsOnceAcrossHeaderStatusAndFooter(t *testing.T) {
	m := headerTest()
	c := 0.12
	m = chatStep(m, sev(1, daemon.KindSessionUsage, "s-1", usage.Usage{Input: 1000, Output: 234, CostUSD: &c}))
	m.noise = append(m.noise, noise{at: time.Now(), text: "session s-1 started with claude"})
	m.sideOff = true
	all := ansi.Strip(strings.Join(m.header(160), "\n") + "\n" + m.statusLine(160) + "\n" + m.footer(160))
	for _, fact := range []string{"09:07", "proj", "main", "$0.12", "1.2K (1%)"} {
		if n := strings.Count(all, fact); n != 1 {
			t.Errorf("%q shows %d times:\n%s", fact, n, all)
		}
	}
}

func TestHeaderPillCountsRunningAndBreathesWithoutToolText(t *testing.T) {
	m := newChatView(nil, "http://127.0.0.1:7477", newTheme(false, colorprofile.TrueColor, true))
	m.width, m.height = 120, 30
	m.clock = func() time.Time { return time.Date(2026, 11, 25, 9, 7, 0, 0, time.Local) }
	m.sessions = []chatSession{{id: "s-1", state: "running", now: "Write health.go", turn: time.Now()}, {id: "s-2", state: "running"}}
	m.current = "s-1"
	bar := ansi.Strip(m.header(120)[0])
	if !strings.Contains(bar, "2 running") || strings.Contains(bar, "health.go") {
		t.Fatalf("pill: %q", bar)
	}
	frames := map[string]bool{}
	for f := 0; f < 2*fps; f++ {
		m.frame = f
		frames[m.header(120)[0]] = true
	}
	if len(frames) < 3 {
		t.Fatalf("the pill does not breathe: %d distinct frames", len(frames))
	}
	m.frame = 0
	a := m.header(120)[0]
	m.frame = 1
	if b := m.header(120)[0]; ansi.Strip(a) != ansi.Strip(b) {
		t.Fatal("the bar text moves while running")
	}
	m.sessions[0].state, m.sessions[1].state = "done", "done"
	if bar := ansi.Strip(m.header(120)[0]); !strings.Contains(bar, "idle") {
		t.Fatalf("no idle pill when done: %q", bar)
	}
}

func TestHeaderFitsNarrowTerminals(t *testing.T) {
	m := headerTest()
	for _, w := range []int{80, 60, 40, 24} {
		bar := m.header(w)[0]
		if ansi.StringWidth(bar) > w || !strings.Contains(ansi.Strip(bar), "smithy") {
			t.Errorf("width %d: %q", w, ansi.Strip(bar))
		}
	}
}

func TestLoaderIsOneSmallRowWithLiveTokRate(t *testing.T) {
	m := newChatTest(0)
	m.width, m.height = 100, 30
	m.current = "s-1"
	m.sessions = []chatSession{{id: "s-1", state: "running", turn: time.Now().Add(-2 * time.Second)}}
	m = chatStep(m, sev(1, daemon.KindSessionText, "s-1", map[string]string{"text": strings.Repeat("word ", 160)}))
	m.msgs = nil
	row := ansi.Strip(m.spinRow(100))
	if !strings.Contains(row, "tok/s") {
		t.Fatalf("no tok/s: %q", row)
	}
	if r := m.tokRate("s-1"); r < 50 || r > 150 {
		t.Fatalf("rate %.0f, want ~100 (200 tokens in 2s)", r)
	}
	lead := strings.TrimLeft(row, " ")
	if strings.ContainsAny(lead[:4], "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏") {
		t.Fatalf("loader still the braille spinner: %q", row)
	}
}

func TestClockTicksOncePerMinute(t *testing.T) {
	at := time.Date(2026, 11, 25, 9, 7, 42, 0, time.Local)
	if d := untilMinute(at); d != 18*time.Second {
		t.Fatalf("next tick in %v, want 18s", d)
	}
	m := headerTest()
	if _, cmd := m.Update(clockMsg{}); cmd == nil {
		t.Fatal("the clock stops after one tick")
	}
}

func TestRunningCountShowsOnlyInTheHeaderPill(t *testing.T) {
	m := headerTest()
	m.setPending([]daemon.Decision{{ID: "d1", Question: "Commit?", Options: []string{"commit", "not yet"}}})
	m.sessions = []chatSession{{id: "s-1", state: "running", title: "add a health endpoint"}}
	if n := strings.Count(screen(m), "running"); n != 1 {
		t.Fatalf("\"running\" shows %d times:\n%s", n, screen(m))
	}
}

func TestChatFramesAreSquare(t *testing.T) {
	g := frameGlyphs(true)
	if got := g.tl + g.tr + g.bl + g.br; got != "┌┐└┘" {
		t.Fatalf("chat frame corners %q, want square", got)
	}
	m := headerTest()
	if box := strings.Join(m.overBox(make([]string, 40), []string{"x"}, 160), "\n"); strings.ContainsAny(box, "╭╮╰╯") {
		t.Fatal("a chat overlay has rounded corners")
	}
}
