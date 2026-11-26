package tui

import (
	"sort"
	"time"

	"github.com/Zovaris/smithy/internal/events"
)

// throughput is issues launched and PRs ready or merged per hour over the last 24 h, oldest first.
func throughput(es []events.Event, now time.Time) (launched, done []int) {
	launched, done = make([]int, 24), make([]int, 24)
	start := now.Truncate(time.Hour).Add(-23 * time.Hour)
	for _, e := range es {
		if e.T.Before(start) || e.T.After(now) {
			continue
		}
		i := int(e.T.Sub(start).Hours())
		if i < 0 || i > 23 {
			continue
		}
		switch {
		case e.Kind == "launched":
			launched[i]++
		case e.Kind == "merged" || (e.Kind == "stage" && e.Detail == "ready"):
			done[i]++
		}
	}
	return
}

type ciRun struct {
	green bool
	took  time.Duration
}

// ciRuns is the last n CI results, oldest first, with how long each took since its pending.
func ciRuns(es []events.Event, n int) []ciRun {
	started := map[string]time.Time{}
	var out []ciRun
	for _, e := range es {
		if e.Kind != "ci" {
			continue
		}
		k := e.Batch + "/" + e.Item
		switch e.Detail {
		case "pending", "running", "queued", "in_progress":
			started[k] = e.T
		case "green", "pass", "success", "red", "fail", "failure":
			r := ciRun{green: e.Detail == "green" || e.Detail == "pass" || e.Detail == "success"}
			if s, ok := started[k]; ok {
				r.took = e.T.Sub(s)
				delete(started, k)
			}
			out = append(out, r)
		}
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}

// leadTimes is queued → ready and ready → merged, per item that got there.
func leadTimes(es []events.Event) (toReady, toMerge []time.Duration) {
	queued := map[string]time.Time{}
	itemQueued := map[string]time.Time{}
	ready := map[string]time.Time{}
	for _, e := range es {
		k := e.Batch + "/" + e.Item
		switch {
		case e.Kind == "queued":
			queued[e.Source+":"+e.ID] = e.T
		case e.Kind == "launched":
			if q, ok := queued[e.Source+":"+e.ID]; ok {
				itemQueued[k] = q
			} else {
				itemQueued[k] = e.T
			}
		case e.Kind == "stage" && e.Detail == "ready":
			if _, seen := ready[k]; seen {
				continue
			}
			ready[k] = e.T
			if q, ok := itemQueued[k]; ok {
				toReady = append(toReady, e.T.Sub(q))
			}
		case e.Kind == "merged":
			if r, ok := ready[k]; ok {
				toMerge = append(toMerge, e.T.Sub(r))
				delete(ready, k)
			}
		}
	}
	return
}

func percentile(ds []time.Duration, p float64) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	s := append([]time.Duration(nil), ds...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[min(int(p*float64(len(s)-1)+0.5), len(s)-1)]
}

// histogram buckets durations into n bins from 0 to the p95.
func histogram(ds []time.Duration, n int) []int {
	out := make([]int, n)
	hi := percentile(ds, 0.95)
	if hi <= 0 {
		return out
	}
	for _, d := range ds {
		i := int(float64(d) / float64(hi) * float64(n-1))
		out[max(min(i, n-1), 0)]++
	}
	return out
}
