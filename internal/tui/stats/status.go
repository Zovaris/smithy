package stats

import (
	"math"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/Zovaris/smithy/internal/tui/kit"
)

func (m Model) counts() (working, waiting, stuck, ready, ciRunning int) {
	for _, b := range m.snap.Batches {
		for _, it := range b.Items {
			switch it.Stage {
			case "working", "starting":
				working++
			case "waiting":
				waiting++
			case "stuck", "decide":
				stuck++
			case "verify":
				switch it.CI {
				case "pending", "running", "queued", "in_progress":
					ciRunning++
				case "red", "fail", "failure":
				default:
					ready++
				}
			}
		}
	}
	return
}

func (m Model) watchWord() string {
	wt := m.snap.Watcher
	switch {
	case wt.Paused:
		return "paused"
	case wt.Alive:
		return "watching"
	case wt.Polled:
		return "stale"
	}
	return "not running"
}

func (m Model) capBar(w int) string {
	t := m.th
	if t.Plain || !t.G.Unicode {
		n := int(m.capPos*float64(w) + 0.5)
		return "[" + strings.Repeat("#", n) + strings.Repeat(".", max(w-n, 0)) + "]"
	}
	return t.DitherBar(w, m.capPos)
}

// header is the shared one-row bar: the repo on the left; the DEMO tag and the watcher's state
// in the pill. Counts, the cap and PRs live in the views, so none of them repeat here. Only the
// pill's dot moves, a slow breath while items run with motion on.
func (m Model) header(w int) []string {
	t := m.th
	pill := func(bar lipgloss.Style) string {
		s := ""
		if m.snap.Demo {
			tag := lipgloss.NewStyle().Bold(true).Reverse(true)
			if !t.Plain {
				tag = lipgloss.NewStyle().Bold(true).Foreground(t.Surface).Background(t.Warn)
			}
			s = tag.Render(" DEMO ") + bar.Render(" ")
		}
		wt := m.snap.Watcher
		dot, word := t.Dim, bar.Foreground(t.Dim)
		switch {
		case wt.Paused:
			dot = t.Warn
		case wt.Alive:
			dot, word = t.Ok, bar.Foreground(t.Text)
			if m.motion && m.running() && !t.Plain && t.Ramp != nil {
				ph := float64(m.frames) / (kit.BreathSecs * kit.Fps) * 2 * math.Pi
				dot = lipgloss.Blend1D(8, t.Dim, t.Ok)[int((1-math.Cos(ph))/2*7+0.5)]
			}
		case wt.Polled:
			dot = t.Err
		}
		return s + bar.Foreground(dot).Render(t.G.On) + word.Render(" "+m.watchWord())
	}
	repo := m.snap.Repo
	if repo == "-" {
		repo = ""
	}
	return []string{t.HeadBar(w, 1, repo, pill, m.snap.At.Local().Format("15:04"))}
}
