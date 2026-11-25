package bench

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/usage"
)

func cost(c float64) *float64 { return &c }

func fixtureRuns(t *testing.T) (string, Result, Result) {
	dir := t.TempDir()
	a := Result{ID: "20261101-100000.000-claude", Agent: "claude", Skills: "v0.21.0", Started: time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC), Seconds: 30,
		Passed: 1, Total: 3, PassRate: 1.0 / 3, Usage: usage.Usage{Input: 300, Output: 30, CostUSD: cost(0.3)},
		Tasks: []TaskResult{
			{ID: "bug-a", Kind: "bug", Pass: true, Seconds: 10, Usage: usage.Usage{Input: 100, CostUSD: cost(0.1)}},
			{ID: "bug-b", Kind: "bug", Pass: false, Seconds: 10, Usage: usage.Usage{Input: 100, CostUSD: cost(0.1)}},
			{ID: "only-a", Kind: "docs", Pass: false, Seconds: 10},
		}}
	b := Result{ID: "20261102-100000.000-claude", Agent: "claude", Skills: "v0.22.0", Started: time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC), Seconds: 20,
		Passed: 2, Total: 3, PassRate: 2.0 / 3, Usage: usage.Usage{Input: 150, Output: 15, CostUSD: cost(0.15)},
		Tasks: []TaskResult{
			{ID: "bug-a", Kind: "bug", Pass: false, Seconds: 5, Usage: usage.Usage{Input: 50, CostUSD: cost(0.05)}},
			{ID: "bug-b", Kind: "bug", Pass: true, Seconds: 5, Usage: usage.Usage{Input: 50, CostUSD: cost(0.05)}},
			{ID: "only-b", Kind: "feature", Pass: true, Seconds: 10},
		}}
	for _, r := range []Result{a, b} {
		if err := save(dir, r); err != nil {
			t.Fatal(err)
		}
	}
	return dir, a, b
}

func TestCompareFlagsRegressionsFixesAndDeltas(t *testing.T) {
	_, a, b := fixtureRuns(t)
	c := Compare(a, b)
	if c.PassRateDelta < 0.33 || c.PassRateDelta > 0.34 {
		t.Fatalf("pass rate delta %v", c.PassRateDelta)
	}
	if c.SecondsDelta != -10 || c.TokensDelta != -165 || c.CostDelta == nil || *c.CostDelta > -0.149 || *c.CostDelta < -0.151 {
		t.Fatalf("deltas %+v", c)
	}
	if strings.Join(c.Regressed, ",") != "bug-a" || strings.Join(c.Fixed, ",") != "bug-b" {
		t.Fatalf("regressed %v fixed %v", c.Regressed, c.Fixed)
	}
	if strings.Join(c.OnlyA, ",") != "only-a" || strings.Join(c.OnlyB, ",") != "only-b" {
		t.Fatalf("only %v %v", c.OnlyA, c.OnlyB)
	}
	if len(c.Rows) != 4 || c.Rows[0].ID != "bug-a" || c.Rows[0].A == nil || c.Rows[2].B != nil {
		t.Fatalf("rows %+v", c.Rows)
	}
}

func TestCostDeltaUnknownWhenEitherSideHasNoCost(t *testing.T) {
	_, a, b := fixtureRuns(t)
	b.Usage.CostUSD = nil
	if c := Compare(a, b); c.CostDelta != nil {
		t.Fatalf("cost delta %v, want unknown", *c.CostDelta)
	}
}

func TestRunsListsNewestFirstAndFindResolvesRefs(t *testing.T) {
	dir, a, b := fixtureRuns(t)
	os.WriteFile(filepath.Join(dir, "junk.json"), []byte("{"), 0o644)
	rs, err := Runs(dir)
	if err != nil || len(rs) != 2 || rs[0].ID != b.ID {
		t.Fatalf("runs %v %v", rs, err)
	}
	for ref, want := range map[string]string{
		b.ID: b.ID, "latest": b.ID, "latest~1": a.ID, "20261101": a.ID,
		filepath.Join(dir, a.ID+".json"): a.ID,
	} {
		r, err := Find(dir, ref)
		if err != nil || r.ID != want {
			t.Errorf("Find(%q) = %q, %v; want %q", ref, r.ID, err, want)
		}
	}
	for _, bad := range []string{"2026110", "nope", "latest~5", "../x"} {
		if _, err := Find(dir, bad); err == nil {
			t.Errorf("Find(%q) resolved", bad)
		}
	}
}

func TestMainCompareAndRuns(t *testing.T) {
	dir, a, b := fixtureRuns(t)
	var out, errw bytes.Buffer
	if code := mainIn(dir, []string{"compare", a.ID, b.ID}, &out, &errw); code != 0 {
		t.Fatalf("compare exit %d: %s", code, errw.String())
	}
	s := out.String()
	for _, want := range []string{"v0.21.0", "v0.22.0", "pass rate", "+33", "regressed", "bug-a", "fixed", "bug-b", "only in a", "only-a"} {
		if !strings.Contains(s, want) {
			t.Errorf("compare output lacks %q:\n%s", want, s)
		}
	}
	out.Reset()
	if code := mainIn(dir, []string{"compare", "--json", "latest~1", "latest"}, &out, &errw); code != 0 || !strings.Contains(out.String(), `"regressed"`) {
		t.Fatalf("json exit %d: %s", code, out.String())
	}
	out.Reset()
	if code := mainIn(dir, []string{"runs"}, &out, &errw); code != 0 || !strings.Contains(out.String(), b.ID) {
		t.Fatalf("runs exit %d: %s", code, out.String())
	}
	if code := mainIn(dir, []string{"compare", a.ID}, &out, &errw); code != 2 {
		t.Fatalf("one ref exit %d, want 2", code)
	}
	if code := mainIn(dir, []string{"compare", a.ID, "nope"}, &out, &errw); code != 1 {
		t.Fatalf("bad ref exit %d, want 1", code)
	}
}
