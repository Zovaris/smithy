// Package report builds the weekly report: what shipped (PRs merged), what failed, time and known
// cost per harness, and the decisions still waiting, in plain language, from the events log.
package report

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/events"
	"github.com/Zovaris/smithy/internal/usage"
)

// Line is one item in a report section.
type Line struct{ Batch, Item, PR, Detail string }

// Harness is one harness (and pool account) row: agent runs, time items spent working and the
// known cost of those runs (nil when no run reported one).
type Harness struct {
	Name string
	Runs int
	Time time.Duration
	Cost *float64
}

// Report is one week, From (a Monday 00:00) to To.
type Report struct {
	From, To time.Time
	Shipped  []Line
	Failed   []Line
	Open     []Line
	Harness  []Harness
}

// Start is the Monday 00:00 that begins the last full week before now.
func Start(now time.Time) time.Time {
	d := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	back := (int(d.Weekday()) + 6) % 7
	return d.AddDate(0, 0, -back-7)
}

// harness names the harness of a usage event: only claude runs report usage today.
func harness(e events.Event) string {
	if e.Account != "" {
		return "claude (" + e.Account + ")"
	}
	return "claude"
}

// Week is the report for the last full week before now. Open decisions are counted over all of es:
// an item whose latest stage is needs_decision or ready still waits on you.
func Week(es []events.Event, now time.Time) Report {
	r := Report{From: Start(now)}
	r.To = r.From.AddDate(0, 0, 7)
	in := func(t time.Time) bool { return !t.Before(r.From) && t.Before(r.To) }
	latest := map[string]events.Event{}
	var order []string
	started := map[string]time.Time{}
	spent := map[string]time.Duration{}
	owner := map[string]string{}
	rows := map[string]*Harness{}
	var names []string
	row := func(n string) *Harness {
		if rows[n] == nil {
			rows[n] = &Harness{Name: n}
			names = append(names, n)
		}
		return rows[n]
	}
	for _, e := range es {
		key := e.Batch + "/" + e.Item
		if e.Kind == "stage" && e.Item != "" {
			if _, ok := latest[key]; !ok {
				order = append(order, key)
			}
			latest[key] = e
		}
		if !in(e.T) {
			continue
		}
		switch {
		case e.Kind == "merged":
			r.Shipped = append(r.Shipped, Line{Batch: e.Batch, Item: e.Item, PR: e.PR})
		case e.Kind == "stage" && (e.Detail == "failed" || e.Detail == "blocked"):
			r.Failed = append(r.Failed, Line{Batch: e.Batch, Item: e.Item, PR: e.PR, Detail: e.Detail})
		case e.Kind == "orchestrator_failed":
			r.Failed = append(r.Failed, Line{Detail: "orchestrator failed: " + e.Detail})
		case e.Kind == "usage" && e.Usage != nil:
			h := row(harness(e))
			h.Runs++
			if e.Usage.CostUSD != nil {
				h.Cost = usage.Usage{CostUSD: h.Cost}.Add(usage.Usage{CostUSD: e.Usage.CostUSD}).CostUSD
			}
			if e.Item != "" && owner[key] == "" {
				owner[key] = h.Name
			}
		}
		if e.Kind == "stage" && e.Item != "" {
			if t, ok := started[key]; ok {
				spent[key] += e.T.Sub(t)
				delete(started, key)
			}
			if e.Detail == "working" {
				started[key] = e.T
			}
		}
	}
	for key, d := range spent {
		if n := owner[key]; n != "" {
			rows[n].Time += d
		}
	}
	for _, n := range names {
		r.Harness = append(r.Harness, *rows[n])
	}
	for _, key := range order {
		e := latest[key]
		if e.Detail == "needs_decision" || e.Detail == "ready" {
			r.Open = append(r.Open, Line{Batch: e.Batch, Item: e.Item, PR: e.PR, Detail: e.Detail})
		}
	}
	sort.SliceStable(r.Open, func(i, j int) bool {
		return latest[r.Open[i].Batch+"/"+r.Open[i].Item].T.Before(latest[r.Open[j].Batch+"/"+r.Open[j].Item].T)
	})
	return r
}

func hm(d time.Duration) string {
	m := int(d.Round(time.Minute) / time.Minute)
	return fmt.Sprintf("%dh%dm", m/60, m%60)
}

func plural(n int, one string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %ss", n, one)
}

func (l Line) name() string {
	if l.Item == "" {
		return l.Detail
	}
	s := l.Batch + "/" + l.Item
	if l.PR != "" {
		s += " " + l.PR
	}
	return s
}

// Text is the report in plain language, one fact per line.
func (r Report) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Week of %s to %s\n\n", r.From.Format("2 Jan 2006"), r.To.AddDate(0, 0, -1).Format("2 Jan 2006"))
	if len(r.Shipped) == 0 {
		b.WriteString("Nothing shipped this week.\n")
	} else {
		fmt.Fprintf(&b, "Shipped: %s merged\n", plural(len(r.Shipped), "PR"))
		for _, l := range r.Shipped {
			b.WriteString("  ✓ " + l.name() + "\n")
		}
	}
	if len(r.Failed) == 0 {
		b.WriteString("Nothing failed.\n")
	} else {
		fmt.Fprintf(&b, "Failed: %d\n", len(r.Failed))
		for _, l := range r.Failed {
			s := l.name()
			if l.Item != "" {
				s += " (" + l.Detail + ")"
			}
			b.WriteString("  ✗ " + s + "\n")
		}
	}
	if len(r.Harness) == 0 {
		b.WriteString("No agent runs reported time or cost.\n")
	} else {
		b.WriteString("Time and known cost per harness:\n")
		total := usage.Usage{}
		for _, h := range r.Harness {
			fmt.Fprintf(&b, "  %s: %s, %s, %s\n", h.Name, plural(h.Runs, "run"), hm(h.Time), usage.Cost(h.Cost))
			total = total.Add(usage.Usage{CostUSD: h.Cost})
		}
		b.WriteString("Total known cost: " + usage.Cost(total.CostUSD) + "\n")
	}
	if len(r.Open) == 0 {
		b.WriteString("No open decisions.\n")
	} else {
		fmt.Fprintf(&b, "Open decisions: %d\n", len(r.Open))
		for _, l := range r.Open {
			what := "decision needed"
			if l.Detail == "ready" {
				what = "ready to merge"
			}
			b.WriteString("  ? " + l.name() + " (" + what + ")\n")
		}
	}
	return b.String()
}

// Usage is the help for smithy report.
const Usage = `usage: smithy report week

week   what shipped (PRs merged), what failed, time and known cost per harness, and the
       decisions still waiting, for the last full week (Monday to Sunday). The harness build's
       daemon posts the same report to the enabled chat connectors every Monday from 09:00.
`

// Due reports whether the Monday post is due at now: Monday, from 09:00 local time.
func Due(now time.Time) bool { return now.Weekday() == time.Monday && now.Hour() >= 9 }

// Main runs smithy report.
func Main(args []string, out, errw io.Writer) int {
	if len(args) != 1 || args[0] != "week" {
		fmt.Fprint(errw, Usage)
		return 2
	}
	now := time.Now()
	es, err := events.Read(Start(now).AddDate(0, 0, -60), 0)
	if err != nil {
		fmt.Fprintln(errw, "smithy report: "+err.Error())
		return 1
	}
	fmt.Fprint(out, Week(es, now).Text())
	return 0
}
