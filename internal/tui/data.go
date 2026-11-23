// Package tui is `smithy tui`: a live, read-only dashboard of the auto watcher, warden batches,
// task lists and pending decisions. It reads local files only.
package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Zovaris/smithy/internal/auto"
	"github.com/Zovaris/smithy/internal/cards"
	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/decisions"
	"github.com/Zovaris/smithy/internal/events"
	"github.com/Zovaris/smithy/internal/tasks"
	"github.com/charmbracelet/x/ansi"
)

// Snapshot is everything the dashboard shows, read at one moment.
type Snapshot struct {
	At        time.Time
	Watcher   Watcher
	Queue     []auto.Queued
	Batches   []Batch
	Tasks     []TaskList
	Decisions []string
	Worktrees []Worktree
	Activity  []Event
	Events    []events.Event // the last 7 days of ~/.smithy/events.jsonl, oldest first
	Rates     map[string]int // API requests left per source, when the source reports it
	Repo      string
	Demo      bool
	Errors    []string
}

// Worktree is one git worktree of a repo that has batch items.
type Worktree struct {
	Path, Branch, Item string
	Age                time.Duration
	Dirty, Orphan      bool
	Ahead, Behind      int
	Status             []string // git status --short, first lines
}

// Event is one Activity line.
type Event struct {
	At         time.Time
	Kind, Text string
	URL        string
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
	Daily    []int // launches per local day, the last 14 days, oldest first
	Hourly   []int // launches per hour today, 0..23
	Paused   bool
	Team     string
	Serve    auto.ServeStatus
}

// Batch is one warden batch and its items.
type Batch struct {
	Name  string
	Items []Item
}

// Item is one batch item: the card row plus the item's title and model tier.
type Item struct {
	cards.Row
	Batch, Title, Model      string
	Repo, Speed              string
	LogTail                  []string // demo only: the log lines, when there is no log file
	Source, Branch, Worktree string
	LastBeat, Log            string
	Card                     [][2]string // every card field, sorted by name
}

// TaskList is one task dir with its count per status.
type TaskList struct {
	Dir    string
	Total  int
	Counts map[string]int
	Tasks  []tasks.Task
	Text   map[string]string // demo only: task file text by file name
}

var (
	taskDirs      = tasks.Dirs
	readEvents    = events.Read
	scanWorktrees = worktrees
	repoName      = sync.OnceValue(func() string {
		out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
		if err != nil {
			return "-"
		}
		return filepath.Base(strings.TrimSpace(string(out)))
	})
	decisionsFile    = sync.OnceValue(decisions.File)
	pendingDecisions = func() ([]string, error) { return decisions.PendingIn(decisionsFile()) }
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
	w := Watcher{Sources: s.Sources, Cap: s.MaxPerDay, Hours: s.Hours, Every: s.Every, Paused: s.Paused,
		Team:  config.Get("linear.team", ""),
		Today: len(st.Launched[now.Format("2006-01-02")])}
	w.OpenNow, _ = auto.InHours(s.Hours, now)
	for d := 13; d >= 0; d-- {
		w.Daily = append(w.Daily, len(st.Launched[now.AddDate(0, 0, -d).Format("2006-01-02")]))
	}
	w.Hourly = make([]int, 24)
	for _, l := range st.Launched[now.Format("2006-01-02")] {
		if !l.At.IsZero() {
			w.Hourly[l.At.In(now.Location()).Hour()]++
		}
	}
	for _, p := range st.LastPoll {
		if p.After(w.LastPoll) {
			w.LastPoll, w.Polled = p, true
		}
	}
	w.Alive = w.Polled && now.Sub(w.LastPoll) <= time.Duration(3*s.Every)*time.Second
	w.Serve = auto.ServeOf(now)
	w.Serve.Agent, w.Serve.SessionLog, w.Serve.Loop = clean(w.Serve.Agent), clean(w.Serve.SessionLog), clean(w.Serve.Loop)
	snap.Watcher = w
	snap.Queue = st.Queue

	for _, name := range cards.Batches() {
		rows, err := cards.Rows(name)
		if err != nil {
			continue
		}
		meta, repo := itemMeta(name)
		b := Batch{Name: name}
		dir := filepath.Join(cards.Home(), name)
		for _, r := range rows {
			m := meta[r.ID]
			model := m.Model
			if model == "" {
				model = m.Route
			}
			it := Item{Row: r, Batch: name, Title: m.Title, Model: or(model), Speed: or(m.Speed), Repo: or(repo), Source: m.Source, Branch: m.Branch,
				Worktree: m.Worktree, Log: filepath.Join(dir, r.ID+".log")}
			card := readCard(filepath.Join(dir, r.ID+".json"))
			for k, v := range card {
				it.Card = append(it.Card, [2]string{k, fmt.Sprint(v)})
			}
			sort.Slice(it.Card, func(i, j int) bool { return it.Card[i][0] < it.Card[j][0] })
			if v, ok := card["last_beat"].(string); ok {
				it.LastBeat = v
			}
			if v, ok := card["branch"].(string); ok && it.Branch == "" {
				it.Branch = v
			}
			if v, ok := card["worktree"].(string); ok && it.Worktree == "" {
				it.Worktree = v
			}
			b.Items = append(b.Items, it)
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

	snap.Worktrees = scanWorktrees(snap.Batches, now)
	snap.Activity = activity(st, snap.Batches)
	snap.Rates = st.Rate
	if es, err := readEvents(now.AddDate(0, 0, -7), 20000); err != nil {
		snap.Errors = append(snap.Errors, "events: "+err.Error())
	} else {
		snap.Events = es
	}
	snap.Repo = repoName()

	if d, err := pendingDecisions(); err != nil {
		snap.Errors = append(snap.Errors, "decisions: "+err.Error())
	} else {
		snap.Decisions = d
	}
	return sanitize(snap)
}

var c1ToESC = strings.NewReplacer("\u009b", "\x1b[", "\u009d", "\x1b]", "\u0090", "\x1bP", "\u009e", "\x1b^", "\u009f", "\x1b_", "\u0098", "\x1bX")

// clean drops escape sequences and C0/C1 control characters, so issue and card text can't drive the terminal.
func clean(s string) string {
	s = strings.Map(func(r rune) rune { // C1 controls as runes (U+009B is CSI) become ESC forms Strip knows
		if r >= 0x80 && r <= 0x9f {
			return -1
		}
		return r
	}, c1ToESC.Replace(s))
	s = ansi.Strip(s)
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t' || r == '\r':
			return ' '
		case r < 0x20 || (r >= 0x7f && r <= 0x9f):
			return -1
		}
		return r
	}, s)
}

func cleanAll(l []string) []string {
	for i := range l {
		l[i] = clean(l[i])
	}
	return l
}

// sanitize cleans every string in the snapshot.
func sanitize(s Snapshot) Snapshot {
	s.Watcher.Sources = cleanAll(s.Watcher.Sources)
	s.Watcher.Hours = clean(s.Watcher.Hours)
	for i := range s.Queue {
		q := &s.Queue[i]
		q.Source, q.Key, q.ID, q.Title, q.URL, q.Body, q.Model, q.Speed =
			clean(q.Source), clean(q.Key), clean(q.ID), clean(q.Title), clean(q.URL), clean(q.Body), clean(q.Model), clean(q.Speed)
	}
	for i := range s.Batches {
		b := &s.Batches[i]
		b.Name = clean(b.Name)
		for j := range b.Items {
			it := &b.Items[j]
			it.ID, it.Stage, it.State, it.Phase, it.Progress, it.PR, it.CI, it.Age, it.Text, it.Title, it.Model =
				clean(it.ID), clean(it.Stage), clean(it.State), clean(it.Phase), clean(it.Progress), clean(it.PR),
				clean(it.CI), clean(it.Age), clean(it.Text), clean(it.Title), clean(it.Model)
			it.Batch, it.Source, it.Branch, it.Worktree, it.LastBeat, it.Log =
				clean(it.Batch), clean(it.Source), clean(it.Branch), clean(it.Worktree), clean(it.LastBeat), clean(it.Log)
			it.Repo, it.Speed = clean(it.Repo), clean(it.Speed)
			it.LogTail = cleanAll(it.LogTail)
			for k := range it.Card {
				it.Card[k] = [2]string{clean(it.Card[k][0]), clean(it.Card[k][1])}
			}
			if it.Updated != nil {
				u := clean(*it.Updated)
				it.Updated = &u
			}
		}
	}
	for i := range s.Tasks {
		tl := &s.Tasks[i]
		tl.Dir = clean(tl.Dir)
		counts := map[string]int{}
		for k, v := range tl.Counts {
			counts[clean(k)] += v
		}
		tl.Counts = counts
		for j := range tl.Tasks {
			t := &tl.Tasks[j]
			t.File, t.NN, t.Title, t.Status, t.PR = clean(t.File), clean(t.NN), clean(t.Title), clean(t.Status), clean(t.PR)
			t.Blockers = cleanAll(t.Blockers)
		}
		for k, v := range tl.Text {
			tl.Text[k] = strings.Join(cleanAll(strings.Split(v, "\n")), "\n")
		}
	}
	for i := range s.Worktrees {
		w := &s.Worktrees[i]
		w.Path, w.Branch, w.Item = clean(w.Path), clean(w.Branch), clean(w.Item)
		w.Status = cleanAll(w.Status)
	}
	for i := range s.Events {
		e := &s.Events[i]
		e.Kind, e.Batch, e.Item, e.Source, e.ID, e.PR, e.Detail = clean(e.Kind), clean(e.Batch), clean(e.Item), clean(e.Source), clean(e.ID), clean(e.PR), clean(e.Detail)
	}
	for i := range s.Activity {
		e := &s.Activity[i]
		e.Kind, e.Text, e.URL = clean(e.Kind), clean(e.Text), clean(e.URL)
	}
	s.Watcher.Team = clean(s.Watcher.Team)
	s.Repo = clean(s.Repo)
	s.Decisions = cleanAll(s.Decisions)
	s.Errors = cleanAll(s.Errors)
	return s
}

type meta struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Route    string `json:"route"`
	Model    string `json:"model"`
	Speed    string `json:"speed"`
	Source   string `json:"source"`
	Branch   string `json:"branch"`
	Worktree string `json:"worktree"`
}

func readCard(file string) map[string]any {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	return m
}

// activity derives the feed from polls, the queue, launches and card updates, newest first.
func activity(st *auto.State, batches []Batch) []Event {
	var ev []Event
	for src, t := range st.LastPoll {
		ev = append(ev, Event{At: t, Kind: "poll", Text: "polled " + src})
	}
	for _, q := range st.Queue {
		if !q.QueuedAt.IsZero() {
			ev = append(ev, Event{At: q.QueuedAt, Kind: "queued", Text: "queued " + q.ID + " " + q.Title, URL: q.URL})
		}
	}
	for _, ls := range st.Launched {
		for _, l := range ls {
			if !l.At.IsZero() {
				to := ""
				if l.Batch != "" {
					to = " → " + l.Batch + "/" + l.Item
				}
				ev = append(ev, Event{At: l.At, Kind: "launched", Text: "launched worker for " + l.ID + to, URL: l.URL})
			}
		}
	}
	for _, b := range batches {
		for _, it := range b.Items {
			if it.Updated == nil {
				continue
			}
			t, err := time.Parse(time.RFC3339, *it.Updated)
			if err != nil {
				continue
			}
			text := b.Name + "/" + it.ID + " " + it.State
			url := ""
			if it.PR != "-" {
				text += " · PR " + shortPR(it.PR)
				url = it.PR
			}
			if it.CI != "-" {
				text += " · CI " + it.CI
			}
			ev = append(ev, Event{At: t, Kind: it.Stage, Text: text, URL: url})
		}
	}
	sort.SliceStable(ev, func(i, j int) bool { return ev[i].At.After(ev[j].At) })
	if len(ev) > 50 {
		ev = ev[:50]
	}
	return ev
}

var wtCache struct {
	sync.Mutex
	at  time.Time
	key string
	out []Worktree
}

// worktrees lists the git worktrees of every repo that holds a batch item's worktree, cached 10 s.
func worktrees(batches []Batch, now time.Time) []Worktree {
	items := map[string]Item{}
	var paths []string
	for _, b := range batches {
		for _, it := range b.Items {
			if it.Worktree != "" {
				p := filepath.Clean(it.Worktree)
				items[p] = it
				paths = append(paths, p)
			}
		}
	}
	key := strings.Join(paths, "\n")
	wtCache.Lock()
	defer wtCache.Unlock()
	if key == wtCache.key && now.Sub(wtCache.at) < 10*time.Second {
		return wtCache.out
	}
	roots := map[string]bool{}
	for _, p := range paths {
		out, err := exec.Command("git", "-C", p, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
		if err == nil {
			roots[filepath.Dir(strings.TrimSpace(string(out)))] = true
		}
	}
	var res []Worktree
	seen := map[string]bool{}
	for root := range roots {
		out, err := exec.Command("git", "-C", root, "worktree", "list", "--porcelain").Output()
		if err != nil {
			continue
		}
		var cur *Worktree
		flush := func() {
			if cur != nil && filepath.Clean(cur.Path) != filepath.Clean(root) {
				res = append(res, *cur)
			}
			cur = nil
		}
		for _, l := range strings.Split(string(out), "\n") {
			switch {
			case strings.HasPrefix(l, "worktree "):
				flush()
				cur = &Worktree{Path: strings.TrimPrefix(l, "worktree ")}
			case strings.HasPrefix(l, "branch ") && cur != nil:
				cur.Branch = strings.TrimPrefix(strings.TrimPrefix(l, "branch "), "refs/heads/")
			}
		}
		flush()
	}
	for _, p := range paths {
		if !containsPath(res, p) {
			res = append(res, Worktree{Path: p, Branch: items[p].Branch})
		}
	}
	for i := range res {
		w := &res[i]
		if seen[w.Path] {
			continue
		}
		seen[w.Path] = true
		if it, ok := items[filepath.Clean(w.Path)]; ok {
			w.Item = it.Batch + "/" + it.ID
			switch it.Stage {
			case "merged", "reported", "dropped":
				w.Orphan = true
			}
		} else {
			w.Orphan = true
		}
		if fi, err := os.Stat(w.Path); err == nil {
			w.Age = now.Sub(fi.ModTime())
			if out, err := exec.Command("git", "-C", w.Path, "rev-list", "--left-right", "--count", "@{upstream}...HEAD").Output(); err == nil {
				fmt.Sscan(string(out), &w.Behind, &w.Ahead)
			}
			if out, err := exec.Command("git", "-C", w.Path, "status", "--short").Output(); err == nil {
				lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
				if lines[0] != "" {
					w.Dirty = true
					w.Status = lines[:min(len(lines), 12)]
				}
			}
		} else {
			w.Status = []string{"missing on disk"}
		}
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Path < res[j].Path })
	wtCache.at, wtCache.key, wtCache.out = now, key, res
	return res
}

func containsPath(ws []Worktree, p string) bool {
	for _, w := range ws {
		if filepath.Clean(w.Path) == p {
			return true
		}
	}
	return false
}

// tail is the last n lines of a file, cleaned, or nil.
func tail(file string, n int) []string {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	if len(b) > 256<<10 {
		b = b[len(b)-256<<10:]
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return cleanAll(lines)
}

func itemMeta(batch string) (map[string]meta, string) {
	out := map[string]meta{}
	b, err := os.ReadFile(filepath.Join(cards.Home(), batch, "batch.json"))
	if err != nil {
		return out, ""
	}
	var plan struct {
		Repo  string `json:"repo"`
		Items []meta `json:"items"`
	}
	if json.Unmarshal(b, &plan) == nil {
		for _, m := range plan.Items {
			out[m.ID] = m
		}
	}
	repo := ""
	if plan.Repo != "" {
		repo = filepath.Base(plan.Repo)
	}
	return out, repo
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
