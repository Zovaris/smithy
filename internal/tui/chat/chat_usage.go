//go:build harness

package chat

import (
	"context"
	"fmt"
	"sort"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/events"
	"github.com/Zovaris/smithy/internal/usage"
)

// usageAPI is the daemon's GET /v1/usage: the token report smithy stats reads from the events log.
type usageAPI interface {
	Usage(ctx context.Context, session string) (events.Report, error)
}

// openUsage is /usage: the token report in the info modal, the open session first.
func (m chatView) openUsage() (tea.Model, tea.Cmd) {
	api, ok := m.cfg.(usageAPI)
	if !ok {
		m.system("/usage needs a daemon this chat can reach")
		return m, nil
	}
	m.info, m.overlay, m.pal = infoPanel{title: "Usage", lines: []string{"reading token use…"}}, overlayInfo, palette{}
	id := m.current
	return m, func() tea.Msg {
		r, err := api.Usage(context.Background(), id)
		if err != nil {
			return sessionErrMsg{what: "/usage", err: err}
		}
		return settingsDoneMsg{panel: usagePanel(r)}
	}
}

// usagePanel lays the report out by period, each with its total, then per agent and per model.
func usagePanel(r events.Report) *infoPanel {
	var out []string
	for _, p := range []struct {
		head string
		t    events.Tokens
	}{{"THIS SESSION", r.Session}, {"TODAY", r.Today}, {"LAST 7 DAYS", r.Week}, {"ALL TIME", r.All}} {
		if p.t.Runs == 0 {
			continue
		}
		out = append(out, "#"+p.head, fmt.Sprintf("total · %d run%s\t%s", p.t.Runs, plural(p.t.Runs), tokenLine(p.t.Total)))
		for _, k := range byTokens(p.t.Agents) {
			out = append(out, "agent "+k+"\t"+tokenLine(p.t.Agents[k]))
		}
		for _, k := range byTokens(p.t.Models) {
			out = append(out, "model "+k+"\t"+tokenLine(p.t.Models[k]))
		}
	}
	if len(out) == 0 {
		out = []string{"no token use recorded yet: agents report it when a run or turn ends"}
	}
	return &infoPanel{title: "Usage", lines: out, scroll: true}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// tokenLine is "in 12.0k · out 3.4k · cache 56.0k · $1.25", the cost only when reported.
func tokenLine(u usage.Usage) string {
	s := "in " + kilo(u.Input) + " · out " + kilo(u.Output) + " · cache " + kilo(u.CacheRead+u.CacheWrite)
	if u.CostUSD != nil {
		s += " · " + usage.Cost(u.CostUSD)
	}
	return s
}

func kilo(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}
	return fmt.Sprint(n)
}

// byTokens is the keys of m, the most tokens first.
func byTokens(m map[string]usage.Usage) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	tot := func(u usage.Usage) int64 { return u.Input + u.Output + u.CacheRead + u.CacheWrite }
	sort.Slice(ks, func(i, j int) bool {
		if a, b := tot(m[ks[i]]), tot(m[ks[j]]); a != b {
			return a > b
		}
		return ks[i] < ks[j]
	})
	return ks
}
