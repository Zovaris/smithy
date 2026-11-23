package tui

import (
	"image/color"
	"sort"
	"strings"
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

// braille2 draws two series as lines in w×h braille cells, scaled to their joint max; a cell
// takes the colour of the series with more dots in it.
func braille2(a, b []int, w, h int, ca, cb color.Color, th theme) []string {
	dw, dh := w*2, h*4
	top := 1
	for _, v := range append(append([]int{}, a...), b...) {
		top = max(top, v)
	}
	type dot struct{ a, b bool }
	grid := make([][]dot, dh)
	for i := range grid {
		grid[i] = make([]dot, dw)
	}
	plot := func(vals []int, isA bool) {
		if len(vals) < 2 {
			return
		}
		px, py := -1, -1
		for i, v := range vals {
			x := i * (dw - 1) / (len(vals) - 1)
			y := dh - 1 - v*(dh-1)/top
			if px >= 0 {
				for xx := px; xx <= x; xx++ {
					yy := py
					if x != px {
						yy = py + (y-py)*(xx-px)/(x-px)
					}
					lo, hi := min(yy, py), max(yy, py)
					if xx == x {
						lo, hi = min(y, yy), max(y, yy)
					}
					for y2 := lo; y2 <= hi; y2++ {
						if isA {
							grid[y2][xx].a = true
						} else {
							grid[y2][xx].b = true
						}
					}
				}
			}
			px, py = x, y
		}
	}
	plot(a, true)
	plot(b, false)
	bits := [4][2]rune{{0x01, 0x08}, {0x02, 0x10}, {0x04, 0x20}, {0x40, 0x80}}
	out := make([]string, h)
	for r := 0; r < h; r++ {
		var sb strings.Builder
		for c := 0; c < w; c++ {
			var ch rune = 0x2800
			na, nb := 0, 0
			for dy := 0; dy < 4; dy++ {
				for dx := 0; dx < 2; dx++ {
					d := grid[r*4+dy][c*2+dx]
					if d.a || d.b {
						ch |= bits[dy][dx]
					}
					if d.a {
						na++
					}
					if d.b {
						nb++
					}
				}
			}
			switch {
			case ch == 0x2800:
				sb.WriteRune(' ')
			case !th.g.unicode:
				g := "a"
				if nb > na {
					g = "b"
				}
				sb.WriteString(map[string]string{"a": "*", "b": "+"}[g])
			case nb > na:
				sb.WriteString(th.fg(cb).Render(string(ch)))
			default:
				sb.WriteString(th.fg(ca).Render(string(ch)))
			}
		}
		out[r] = sb.String()
	}
	return out
}
