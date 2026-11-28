package events

import (
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/usage"
)

func TestTallySplitsPeriodsAgentsAndModels(t *testing.T) {
	now := time.Date(2026, 11, 27, 15, 0, 0, 0, time.Local)
	c := 0.5
	u := func(in int64, cost *float64) *usage.Usage {
		return &usage.Usage{Input: in, Output: 1, CacheRead: 2, CostUSD: cost}
	}
	es := []Event{
		{T: now.Add(-time.Hour), Kind: "usage", Usage: u(10, &c), Agent: "claude", Model: "haiku", Session: "s1"},
		{T: now.Add(-2 * time.Hour), Kind: "usage", Usage: u(20, nil), Agent: "codex", Model: "gpt-5", Session: "s2"},
		{T: now.AddDate(0, 0, -3), Kind: "usage", Usage: u(40, nil)},
		{T: now.AddDate(0, 0, -30), Kind: "usage", Usage: u(80, &c), Agent: "claude", Model: "haiku"},
		{T: now, Kind: "launch"},
	}
	r := Tally(es, now, "s1")
	if r.Session.Total.Input != 10 || r.Today.Total.Input != 30 || r.Week.Total.Input != 70 || r.All.Total.Input != 150 {
		t.Fatalf("periods: %+v %+v %+v %+v", r.Session.Total, r.Today.Total, r.Week.Total, r.All.Total)
	}
	if r.All.Agents["claude"].Input != 90 || r.All.Agents["unknown"].Input != 40 || r.All.Models["gpt-5"].Input != 20 {
		t.Fatalf("breakdown: %+v %+v", r.All.Agents, r.All.Models)
	}
	if r.All.Total.CostUSD == nil || *r.All.Total.CostUSD != 1 || r.All.Runs != 4 {
		t.Fatalf("cost or runs: %+v %d", r.All.Total, r.All.Runs)
	}
	if r := Tally(es, now, ""); r.Session.Runs != 0 {
		t.Fatalf("no session id still tallied: %+v", r.Session)
	}
}

func TestTallyKeepsThirtyDaysForTheChart(t *testing.T) {
	now := time.Date(2026, 11, 27, 15, 0, 0, 0, time.Local)
	c := 0.25
	es := []Event{
		{T: now.Add(-time.Hour), Kind: "usage", Usage: &usage.Usage{Output: 5, CostUSD: &c}},
		{T: now.AddDate(0, 0, -2), Kind: "usage", Usage: &usage.Usage{Input: 7}},
		{T: now.AddDate(0, 0, -40), Kind: "usage", Usage: &usage.Usage{Input: 9}},
	}
	r := Tally(es, now, "")
	if len(r.Days) != DaysShown || r.Days[DaysShown-1].Day != "2026-11-27" || r.Days[0].Day != "2026-10-29" {
		t.Fatalf("days: %d %+v", len(r.Days), r.Days[DaysShown-1])
	}
	if d := r.Days[DaysShown-1]; d.Runs != 1 || d.Tokens != 5 || d.Cost != 0.25 {
		t.Fatalf("today: %+v", d)
	}
	if d := r.Days[DaysShown-3]; d.Tokens != 7 || r.Month.Runs != 2 || r.All.Runs != 3 {
		t.Fatalf("two days ago %+v, month %d, all %d", d, r.Month.Runs, r.All.Runs)
	}
}
