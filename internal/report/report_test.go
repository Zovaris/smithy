package report

import (
	"strings"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/events"
	"github.com/Zovaris/smithy/internal/usage"
)

func at(d, h int) time.Time { return time.Date(2026, 11, d, h, 0, 0, 0, time.UTC) }

func fixture() []events.Event {
	c1, c2 := 1.25, 0.75
	return []events.Event{
		{T: at(10, 9), Kind: "merged", Batch: "old", Item: "01", PR: "https://x/pr/1"},
		{T: at(16, 9), Kind: "stage", Batch: "b", Item: "01", Detail: "working"},
		{T: at(16, 11), Kind: "usage", Batch: "b", Item: "01", Usage: &usage.Usage{CostUSD: &c1}},
		{T: at(16, 12), Kind: "stage", Batch: "b", Item: "01", PR: "https://x/pr/2", Detail: "pr_open"},
		{T: at(17, 9), Kind: "merged", Batch: "b", Item: "01", PR: "https://x/pr/2"},
		{T: at(18, 9), Kind: "stage", Batch: "b", Item: "02", Detail: "working"},
		{T: at(18, 10), Kind: "usage", Batch: "b", Item: "02", Account: "work", Usage: &usage.Usage{CostUSD: &c2}},
		{T: at(18, 10), Kind: "stage", Batch: "b", Item: "02", Detail: "failed"},
		{T: at(19, 9), Kind: "orchestrator_failed", Detail: "exit 1"},
		{T: at(20, 9), Kind: "stage", Batch: "b", Item: "03", Detail: "working"},
		{T: at(20, 10), Kind: "stage", Batch: "b", Item: "03", Detail: "needs_decision"},
		{T: at(20, 11), Kind: "stage", Batch: "b", Item: "04", Detail: "ready", PR: "https://x/pr/4"},
		{T: at(21, 9), Kind: "stage", Batch: "b", Item: "05", Detail: "needs_decision"},
		{T: at(21, 10), Kind: "stage", Batch: "b", Item: "05", Detail: "working"},
	}
}

func TestWeekCountsShippedFailedOpenAndSpend(t *testing.T) {
	r := Week(fixture(), at(23, 9))
	if len(r.Shipped) != 1 || r.Shipped[0].PR != "https://x/pr/2" {
		t.Fatalf("shipped = %+v, want only pr/2 (pr/1 is before the week)", r.Shipped)
	}
	if len(r.Failed) != 2 {
		t.Fatalf("failed = %+v, want b/02 and the orchestrator", r.Failed)
	}
	if len(r.Open) != 2 || r.Open[0].Item != "03" || r.Open[1].Item != "04" {
		t.Fatalf("open = %+v, want b/03 decision and b/04 merge (b/05 moved on)", r.Open)
	}
	if len(r.Harness) != 2 {
		t.Fatalf("harness rows = %+v", r.Harness)
	}
	h := r.Harness[0]
	if h.Name != "claude" || h.Runs != 1 || h.Cost == nil || *h.Cost != 1.25 || h.Time != 3*time.Hour {
		t.Fatalf("claude row = %+v", h)
	}
	if r.Harness[1].Name != "claude (work)" || r.Harness[1].Time != time.Hour {
		t.Fatalf("account row = %+v", r.Harness[1])
	}
}

func TestTextIsPlain(t *testing.T) {
	s := Week(fixture(), at(23, 9)).Text()
	for _, want := range []string{"Week of 16 Nov 2026", "Shipped: 1 PR merged", "https://x/pr/2", "Failed: 2",
		"orchestrator failed: exit 1", "Open decisions: 2", "claude: 1 run, 3h0m, $1.25", "Total known cost: $2.00"} {
		if !strings.Contains(s, want) {
			t.Errorf("text missing %q:\n%s", want, s)
		}
	}
}

func TestEmptyWeekSaysSo(t *testing.T) {
	s := Week(nil, at(23, 9)).Text()
	if !strings.Contains(s, "Nothing shipped") || !strings.Contains(s, "No open decisions") {
		t.Fatalf("empty text:\n%s", s)
	}
}

func TestWeekStartIsLastMonday(t *testing.T) {
	if got := Start(at(23, 9)); !got.Equal(at(16, 0)) {
		t.Fatalf("Start(Mon 23) = %v, want Mon 16 00:00", got)
	}
	if got := Start(at(25, 9)); !got.Equal(at(16, 0)) {
		t.Fatalf("Start(Wed 25) = %v, want Mon 16 00:00 (last full week)", got)
	}
}

func TestMainWeekPrintsReport(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	var out, errb strings.Builder
	if code := Main([]string{"week"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "Week of") {
		t.Fatalf("out = %q", out.String())
	}
	if code := Main([]string{"month"}, &out, &errb); code != 2 {
		t.Fatalf("unknown subcommand exit = %d, want 2", code)
	}
}

func TestDueOnlyMondayFromNine(t *testing.T) {
	if Due(time.Date(2026, 11, 23, 8, 59, 0, 0, time.UTC)) || Due(time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)) {
		t.Fatal("due outside Monday 09:00+")
	}
	if !Due(time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)) {
		t.Fatal("not due Monday 09:00")
	}
}
