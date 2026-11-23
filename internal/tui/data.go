// Package tui is `smithy tui`: a live, read-only dashboard of the auto watcher, warden batches,
// task lists and pending decisions. It reads local files only.
package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Zovaris/smithy/internal/auto"
	"github.com/Zovaris/smithy/internal/cards"
	"github.com/Zovaris/smithy/internal/decisions"
	"github.com/Zovaris/smithy/internal/tasks"
)

// Snapshot is everything the dashboard shows, read at one moment.
type Snapshot struct {
	At        time.Time
	Watcher   Watcher
	Queue     []auto.Queued
	Batches   []Batch
	Tasks     []TaskList
	Decisions []string
	Errors    []string
}

// Watcher is the auto watcher's health and limits.
type Watcher struct {
	Alive    bool
	Polled   bool
	Sources  []string
	LastPoll time.Time
	Today    int
	Cap      int
	Hours    string
	OpenNow  bool
	Every    int
}

// Batch is one warden batch and its items.
type Batch struct {
	Name  string
	Items []Item
}

// Item is one batch item: the card row plus the item's title and model tier.
type Item struct {
	cards.Row
	Title string
	Model string
}

// TaskList is one task dir with its count per status.
type TaskList struct {
	Dir    string
	Total  int
	Counts map[string]int
	Tasks  []tasks.Task
}

var (
	taskDirs         = tasks.Dirs
	pendingDecisions = decisions.Pending
)

// Load reads the snapshot from local files; a part that fails to read is left empty and named in Errors.
func Load(now time.Time) Snapshot {
	snap := Snapshot{At: now}
	s, err := auto.LoadSettings()
	if err != nil {
		snap.Errors = append(snap.Errors, "settings: "+err.Error())
	}
	st, err := auto.Load()
	if err != nil {
		snap.Errors = append(snap.Errors, "auto state: "+err.Error())
		st = &auto.State{}
	}
	w := Watcher{Sources: s.Sources, Cap: s.MaxPerDay, Hours: s.Hours, Every: s.Every,
		Today: len(st.Launched[now.Format("2006-01-02")])}
	w.OpenNow, _ = auto.InHours(s.Hours, now)
	for _, p := range st.LastPoll {
		if p.After(w.LastPoll) {
			w.LastPoll, w.Polled = p, true
		}
	}
	w.Alive = w.Polled && now.Sub(w.LastPoll) <= time.Duration(3*s.Every)*time.Second
	snap.Watcher = w
	snap.Queue = st.Queue

	for _, name := range cards.Batches() {
		rows, err := cards.Rows(name)
		if err != nil {
			continue
		}
		meta := itemMeta(name)
		b := Batch{Name: name}
		for _, r := range rows {
			m := meta[r.ID]
			b.Items = append(b.Items, Item{Row: r, Title: m.Title, Model: or(m.Route)})
		}
		snap.Batches = append(snap.Batches, b)
	}

	for _, dir := range taskDirs() {
		ts := tasks.Load(dir)
		tl := TaskList{Dir: dir, Total: len(ts), Counts: map[string]int{}, Tasks: ts}
		for _, t := range ts {
			tl.Counts[t.Status]++
		}
		snap.Tasks = append(snap.Tasks, tl)
	}

	if d, err := pendingDecisions(); err != nil {
		snap.Errors = append(snap.Errors, "decisions: "+err.Error())
	} else {
		snap.Decisions = d
	}
	return snap
}

type meta struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Route string `json:"route"`
}

func itemMeta(batch string) map[string]meta {
	out := map[string]meta{}
	b, err := os.ReadFile(filepath.Join(cards.Home(), batch, "batch.json"))
	if err != nil {
		return out
	}
	var plan struct {
		Items []meta `json:"items"`
	}
	if json.Unmarshal(b, &plan) == nil {
		for _, m := range plan.Items {
			out[m.ID] = m
		}
	}
	return out
}

func or(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// statuses orders a task list's statuses: the common ones first, then the rest by name.
func statuses(c map[string]int) []string {
	rank := map[string]int{"doing": 0, "todo": 1, "blocked": 2, "done": 3}
	var out []string
	for k := range c {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		ri, oki := rank[out[i]]
		rj, okj := rank[out[j]]
		if oki != okj {
			return oki
		}
		if ri != rj {
			return ri < rj
		}
		return out[i] < out[j]
	})
	return out
}
