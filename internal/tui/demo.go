package tui

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"time"

	"github.com/Zovaris/smithy/internal/auto"
	"github.com/Zovaris/smithy/internal/cards"
	"github.com/Zovaris/smithy/internal/events"
	"github.com/Zovaris/smithy/internal/tasks"
	"github.com/Zovaris/smithy/internal/usage"
)

// Demo is `smithy tui --demo`: a rich, deterministic fake world relative to now, built in memory
// (no file is read or written): 3 repos, 2 sources, 14 agents across every stage, ~30 events an
// hour for 24 h, CI history, 4 plans and 5 items that need the user.
func Demo(now time.Time) Snapshot {
	r := rand.New(rand.NewPCG(2026, 11))
	day0 := now.Truncate(time.Hour)
	repos := []string{"smithy", "atlas", "ledger-web"}
	titles := []string{"Add dark mode to the report", "Fix flaky login test", "Rate-limit the export API", "Cache board queries",
		"Paginate the audit log", "Retry webhooks with backoff", "Split the billing service", "Upgrade to Go 1.27",
		"Drop the legacy CSV importer", "Add SSO for admins", "Speed up cold start", "Typed config loader",
		"Show CI time per PR", "Archive stale branches", "Make search fuzzy", "Stream large downloads"}
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	stamp := func(t time.Time) *string { s := t.UTC().Format(time.RFC3339); return &s }
	var evs []events.Event
	add := func(e events.Event) {
		if !e.T.After(now) && e.T.After(now.Add(-7*24*time.Hour)) {
			evs = append(evs, e)
		}
	}
	// Lifecycles: queued → launched → building → PR → CI → ready → merged, ~4 an hour for a day.
	n := 0
	for h := 30; h >= 0; h-- {
		for k := 0; k < 3+r.IntN(3); k++ {
			n++
			id := fmt.Sprintf("TES-%d", 1000+n)
			src := "linear"
			if n%3 == 0 {
				src, id = "github", fmt.Sprintf("#%d", 400+n)
			}
			batch := fmt.Sprintf("auto-%s-%d", day0.Add(-time.Duration(h)*time.Hour).Format("2006-01-02"), 1+n%3)
			item := fmt.Sprintf("%02d-%s", n%20+1, repos[n%3])
			pr := fmt.Sprintf("https://github.com/acme/%s/pull/%d", repos[n%3], 1000+n)
			t := day0.Add(-time.Duration(h)*time.Hour + time.Duration(r.IntN(60))*time.Minute)
			add(events.Event{T: t, Kind: "queued", Source: src, ID: id, Detail: titles[n%len(titles)]})
			t = t.Add(time.Duration(3+r.IntN(40)) * time.Minute)
			add(events.Event{T: t, Kind: "launched", Source: src, ID: id, Batch: batch, Item: item, Detail: titles[n%len(titles)]})
			t = t.Add(2 * time.Minute)
			add(events.Event{T: t, Kind: "stage", Batch: batch, Item: item, Detail: "working"})
			t = t.Add(time.Duration(20+r.IntN(90)) * time.Minute)
			add(events.Event{T: t, Kind: "stage", Batch: batch, Item: item, PR: pr, Detail: "pr_open"})
			add(events.Event{T: t, Kind: "ci", Batch: batch, Item: item, PR: pr, Detail: "pending"})
			t = t.Add(time.Duration(4+r.IntN(12)) * time.Minute)
			ci := "green"
			if r.IntN(10) < 2 {
				ci = "red"
			}
			add(events.Event{T: t, Kind: "ci", Batch: batch, Item: item, PR: pr, Detail: ci})
			if ci == "red" {
				end := "blocked"
				if n%2 == 0 {
					end = "dropped"
				}
				add(events.Event{T: t.Add(9 * time.Minute), Kind: "stage", Batch: batch, Item: item, PR: pr, Detail: end})
			}
			if ci == "green" {
				t = t.Add(time.Duration(5+r.IntN(30)) * time.Minute)
				add(events.Event{T: t, Kind: "stage", Batch: batch, Item: item, PR: pr, Detail: "ready"})
				t = t.Add(time.Duration(20+r.IntN(200)) * time.Minute)
				add(events.Event{T: t, Kind: "merged", Batch: batch, Item: item, PR: pr, Detail: "merged"})
				add(events.Event{T: t.Add(time.Minute), Kind: "report", Source: src, ID: id, PR: pr, Detail: "merged"})
			}
		}
	}
	for m := 24 * 60; m >= 0; m -= 2 {
		t := ago(time.Duration(m) * time.Minute)
		src := "github"
		if m%4 == 0 {
			src = "linear"
		}
		add(events.Event{T: t, Kind: "poll", Source: src, Detail: fmt.Sprintf("%d issues", r.IntN(4))})
		if r.IntN(9) == 0 {
			reasons := []string{"no auto label", "project Mobile is filtered out", "state Backlog is filtered out", "cycle 41 is not current"}
			add(events.Event{T: t, Kind: "skipped", Source: src, ID: fmt.Sprintf("TES-%d", 300+m%97), Detail: reasons[r.IntN(len(reasons))] + ": " + titles[r.IntN(len(titles))]})
		}
	}
	for _, e := range evs {
		if e.Kind == "launched" {
			c := float64(20+r.IntN(260)) / 100
			add(events.Event{T: e.T.Add(40 * time.Minute), Kind: "usage", Batch: e.Batch, Item: e.Item,
				Usage: &usage.Usage{Input: 40 + int64(r.IntN(400)), Output: 2000 + int64(r.IntN(30000)), CacheRead: 300000 + int64(r.IntN(900000)), CostUSD: &c}})
		}
	}
	sort.SliceStable(evs, func(i, j int) bool { return evs[i].T.Before(evs[j].T) })

	// Agents now: 14 workers in 3 batches across every stage.
	type spec struct{ state, phase, ci string }
	specs := []spec{{"", "", ""}, {"", "", ""}, {"working", "build", ""}, {"working", "build", ""}, {"working", "plan", ""},
		{"working", "check", ""}, {"working", "check", ""}, {"pr_open", "ship", "pending"}, {"pr_open", "ship", "green"},
		{"ready", "ship", "green"}, {"ready", "ship", "green"}, {"blocked", "build", "red"}, {"needs_decision", "plan", ""}, {"merged", "done", "green"}}
	models := []string{"cheap", "mid", "strong"}
	speeds := []string{"fast", "normal", "full"}
	var batches []Batch
	for bi, repo := range repos {
		b := Batch{Name: fmt.Sprintf("auto-%s-%d", now.Format("2006-01-02"), bi+1)}
		for k := bi; k < len(specs); k += 3 {
			sp := specs[k]
			id := fmt.Sprintf("TES-%d", 200+k)
			card := map[string]any{}
			if sp.state != "" {
				card["state"] = sp.state
				card["updated"] = now.Add(-time.Duration(2+k*3) * time.Minute).UTC().Format(time.RFC3339)
			}
			if sp.state == "needs_decision" {
				card["question"] = "Postgres or SQLite for the cache?"
			}
			it := Item{Batch: b.Name, Title: titles[k], Model: models[k%3], Speed: speeds[(k+1)%3], Repo: repo,
				Source: id, Branch: fmt.Sprintf("feat/%s-%s", id, []string{"api", "ui", "db", "cache"}[k%4]),
				Worktree: fmt.Sprintf("/home/dev/worktrees/%s-%s", repo, id)}
			row := cards.Row{ID: id, State: or(sp.state), Phase: or(sp.phase), Progress: "-", PR: "-", CI: or(sp.ci), Age: "-"}
			row.Stage = map[string]string{"": "queued", "working": "working", "pr_open": "verify", "ready": "verify",
				"blocked": "stuck", "needs_decision": "decide", "merged": "merged"}[sp.state]
			if sp.state != "" {
				row.Updated = stamp(now.Add(-time.Duration(2+k*3) * time.Minute))
				beat := now.Add(-time.Duration(20+k*11) * time.Second)
				if sp.state == "blocked" {
					beat = now.Add(-38 * time.Minute)
				}
				row.LastBeat = stamp(beat)
				it.LastBeat = *row.LastBeat
				row.Age = fmt.Sprintf("%dm", 12+k*9)
			}
			if sp.ci != "" {
				row.PR = fmt.Sprintf("https://github.com/acme/%s/pull/%d", repo, 2000+k)
			}
			if sp.state == "blocked" {
				row.Text = "integration tests need a Postgres service"
			}
			if sp.state == "needs_decision" {
				row.Text = "Postgres or SQLite for the cache?"
			}
			it.Row = row
			it.Card = [][2]string{{"phase", row.Phase}, {"state", row.State}, {"updated", or(derefS(row.Updated))}}
			it.LogTail = []string{
				"[smelt] reading the task and the code it touches",
				"[smelt] plan: 3 steps, test first",
				"[go] go test ./internal/... (1 failing as expected)",
				"[edit] internal/" + repo + "/handler.go",
				"[go] go test ./... ok (2.4s)",
				"[git] commit: feat: " + titles[k],
			}
			if row.PR != "-" {
				it.LogTail = append(it.LogTail, "[gh] opened PR "+shortPR(row.PR), "[ci] checks "+sp.ci)
			}
			b.Items = append(b.Items, it)
			if sp.state != "" {
				t0 := now.Add(-time.Duration(40+k*9) * time.Minute)
				add(events.Event{T: t0.Add(-12 * time.Minute), Kind: "queued", Source: "linear", ID: id, Detail: titles[k]})
				add(events.Event{T: t0, Kind: "launched", Source: "linear", ID: id, Batch: b.Name, Item: id})
				add(events.Event{T: t0.Add(2 * time.Minute), Kind: "stage", Batch: b.Name, Item: id, Detail: "working"})
				if row.PR != "-" {
					add(events.Event{T: t0.Add(25 * time.Minute), Kind: "stage", Batch: b.Name, Item: id, PR: row.PR, Detail: "pr_open"})
					add(events.Event{T: t0.Add(25 * time.Minute), Kind: "ci", Batch: b.Name, Item: id, PR: row.PR, Detail: "pending"})
					if sp.ci != "pending" {
						add(events.Event{T: t0.Add(34 * time.Minute), Kind: "ci", Batch: b.Name, Item: id, PR: row.PR, Detail: sp.ci})
					}
				}
				if sp.state != "working" && sp.state != "pr_open" {
					add(events.Event{T: t0.Add(38 * time.Minute), Kind: map[bool]string{true: "merged", false: "stage"}[sp.state == "merged"], Batch: b.Name, Item: id, PR: row.PR, Detail: sp.state})
				}
			}
		}
		batches = append(batches, b)
	}
	sort.SliceStable(evs, func(i, j int) bool { return evs[i].T.Before(evs[j].T) })

	today := 0
	daily := make([]int, 14)
	hourly := make([]int, 24)
	for _, e := range evs {
		if e.Kind != "launched" {
			continue
		}
		d := int(now.Sub(e.T).Hours() / 24)
		if e.T.Format("2006-01-02") == now.Format("2006-01-02") {
			today++
			hourly[e.T.Hour()]++
		}
		if d < 14 {
			daily[13-d]++
		}
	}
	for i := 0; i < 12; i++ {
		if daily[i] == 0 {
			daily[i] = 18 + r.IntN(20)
		}
	}
	daily[13] = today

	var queue []auto.Queued
	for k := 0; k < 6; k++ {
		src, id := "linear", fmt.Sprintf("TES-%d", 260+k)
		if k%2 == 1 {
			src, id = "github", fmt.Sprintf("#%d", 520+k)
		}
		queue = append(queue, auto.Queued{Source: src, Key: id, ID: id, Title: titles[(k+5)%len(titles)], URL: "https://linear.app/acme/issue/" + id,
			Model: models[k%3], Speed: speeds[k%3], QueuedAt: now.Add(-time.Duration(3+k*7) * time.Minute), Body: "Steps to reproduce are in the issue."})
	}
	if len(queue) > 0 {
		queue[1].URL = "https://github.com/acme/atlas/issues/521"
	}

	var lists []TaskList
	for k, name := range []string{"v015-auto", "billing-split", "search-v2", "onboarding"} {
		tl := TaskList{Dir: "/home/dev/.smithy/plans/smithy/" + name, Counts: map[string]int{}, Text: map[string]string{}}
		for j := 0; j < 4+k; j++ {
			st := []string{"done", "done", "doing", "todo", "todo", "blocked", "done"}[(j+k)%7]
			f := fmt.Sprintf("%02d-step-%d.md", j+1, j+1)
			tl.Tasks = append(tl.Tasks, tasks.Task{File: tl.Dir + "/" + f, NN: fmt.Sprintf("%02d", j+1), Title: titles[(j+k*3)%len(titles)], Status: st, PR: "-"})
			tl.Counts[st]++
			tl.Text[f] = fmt.Sprintf("# %02d · %s\nStatus: %s\nSpeed: normal\n\n## Goal\n%s, with a test that fails first.\n\n## Done when\n- go test ./... is green\n- the CHANGELOG has a line", j+1, titles[(j+k*3)%len(titles)], st, titles[(j+k*3)%len(titles)])
		}
		tl.Total = len(tl.Tasks)
		if k != 3 {
			tl.Text["spec.md"] = fmt.Sprintf("# %s\n\n## Why\nShip %s without breaking the nightly runs. Users asked for it in three issues.\n\n"+
				"## Concepts\n- **Worker**: one agent on one task, in its own worktree.\n- **Gate**: CI must be green before review.\n\n"+
				"## Plan\n| step | owner | status |\n|---|---|---|\n| design | agent | done |\n| build | agent | doing |\n| review | you | todo |\n\n"+
				"## Done when\n1. `go test ./...` is green\n2. the CHANGELOG has a line\n3. a reviewer approved the PR\n\n```sh\nsmithy warden status\n```\n", name, name)
		}
		lists = append(lists, tl)
	}

	var wts []Worktree
	for _, b := range batches {
		for _, it := range b.Items {
			if it.Stage == "queued" {
				continue
			}
			w := Worktree{Path: it.Worktree, Branch: it.Branch, Item: b.Name + "/" + it.ID, Age: time.Duration(10+len(wts)*13) * time.Minute}
			w.Ahead = len(wts) % 4
			if len(wts)%3 == 1 {
				w.Dirty, w.Status = true, []string{" M internal/handler.go", "?? internal/handler_test.go"}
			}
			wts = append(wts, w)
		}
	}
	wts = append(wts, Worktree{Path: "/home/dev/worktrees/atlas-TES-87", Branch: "feat/TES-87-export", Age: 52 * time.Hour, Orphan: true, Behind: 14},
		Worktree{Path: "/home/dev/worktrees/smithy-TES-91", Branch: "fix/TES-91-flake", Age: 30 * time.Hour, Orphan: true, Dirty: true, Behind: 3, Status: []string{" M go.sum"}})

	w := Watcher{Alive: true, Polled: true, Sources: []string{"github", "linear"}, Team: "TES", LastPoll: now.Add(-25 * time.Second),
		Today: today, Cap: max(80, today+10), Hours: "", OpenNow: true, Every: 60, Daily: daily, Hourly: hourly,
		Serve: auto.ServeStatus{State: "running", PID: 4242, Agent: "claude", LastCycle: now.Add(-25 * time.Second), Loop: "orchestrating",
			SessionLog: "/home/dev/.smithy/auto/sessions/20261123-091500.log", SessionPID: 5150, SessionOn: true,
			SessionAt: now.Add(-38 * time.Minute), SessionDir: "/home/dev/.smithy/plans/smithy/auto-2026-11-23-1"}}
	for bi := range batches {
		for ii := range batches[bi].Items {
			it := &batches[bi].Items[ii]
			if n++; it.Stage == "queued" || n%4 == 3 {
				continue // queued, or a CLI that reports no usage: shown as "-"
			}
			c := float64(30+n*37%240) / 100
			it.Usage = &usage.Usage{Input: int64(60 + n), Output: int64(4000 + n*900), CacheRead: int64(250000 + n*41000), CostUSD: &c}
		}
	}
	w.Spend = events.Spend(evs, now, 7)
	spendCap := 120.0
	w.SpendCap = &spendCap
	snap := Snapshot{At: now, Watcher: w, Queue: queue, Batches: batches, Tasks: lists, Worktrees: wts, Events: evs,
		Rates: map[string]int{"linear": 1342}, Repo: "smithy", Demo: true,
		Decisions: []string{
			"2026-11-23 · timeout · Retry the export with backoff? → yes, 3 tries · keeps the API polite · ref: https://github.com/acme/atlas/pull/2004",
			"2026-11-23 · self · Split billing into two services? → not yet · too risky this week",
			"2026-11-23 · consult · Pin Go 1.27 in CI? → yes · matches go.mod · ref: https://github.com/acme/smithy/pull/2012 · rejected: wait for the 1.27.5 security fix",
		}}
	return sanitize(snap)
}

func derefS(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
