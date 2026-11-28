package stats

import (
	"strings"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/events"
	"github.com/Zovaris/smithy/internal/usage"
	"github.com/charmbracelet/x/ansi"
)

func TestHistory(t *testing.T) {
	utc(t)
	cost := 1.25
	day := func(d int, h int) time.Time {
		return fixedNow.AddDate(0, 0, -d).Truncate(24 * time.Hour).Add(time.Duration(h) * time.Hour)
	}
	es := []events.Event{
		{T: day(12, 9), Kind: "launched", Source: "linear", ID: "TES-1", Batch: "a", Item: "01", Detail: "Old fix"},
		{T: day(12, 10), Kind: "usage", Batch: "a", Item: "01", Usage: &usage.Usage{CostUSD: &cost}},
		{T: day(12, 11), Kind: "merged", Batch: "a", Item: "01", PR: "https://github.com/o/r/pull/3"},
		{T: day(2, 9), Kind: "launched", Source: "github", ID: "#7", Batch: "b", Item: "02", Detail: "Blocked thing"},
		{T: day(2, 12), Kind: "stage", Batch: "b", Item: "02", Detail: "blocked"},
		{T: day(0, 9), Kind: "launched", Source: "github", ID: "#8", Batch: "c", Item: "03", Detail: "Today"},
		{T: day(0, 10), Kind: "merged", Batch: "c", Item: "03", PR: "https://github.com/o/r/pull/4"},
	}
	m := newTest(160, 48, func(now time.Time) Snapshot { return Snapshot{At: now, History: es, Events: es[3:]} })
	m, _ = press(m, "0")
	if m.view != vHistory {
		t.Fatalf("0 opens %v, want History", m.view)
	}
	rs := m.rows(vHistory)
	var got []string
	for _, r := range rs {
		var cs []string
		for _, c := range r.cells {
			cs = append(cs, c.s)
		}
		got = append(got, strings.Join(cs, "|"))
	}
	all := strings.Join(got, "\n")
	for _, want := range []string{"2026-11-21", "#7|Blocked thing|-|blocked", "2026-11-11", "TES-1|Old fix|#3|merged|$1.25"} {
		if !strings.Contains(all, want) {
			t.Errorf("History lacks %q:\n%s", want, all)
		}
	}
	if strings.Contains(all, "Today") {
		t.Errorf("History shows today's work:\n%s", all)
	}
	if strings.Index(all, "2026-11-21") > strings.Index(all, "2026-11-11") {
		t.Errorf("History is not newest first:\n%s", all)
	}
	if out := ansi.Strip(m.render()); !strings.Contains(out, "History") || !strings.Contains(out, "Old fix") {
		t.Fatalf("History view does not render:\n%s", out)
	}
}

func TestHistoryShowsAutoPROutcomes(t *testing.T) {
	utc(t)
	day := func(d int, h int) time.Time {
		return fixedNow.AddDate(0, 0, -d).Truncate(24 * time.Hour).Add(time.Duration(h) * time.Hour)
	}
	es := []events.Event{
		{T: day(3, 9), Kind: "launched", Source: "github", ID: "#5", Batch: "a", Item: "01", Detail: "Sent back"},
		{T: day(3, 10), Kind: "stage", Batch: "a", Item: "01", PR: "https://github.com/o/r/pull/5", Detail: "pr_open"},
		{T: day(3, 11), Kind: "outcome", Batch: "a", Item: "01", PR: "https://github.com/o/r/pull/5", Detail: "changes_requested"},
		{T: day(2, 9), Kind: "launched", Source: "github", ID: "#6", Batch: "a", Item: "02", Detail: "Turned down"},
		{T: day(2, 11), Kind: "outcome", Batch: "a", Item: "02", PR: "https://github.com/o/r/pull/6", Detail: "closed"},
	}
	m := newTest(160, 48, func(now time.Time) Snapshot { return Snapshot{At: now, History: es} })
	var got []string
	for _, r := range m.rows(vHistory) {
		var cs []string
		for _, c := range r.cells {
			cs = append(cs, c.s)
		}
		got = append(got, strings.Join(cs, "|"))
	}
	all := strings.Join(got, "\n")
	for _, want := range []string{"#5|Sent back|#5|changes requested", "#6|Turned down|#6|closed"} {
		if !strings.Contains(all, want) {
			t.Errorf("History lacks %q:\n%s", want, all)
		}
	}
}
