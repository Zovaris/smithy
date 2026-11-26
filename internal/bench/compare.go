package bench

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Zovaris/smithy/internal/usage"
)

// Row is one task across two runs; A or B is nil when that run did not include the task.
type Row struct {
	ID   string      `json:"id"`
	Kind string      `json:"kind"`
	A    *TaskResult `json:"a"`
	B    *TaskResult `json:"b"`
}

// Comparison is run B measured against run A: positive deltas mean B is higher.
type Comparison struct {
	A             Summary  `json:"a"`
	B             Summary  `json:"b"`
	PassRateDelta float64  `json:"pass_rate_delta"`
	SecondsDelta  float64  `json:"seconds_delta"`
	TokensDelta   int64    `json:"tokens_delta"`
	CostDelta     *float64 `json:"cost_delta"` // nil when either run's cost is unknown
	Regressed     []string `json:"regressed"`  // passed in A, failed in B
	Fixed         []string `json:"fixed"`      // failed in A, passed in B
	OnlyA         []string `json:"only_a"`
	OnlyB         []string `json:"only_b"`
	Rows          []Row    `json:"rows"`
}

// Summary is a run without its per-task results.
type Summary struct {
	ID       string      `json:"id"`
	Agent    string      `json:"agent"`
	Model    string      `json:"model,omitempty"`
	Skills   string      `json:"skills,omitempty"`
	Started  string      `json:"started"`
	Seconds  float64     `json:"seconds"`
	Passed   int         `json:"passed"`
	Total    int         `json:"total"`
	PassRate float64     `json:"pass_rate"`
	Usage    usage.Usage `json:"usage"`
}

// Summarize drops a run's per-task results.
func Summarize(r Result) Summary {
	return Summary{ID: r.ID, Agent: r.Agent, Model: r.Model, Skills: r.Skills, Started: r.Started.Format("2006-01-02 15:04"),
		Seconds: r.Seconds, Passed: r.Passed, Total: r.Total, PassRate: r.PassRate, Usage: r.Usage}
}

func tokens(u usage.Usage) int64 { return u.Input + u.Output + u.CacheRead + u.CacheWrite }

// Compare measures run b against run a, task by task.
func Compare(a, b Result) Comparison {
	c := Comparison{A: Summarize(a), B: Summarize(b), PassRateDelta: b.PassRate - a.PassRate,
		SecondsDelta: b.Seconds - a.Seconds, TokensDelta: tokens(b.Usage) - tokens(a.Usage),
		Regressed: []string{}, Fixed: []string{}, OnlyA: []string{}, OnlyB: []string{}}
	if a.Usage.CostUSD != nil && b.Usage.CostUSD != nil {
		d := *b.Usage.CostUSD - *a.Usage.CostUSD
		c.CostDelta = &d
	}
	rows := map[string]*Row{}
	for i := range a.Tasks {
		t := &a.Tasks[i]
		rows[t.ID] = &Row{ID: t.ID, Kind: t.Kind, A: t}
	}
	for i := range b.Tasks {
		t := &b.Tasks[i]
		if r, ok := rows[t.ID]; ok {
			r.B = t
		} else {
			rows[t.ID] = &Row{ID: t.ID, Kind: t.Kind, B: t}
		}
	}
	for _, r := range rows {
		c.Rows = append(c.Rows, *r)
	}
	sort.Slice(c.Rows, func(i, j int) bool { return c.Rows[i].ID < c.Rows[j].ID })
	for _, r := range c.Rows {
		switch {
		case r.B == nil:
			c.OnlyA = append(c.OnlyA, r.ID)
		case r.A == nil:
			c.OnlyB = append(c.OnlyB, r.ID)
		case r.A.Pass && !r.B.Pass:
			c.Regressed = append(c.Regressed, r.ID)
		case !r.A.Pass && r.B.Pass:
			c.Fixed = append(c.Fixed, r.ID)
		}
	}
	return c
}

// Runs loads every saved run in dir, newest first; unreadable files are skipped.
func Runs(dir string) ([]Result, error) {
	ents, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rs []Result
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if r, err := Load(filepath.Join(dir, e.Name())); err == nil && r.ID != "" {
			rs = append(rs, r)
		}
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i].ID > rs[j].ID })
	return rs, nil
}

// Find resolves a run reference: a saved run's file path, its id, a unique id prefix, or
// latest / latest~N (N runs before the newest).
func Find(dir, ref string) (Result, error) {
	if strings.HasSuffix(ref, ".json") && strings.ContainsRune(ref, os.PathSeparator) {
		return Load(ref)
	}
	rs, err := Runs(dir)
	if err != nil {
		return Result{}, err
	}
	if n, ok := strings.CutPrefix(ref, "latest"); ok {
		i := 0
		if n != "" {
			s, ok := strings.CutPrefix(n, "~")
			if i, err = strconv.Atoi(s); !ok || err != nil || i < 0 {
				return Result{}, fmt.Errorf("bad run %q", ref)
			}
		}
		if i >= len(rs) {
			return Result{}, fmt.Errorf("no run %q: %d saved (smithy bench runs)", ref, len(rs))
		}
		return rs[i], nil
	}
	var hits []Result
	for _, r := range rs {
		if r.ID == ref {
			return r, nil
		}
		if ref != "" && strings.HasPrefix(r.ID, ref) {
			hits = append(hits, r)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return Result{}, fmt.Errorf("no run %q (smithy bench runs)", ref)
	}
	return Result{}, fmt.Errorf("run %q is ambiguous: %d runs match", ref, len(hits))
}

func label(s Summary) string {
	l := s.Agent
	if s.Model != "" {
		l += " " + s.Model
	}
	if s.Skills != "" {
		l += " skills " + s.Skills
	}
	return l
}

func signed(f float64, unit string) string { return fmt.Sprintf("%+.1f%s", f, unit) }

func pass(t *TaskResult) string {
	switch {
	case t == nil:
		return "-"
	case t.Pass:
		return "pass"
	}
	return "FAIL"
}

func writeCompare(out io.Writer, c Comparison) {
	fmt.Fprintf(out, "a  %s  %s  %s\nb  %s  %s  %s\n\n", c.A.ID, c.A.Started, label(c.A), c.B.ID, c.B.Started, label(c.B))
	fmt.Fprintf(out, "%-10s %12s %12s %10s\n", "", "a", "b", "b - a")
	fmt.Fprintf(out, "%-10s %12s %12s %10s\n", "pass rate",
		fmt.Sprintf("%d/%d %.0f%%", c.A.Passed, c.A.Total, c.A.PassRate*100),
		fmt.Sprintf("%d/%d %.0f%%", c.B.Passed, c.B.Total, c.B.PassRate*100), signed(c.PassRateDelta*100, "pt"))
	fmt.Fprintf(out, "%-10s %12s %12s %10s\n", "time", fmt.Sprintf("%.0fs", c.A.Seconds), fmt.Sprintf("%.0fs", c.B.Seconds), signed(c.SecondsDelta, "s"))
	fmt.Fprintf(out, "%-10s %12d %12d %+10d\n", "tokens", tokens(c.A.Usage), tokens(c.B.Usage), c.TokensDelta)
	cd := "unknown"
	if c.CostDelta != nil {
		cd = fmt.Sprintf("%+.2f$", *c.CostDelta)
	}
	fmt.Fprintf(out, "%-10s %12s %12s %10s\n\n", "cost", usage.Cost(c.A.Usage.CostUSD), usage.Cost(c.B.Usage.CostUSD), cd)
	for _, l := range []struct {
		name string
		ids  []string
	}{{"regressed", c.Regressed}, {"fixed", c.Fixed}, {"only in a", c.OnlyA}, {"only in b", c.OnlyB}} {
		if len(l.ids) > 0 {
			fmt.Fprintf(out, "%-10s %s\n", l.name, strings.Join(l.ids, ", "))
		}
	}
	fmt.Fprintln(out)
	for _, r := range c.Rows {
		var sa, sb string
		if r.A != nil {
			sa = fmt.Sprintf("%.0fs", r.A.Seconds)
		}
		if r.B != nil {
			sb = fmt.Sprintf("%.0fs", r.B.Seconds)
		}
		fmt.Fprintf(out, "%-26s %-9s %4s %5s  %4s %5s\n", r.ID, r.Kind, pass(r.A), sa, pass(r.B), sb)
	}
}

func compareMain(dir string, args []string, out, errw io.Writer) int {
	asJSON := len(args) > 0 && args[0] == "--json"
	if asJSON {
		args = args[1:]
	}
	if len(args) != 2 {
		fmt.Fprintln(errw, Usage)
		return 2
	}
	a, err := Find(dir, args[0])
	if err == nil {
		var b Result
		if b, err = Find(dir, args[1]); err == nil {
			c := Compare(a, b)
			if asJSON {
				e := json.NewEncoder(out)
				e.SetIndent("", "  ")
				_ = e.Encode(c)
			} else {
				writeCompare(out, c)
			}
			return 0
		}
	}
	fmt.Fprintln(errw, "smithy bench compare:", err)
	return 1
}

func runsMain(dir string, out, errw io.Writer) int {
	rs, err := Runs(dir)
	if err != nil {
		fmt.Fprintln(errw, "smithy bench:", err)
		return 1
	}
	for _, r := range rs {
		s := Summarize(r)
		fmt.Fprintf(out, "%-34s %3d/%-3d %4.0f%%  %6.0fs  %-8s  %s\n", r.ID, r.Passed, r.Total, r.PassRate*100, r.Seconds, usage.Cost(r.Usage.CostUSD), label(s))
	}
	if len(rs) == 0 {
		fmt.Fprintln(out, "no runs yet (smithy bench run)")
	}
	return 0
}
