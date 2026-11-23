// Package wait blocks until a target on GitHub (through gh) or a warden batch has an event,
// prints one line per event and exits, so a waiting agent spends no tokens until there is work.
package wait

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/auto"
	"github.com/Zovaris/smithy/internal/cards"
	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/ghx"
)

var allEvents = []string{"checks", "merged", "closed", "conflict", "review", "comment", "push", "new", "updated"}

type watcher interface{ poll() ([]string, error) }

// firstPoll polls w up to tries times, waiting delay between tries, so a transient gh or
// GraphQL error on the first read doesn't end the wait.
func firstPoll(w watcher, tries int, delay time.Duration) ([]string, error) {
	var err error
	for i := 0; i < tries; i++ {
		var lines []string
		if lines, err = w.poll(); err == nil {
			return lines, nil
		}
		if i < tries-1 {
			time.Sleep(delay)
		}
	}
	return nil, err
}

type opts struct {
	events map[string]bool
	all    bool
}

func (o opts) on(e string) bool { return o.events[e] }

var repoRe = regexp.MustCompile(`github\.com/([^/]+/[^/]+)`)

func repoOf(ref string) string {
	if m := repoRe.FindStringSubmatch(ref); m != nil {
		return m[1]
	}
	return ""
}

func idOf(ref, pattern string) string {
	if m := regexp.MustCompile(pattern).FindStringSubmatch(ref); m != nil {
		return m[1]
	}
	return ref
}

func dash(s string) string { return strings.Join(strings.Fields(s), "-") }

type check struct {
	Name, Context, Status, Conclusion, State, StartedAt, CompletedAt string
}

// fingerprint identifies a rollup by its timestamps; "" when it has none.
func fingerprint(rollup []check) string {
	var b strings.Builder
	stamped := false
	for _, c := range rollup {
		stamped = stamped || c.StartedAt != "" || c.CompletedAt != ""
		fmt.Fprintf(&b, "%s|%s|%s|%s|%s|%s|%s;", c.Name, c.Context, c.Status, c.Conclusion, c.State, c.StartedAt, c.CompletedAt)
	}
	if !stamped {
		return ""
	}
	return b.String()
}

type checks struct {
	state  string
	failed []string
}

func summarize(rollup []check) checks {
	if len(rollup) == 0 {
		return checks{state: "none"}
	}
	var failed []string
	for _, c := range rollup {
		status := c.Status
		if status == "" {
			status = "COMPLETED"
		}
		if status != "COMPLETED" || c.State == "PENDING" || c.State == "EXPECTED" {
			return checks{state: "pending"}
		}
		res := c.Conclusion
		if res == "" {
			res = c.State
		}
		if res != "SUCCESS" && res != "NEUTRAL" && res != "SKIPPED" {
			name := c.Name
			if name == "" {
				name = c.Context
			}
			if name == "" {
				name = "?"
			}
			failed = append(failed, dash(name))
		}
	}
	if len(failed) > 0 {
		return checks{state: "red", failed: failed}
	}
	return checks{state: "green"}
}

type prState struct {
	url, state, mergeable, review, head, branch string
	num, comments, reviews                      int
	checks                                      checks
	fp                                          string
}

type prWatcher struct {
	ref   string
	last  *prState
	stale string
	o     opts
}

func newPR(ref string, o opts) *prWatcher {
	return &prWatcher{ref: ref, o: o}
}

func (w *prWatcher) read() (*prState, error) {
	var p struct {
		Number            int
		URL               string
		State             string
		Mergeable         string
		ReviewDecision    string
		HeadRefOid        string
		HeadRefName       string
		StatusCheckRollup []check
		Comments          []json.RawMessage
		Reviews           []json.RawMessage
	}
	if err := ghx.PRView(w.ref, &p); err != nil {
		return nil, err
	}
	return &prState{url: p.URL, num: p.Number, state: p.State, mergeable: p.Mergeable, review: p.ReviewDecision, head: p.HeadRefOid,
		comments: len(p.Comments), reviews: len(p.Reviews), checks: summarize(p.StatusCheckRollup), fp: fingerprint(p.StatusCheckRollup), branch: p.HeadRefName}, nil
}

func checksLine(s *prState) string {
	f := ""
	if len(s.checks.failed) > 0 {
		f = " failed=" + strings.Join(s.checks.failed, ",")
	}
	if s.mergeable == "CONFLICTING" && s.checks.state == "green" {
		return conflictLine(s)
	}
	return fmt.Sprintf("pr %d checks=%s%s %s", s.num, s.checks.state, f, s.url)
}

// conflictLine replaces checks=green for a PR that can't merge: green alone would read as done.
func conflictLine(s *prState) string {
	return fmt.Sprintf("pr %d conflict %s", s.num, s.url)
}

func finished(c checks) bool { return c.state == "green" || c.state == "red" }

func (w *prWatcher) poll() ([]string, error) {
	s, err := w.read()
	if err != nil {
		return nil, err
	}
	var out []string
	if pushedAhead(s) {
		// Right after a push GitHub may still report the old head and its checks.
		s.checks = checks{state: "pending"}
	}
	o, last := w.o, w.last
	if last != nil && s.head != last.head && last.fp != "" {
		w.stale = last.fp
	}
	if w.stale != "" {
		if s.fp == w.stale {
			s.checks = checks{state: "pending"}
		} else if s.fp != "" {
			w.stale = ""
		}
	}
	w.last = s
	if last == nil {
		switch {
		case s.state == "MERGED" && o.on("merged"):
			out = append(out, fmt.Sprintf("pr %d merged %s", s.num, s.url))
		case s.state == "CLOSED" && o.on("closed"):
			out = append(out, fmt.Sprintf("pr %d closed %s", s.num, s.url))
		case o.on("checks") && finished(s.checks):
			out = append(out, checksLine(s))
		}
		return out, nil
	}
	if s.state != last.state && s.state == "MERGED" && o.on("merged") {
		out = append(out, fmt.Sprintf("pr %d merged %s", s.num, s.url))
	}
	if s.state != last.state && s.state == "CLOSED" && o.on("closed") {
		out = append(out, fmt.Sprintf("pr %d closed %s", s.num, s.url))
	}
	if s.head != last.head && o.on("push") {
		h := s.head
		if len(h) > 7 {
			h = h[:7]
		}
		out = append(out, fmt.Sprintf("pr %d push head=%s %s", s.num, h, s.url))
	}
	if o.on("checks") && finished(s.checks) && (s.checks.state != last.checks.state || s.head != last.head) {
		out = append(out, checksLine(s))
	}
	if s.mergeable == "CONFLICTING" && last.mergeable != "CONFLICTING" && o.on("conflict") && !slices.Contains(out, conflictLine(s)) {
		out = append(out, conflictLine(s))
	}
	if (s.reviews > last.reviews || s.review != last.review) && o.on("review") {
		r := strings.ToLower(s.review)
		if r == "" {
			r = "commented"
		}
		out = append(out, fmt.Sprintf("pr %d review=%s %s", s.num, r, s.url))
	}
	if s.comments > last.comments && o.on("comment") {
		out = append(out, fmt.Sprintf("pr %d comment +%d %s", s.num, s.comments-last.comments, s.url))
	}
	return out, nil
}

type job struct{ Name, Conclusion string }

func ok(conclusion string) bool {
	return conclusion == "success" || conclusion == "skipped" || conclusion == "neutral"
}

type runWatcher struct {
	id, ref string
}

func newRun(ref string) *runWatcher {
	return &runWatcher{idOf(ref, `/runs/(\d+)`), ref}
}

func (w *runWatcher) poll() ([]string, error) {
	var r struct {
		Status, Conclusion, Name, URL string
		Jobs                          []job
	}
	if err := ghx.RunView(w.ref, &r); err != nil {
		return nil, err
	}
	if r.Status != "completed" {
		return nil, nil
	}
	var failed []string
	for _, j := range r.Jobs {
		if j.Conclusion != "" && !ok(j.Conclusion) {
			failed = append(failed, dash(j.Name))
		}
	}
	f := ""
	if len(failed) > 0 {
		f = " failed=" + strings.Join(failed, ",")
	}
	return []string{fmt.Sprintf("run %s %s%s %s", w.id, r.Conclusion, f, r.URL)}, nil
}

type branchWatcher struct{ name string }

func (w branchWatcher) poll() ([]string, error) {
	var runs []struct {
		Status, Conclusion, Name, HeadSha, CreatedAt string
	}
	if err := ghx.RunList(w.name, &runs); err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return nil, nil
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].CreatedAt > runs[j].CreatedAt })
	sha := runs[0].HeadSha
	var failed []string
	for _, r := range runs {
		if r.HeadSha != sha {
			continue
		}
		if r.Status != "completed" {
			return nil, nil
		}
		if !ok(r.Conclusion) {
			failed = append(failed, dash(r.Name))
		}
	}
	res, f := "success", ""
	if len(failed) > 0 {
		res, f = "failure", " failed="+strings.Join(failed, ",")
	}
	if len(sha) > 7 {
		sha = sha[:7]
	}
	return []string{fmt.Sprintf("branch %s %s%s head=%s", w.name, res, f, sha)}, nil
}

type item struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
	V      string `json:"v"`
}

// listWatcher reports PRs or issues opened or updated since the last run of the same scope.
type listWatcher struct {
	kind, label, me, file string
	seen                  []item
	o                     opts
}

func newList(kind, label string, o opts) (*listWatcher, error) {
	repo, err := ghx.RepoName()
	if err != nil {
		return nil, err
	}
	w := &listWatcher{kind: kind, label: label, o: o}
	if kind == "pr" {
		w.me, _ = ghx.CurrentUser()
	}
	dir := os.Getenv("TEND_HOME")
	if dir == "" {
		dir = filepath.Join(ghx.Home(), "tend")
	}
	scope := "issues"
	if kind == "pr" {
		scope = "prs"
	}
	if label != "" {
		scope += "-" + label
	}
	w.file = filepath.Join(dir, strings.ReplaceAll(repo, "/", "-")+"-"+scope+".json")
	if b, err := os.ReadFile(w.file); err == nil {
		_ = json.Unmarshal(b, &w.seen)
		if w.seen == nil {
			w.seen = []item{}
		}
	}
	return w, nil
}

func batchBranches() map[string]bool {
	out := map[string]bool{}
	for _, b := range cards.Batches() {
		entries, _ := os.ReadDir(filepath.Join(cards.Home(), b))
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".json") || e.Name() == "batch.json" {
				continue
			}
			var c struct{ Branch string }
			if raw, err := os.ReadFile(filepath.Join(cards.Home(), b, e.Name())); err == nil && json.Unmarshal(raw, &c) == nil && c.Branch != "" {
				out[c.Branch] = true
			}
		}
	}
	return out
}

func (w *listWatcher) snapshot() ([]item, error) {
	var out []item
	if w.kind == "pr" {
		var prs []struct {
			Number      int
			URL         string
			HeadRefName string
			UpdatedAt   string
			Author      struct{ Login string }
			Labels      []struct{ Name string }
		}
		if err := ghx.PRList("", "open", &prs); err != nil {
			return nil, err
		}
		mine := batchBranches()
		for _, p := range prs {
			labeled := false
			for _, l := range p.Labels {
				if w.label != "" && l.Name == w.label {
					labeled = true
				}
			}
			if p.Author.Login == w.me || mine[p.HeadRefName] || labeled {
				out = append(out, item{p.Number, p.URL, p.UpdatedAt})
			}
		}
		return out, nil
	}
	label := w.label
	if label == "" {
		label = "agent"
	}
	var issues []struct {
		Number    int
		URL       string
		UpdatedAt string
	}
	if err := ghx.IssueList(label, &issues); err != nil {
		return nil, err
	}
	for _, i := range issues {
		out = append(out, item{i.Number, i.URL, i.UpdatedAt})
	}
	return out, nil
}

func (w *listWatcher) save(now []item) {
	if now == nil {
		now = []item{}
	}
	_ = os.MkdirAll(filepath.Dir(w.file), 0o755)
	b, _ := json.Marshal(now)
	_ = os.WriteFile(w.file, b, 0o644)
	w.seen = now
}

func (w *listWatcher) poll() ([]string, error) {
	now, err := w.snapshot()
	if err != nil {
		return nil, err
	}
	if w.seen == nil {
		w.save(now)
		return nil, nil
	}
	var out []string
	for _, n := range now {
		var old *item
		for i := range w.seen {
			if w.seen[i].Number == n.Number {
				old = &w.seen[i]
			}
		}
		if old == nil && w.o.on("new") {
			out = append(out, fmt.Sprintf("%s %d new %s", w.kind, n.Number, n.URL))
		} else if old != nil && old.V != n.V && w.o.on("updated") {
			out = append(out, fmt.Sprintf("%s %d updated %s", w.kind, n.Number, n.URL))
		}
	}
	if len(out) > 0 {
		w.save(now)
	}
	return out, nil
}

// batchWatcher wakes on cards that need the warden, or any change with --all, and on a change
// of CI verdict, mergeability or merge/close of any card's PR (when GitHub is reachable).
type batchWatcher struct {
	name, since, stuckBefore string
	all, gh                  bool
	seen                     map[string]string
	prs                      map[string]*prWatcher
	prNext                   time.Time
	merge                    map[string]string
	reported                 map[string]bool
	pending                  []string
}

// prEvery spaces the PR reads of a batch wait apart; card reads stay at the batch interval.
var prEvery = 30 * time.Second

var prEvents = opts{events: map[string]bool{"checks": true, "conflict": true, "merged": true, "closed": true}}

var final = map[string]bool{"merged": true, "reported": true, "dropped": true}

// prLines polls the PR of every non-final card. A PR read for the first time reports a state it
// already has (green, red, conflicting, merged, closed), unless a previous wait of this batch
// reported it at the same head, or (at the start of the wait) the card is `ready` or its merge
// mode isn't auto: a green PR left for the user must not wake every new wait.
func (w *batchWatcher) prLines(rows []cards.Row, start bool) []string {
	if !w.gh || time.Now().Before(w.prNext) {
		return nil
	}
	w.prNext = time.Now().Add(prEvery)
	var out []string
	for _, r := range rows {
		if r.PR == "" || final[r.State] {
			continue
		}
		pw, known := w.prs[r.PR]
		if !known {
			pw = newPR(r.PR, prEvents)
		}
		lines, err := pw.poll()
		if err != nil {
			continue
		}
		w.prs[r.PR] = pw
		if !known {
			lines = w.firstSight(r, pw, lines, start)
		}
		out = append(out, w.record(r.ID, pw, lines)...)
	}
	return out
}

// firstSight filters the lines of a PR's first read and adds a conflict it doesn't report itself.
func (w *batchWatcher) firstSight(r cards.Row, pw *prWatcher, lines []string, start bool) []string {
	quiet := start && (r.State == "ready" || w.mergeOf(r.ID) != "auto")
	if quiet {
		// Left for the user: a green, merged or closed PR is no news; red and conflicts are.
		var keep []string
		for _, l := range lines {
			if strings.Contains(l, " checks=red") {
				keep = append(keep, l)
			}
		}
		lines = keep
	}
	if pw.last.mergeable == "CONFLICTING" && pw.last.state == "OPEN" && !slices.Contains(lines, conflictLine(pw.last)) {
		lines = append(lines, conflictLine(pw.last))
	}
	var out []string
	for _, l := range lines {
		if !w.reported[r.ID+" "+pw.last.head+" "+l] {
			out = append(out, l)
		}
	}
	return out
}

// record prefixes lines with the batch and card and saves them in the batch's wait-seen.json,
// so a relaunched wait doesn't report them again.
func (w *batchWatcher) record(id string, pw *prWatcher, lines []string) []string {
	var out []string
	for _, l := range lines {
		w.reported[id+" "+pw.last.head+" "+l] = true
		out = append(out, fmt.Sprintf("batch %s %s %s", w.name, id, l))
	}
	if len(lines) > 0 {
		if err := w.saveSeen(); err != nil {
			fmt.Fprintf(os.Stderr, "smithy wait: %v\n", err)
		}
	}
	return out
}

// seenFile is not a .json file, so the card listing never takes it for a card.
func seenFile(name string) string { return filepath.Join(cards.Home(), name, ".wait-seen") }

// loadSeen reads the reported keys, one per line, into w.reported.
func (w *batchWatcher) loadSeen() {
	b, err := os.ReadFile(seenFile(w.name))
	if err != nil {
		return
	}
	for _, k := range strings.Split(string(b), "\n") {
		if k != "" {
			w.reported[k] = true
		}
	}
}

// saveSeen merges the keys on disk (another waiter may have written some) with this one's and
// replaces the file atomically.
func (w *batchWatcher) saveSeen() error {
	w.loadSeen()
	keys := make([]string, 0, len(w.reported))
	for k := range w.reported {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	f := seenFile(w.name)
	tmp, err := os.CreateTemp(filepath.Dir(f), ".wait-seen-*")
	if err != nil {
		return err
	}
	_, err = tmp.WriteString(strings.Join(keys, "\n") + "\n")
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), f)
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}

// mergeOf is the item's merge mode: its own, else the batch's (also for an item batch.json lacks).
func (w *batchWatcher) mergeOf(id string) string {
	if m, ok := w.merge[id]; ok && m != "" {
		return m
	}
	return w.merge[""]
}

// mergeModes reads each item's merge mode from batch.json; key "" holds the batch default.
func mergeModes(name string) map[string]string {
	var bt struct {
		Merge string
		Items []struct{ ID, Merge string }
	}
	out := map[string]string{}
	if b, err := os.ReadFile(filepath.Join(cards.Home(), name, "batch.json")); err == nil && json.Unmarshal(b, &bt) == nil {
		out[""] = bt.Merge
		for _, it := range bt.Items {
			out[it.ID] = it.Merge
		}
	}
	return out
}

// mark is what makes a card worth a wake: its stage, state, CI verdict and PR. Other field
// updates (phase, progress, notes, tend mirroring the same state) don't wake the warden twice.
func mark(r cards.Row) string { return r.Stage + "|" + r.State + "|" + r.CI + "|" + r.PR }

var act = map[string]bool{"decide": true, "stuck": true, "verify": true, "merged": true, "reported": true, "dropped": true}

func stuck(rows []cards.Row) string {
	var ids []string
	for _, r := range rows {
		if r.Stage == "stuck" {
			ids = append(ids, r.ID)
		}
	}
	return strings.Join(ids, ",")
}

// stuckBeats lists the stuck items with their last heartbeat age.
func stuckBeats(rows []cards.Row) string {
	var ids []string
	for _, r := range rows {
		if r.Stage == "stuck" {
			ids = append(ids, r.ID+" (last beat "+r.Beat+")")
		}
	}
	return strings.Join(ids, ", ")
}

func newBatch(name string, all bool) (*batchWatcher, error) {
	if name == "" {
		b := cards.Batches()
		if len(b) == 0 {
			return nil, fmt.Errorf("no batches in %s", cards.Home())
		}
		name = b[0]
	}
	rows, err := cards.Rows(name)
	if err != nil {
		return nil, err
	}
	seen := map[string]string{}
	for _, r := range rows {
		seen[r.ID] = mark(r)
	}
	w := &batchWatcher{name: name, since: time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), stuckBefore: stuck(rows), all: all, seen: seen,
		gh: ghx.Ready() == nil, prs: map[string]*prWatcher{}, merge: mergeModes(name), reported: map[string]bool{}}
	w.loadSeen()
	if w.gh {
		w.pending = w.prLines(rows, true)
		w.prNext = time.Time{}
	}
	return w, nil
}

func (w *batchWatcher) poll() ([]string, error) {
	rows, err := cards.Rows(w.name)
	if err != nil {
		return nil, err
	}
	out := w.pending
	w.pending = nil
	for _, r := range rows {
		// A card the warden itself set to merged is not news.
		if r.Updated != nil && *r.Updated > w.since && (w.all || (act[r.Stage] && r.State != "merged" && w.seen[r.ID] != mark(r))) {
			out = append(out, fmt.Sprintf("batch %s %s  %s  %s  %s  %s", w.name, r.ID, r.Stage, r.Phase, r.Progress, r.Text))
		}
	}
	out = append(out, w.prLines(rows, false)...)
	if len(out) == 0 {
		if s := stuck(rows); s != "" && s != w.stuckBefore {
			out = []string{fmt.Sprintf("batch %s stale: %s", w.name, stuckBeats(rows))}
		}
	}
	return out, nil
}

// relaunchLines asks for a new wait on each batch that still has a non-final card.
func relaunchLines(ws []watcher) []string {
	var out []string
	for _, w := range ws {
		if b, ok := w.(*batchWatcher); ok {
			if rows, err := cards.Rows(b.name); err == nil && open(rows) {
				out = append(out, "relaunch: smithy wait batch:"+b.name)
			}
		}
	}
	return out
}

// open reports whether any card is not final (merged, reported, dropped): the warden must wait again.
func open(rows []cards.Row) bool {
	for _, r := range rows {
		if r.State != "merged" && r.State != "reported" && r.State != "dropped" {
			return true
		}
	}
	return false
}

const usage = `usage: smithy wait <target>... [--events a,b] [--all] [--every <sec>] [--max <min>]
Blocks until a target has an event, prints one line per event and exits. Run it in the
background where the harness wakes on exit, else in the foreground; never sleep-then-check.
Targets (any mix):
  pr:<number|url>     checks finished (green/red + failed names), merged, closed, conflict, review, comment, push
  run:<id|url>        a workflow run finished
  branch:<name>       every workflow run on the branch's latest commit finished
  prs[:<label>]       a PR opened or updated: yours, a warden batch's, or with the label
  issues[:<label>]    an issue opened or updated with the label (default agent)
  batch:<name>        a warden card needs the warden (--all: any card change), or a card's PR
                      turned green/red, conflicts, merged or closed (already so at the start: only for
                      merge-auto cards not ready, else red/conflict only; events in the batch's .wait-seen are skipped);
                      polls cards every 10 s, PRs every 30 s; while a card is not final the
                      output ends with "relaunch: smithy wait batch:<name>"
  actions[:<branch>]  a workflow run on the branch (default: origin's default) finished red,
                      push, schedule or manual
  base[:<branch>]     the branch (default: origin's default) moved on origin
  alerts              a new open Dependabot alert (skipped, with a note, without read access)
                      actions and alerts already there on the first run are recorded, not reported
  auto                new qualified issues from auto.sources were queued (the "auto … queued"
                      lines of smithy auto poll); polls every auto.every s and records each poll
  timer:<minutes>     fires after the minutes (exit 0, "timer: <m>m elapsed"); alone, no GitHub;
                      the ask timeout: smithy wait timer:$(smithy config get ask.timeout), minimum 0.05;
                      when ask.timeout is 0 skip the timer (wait for the answer)
  watch[:<scopes>]    tend's watch mode; scopes is a comma list of prs, issues, actions, base,
                      alerts, or all (default tend.watch; issue label from tend.label)
Finished checks are reported at once. Exit 0 = events, 4 = heartbeat (plus "pr <n> no-ci" for a PR whose head never got a check), 2 = bad target, 3 = no GitHub access (gh missing or signed out and no GH_TOKEN).
Defaults: settings watch.events, watch.every (30 s), watch.max (50 min).`

// expandWatch turns `watch[:<scopes>]` into targets, the scopes and issue label coming from
// tend.watch and tend.label when not given.
func expandWatch(targets []string) ([]string, bool) {
	var out []string
	watch := false
	for _, t := range targets {
		kind, scope, _ := strings.Cut(t, ":")
		if kind != "watch" {
			out = append(out, t)
			continue
		}
		watch = true
		if scope == "" {
			scope = config.Get("tend.watch", "prs")
		}
		if scope == "all" {
			scope = "prs,issues,actions,base,alerts"
		}
		for _, s := range strings.Split(scope, ",") {
			switch s = strings.TrimSpace(s); s {
			case "issues":
				out = append(out, "issues:"+config.Get("tend.label", "agent"))
			case "":
			default:
				out = append(out, s)
			}
		}
	}
	return out, watch
}

func onlyBatches(targets []string) bool {
	for _, t := range targets {
		if !strings.HasPrefix(t, "batch:") {
			return false
		}
	}
	return true
}

var sleep = time.Sleep

// timer handles `smithy wait timer:<minutes>`, which takes no other target.
func timer(targets []string) (int, bool) {
	for _, t := range targets {
		arg, ok := strings.CutPrefix(t, "timer:")
		if !ok {
			continue
		}
		m, err := strconv.ParseFloat(arg, 64)
		if len(targets) > 1 || err != nil || math.IsNaN(m) || math.IsInf(m, 0) || m < 0.05 {
			fmt.Fprintf(os.Stderr, "smithy wait: timer takes at least 0.05 minutes and no other target: %s\n", strings.Join(targets, " "))
			return 2, true
		}
		sleep(time.Duration(m * float64(time.Minute)))
		fmt.Printf("timer: %gm elapsed\n", m)
		return 0, true
	}
	return 0, false
}

// Main runs `smithy wait`.
func Main(args []string) int {
	var targets []string
	var every, max float64 = -1, -1
	evs := ""
	all := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		next := func() string {
			if i+1 < len(args) {
				i++
				return args[i]
			}
			return ""
		}
		switch a {
		case "-h", "--help":
			fmt.Println(usage)
			return 0
		case "--all":
			all = true
		case "--events":
			evs = next()
		case "--every":
			every, _ = strconv.ParseFloat(next(), 64)
		case "--max":
			max, _ = strconv.ParseFloat(next(), 64)
		default:
			if strings.HasPrefix(a, "--") {
				fmt.Fprintf(os.Stderr, "unknown option %s\n", a)
				return 2
			}
			targets = append(targets, a)
		}
	}
	if len(targets) == 0 {
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}
	if code, ok := timer(targets); ok {
		return code
	}
	targets, watchMode := expandWatch(targets)
	for _, t := range targets {
		kind, arg, _ := strings.Cut(t, ":")
		switch kind {
		case "pr", "run", "branch", "batch":
			if arg != "" {
				continue
			}
		case "prs", "issues", "actions", "base", "alerts", "auto":
			continue
		}
		fmt.Fprintf(os.Stderr, "smithy wait: unknown target %s\n", t)
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}
	if watchMode && evs == "" {
		evs = "new,updated"
	}
	if evs == "" {
		evs = config.Get("watch.events", strings.Join(allEvents, ","))
	}
	if all {
		evs = strings.Join(allEvents, ",")
	}
	o := opts{events: map[string]bool{}, all: all}
	for _, e := range strings.Split(evs, ",") {
		o.events[strings.TrimSpace(e)] = true
	}
	if every < 0 {
		every, _ = strconv.ParseFloat(config.Get("watch.every", "30"), 64)
		if onlyBatches(targets) {
			every = 10
		}
		if len(targets) == 1 && targets[0] == "auto" {
			every, _ = strconv.ParseFloat(config.Get("auto.every", "60"), 64)
		}
	}
	if max < 0 {
		max, _ = strconv.ParseFloat(config.Get("watch.max", "50"), 64)
	}
	for _, t := range targets {
		if !strings.HasPrefix(t, "batch:") && t != "auto" {
			if err := ghx.Ready(); err != nil {
				fmt.Fprintln(os.Stderr, "smithy wait:", err)
				return 3
			}
			break
		}
	}
	var ws []watcher
	for _, t := range targets {
		kind, arg, _ := strings.Cut(t, ":")
		var w watcher
		var err error
		switch kind {
		case "pr":
			w = newPR(arg, o)
		case "run":
			w = newRun(arg)
		case "branch":
			w = branchWatcher{arg}
		case "prs":
			w, err = newList("pr", arg, o)
		case "issues":
			w, err = newList("issue", arg, o)
		case "batch":
			w, err = newBatch(arg, all)
		case "actions":
			w = newActions(arg)
		case "base":
			w = newBase(arg)
		case "alerts":
			w = &alertsWatcher{seen: newSeen("alerts")}
		case "auto":
			w, err = newAuto()
		default:
			err = fmt.Errorf("unknown target %s", t)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "cannot read target:", err)
			return 2
		}
		ws = append(ws, w)
	}
	end := time.Now().Add(time.Duration(max * float64(time.Minute)))
	step := time.Duration(every * float64(time.Second))
	for first := true; ; first = false {
		var out []string
		for _, w := range ws {
			var lines []string
			var err error
			if first {
				lines, err = firstPoll(w, 3, 2*time.Second)
			} else {
				lines, err = w.poll()
			}
			if err != nil {
				if first {
					fmt.Fprintln(os.Stderr, "cannot read target:", err)
					return 2
				}
				continue
			}
			out = append(out, lines...)
		}
		if len(out) > 0 {
			out = append(out, relaunchLines(ws)...)
			fmt.Println(strings.Join(out, "\n"))
			return 0
		}
		if time.Now().Add(step).After(end) {
			break
		}
		time.Sleep(step)
	}
	fmt.Printf("heartbeat: nothing within %gm\n", max)
	for _, l := range noCILines(ws, max) {
		fmt.Println(l)
	}
	return 4
}

// noCILines names PRs whose head never got a check during the wait, so "still pending" and "CI
// never ran" (Actions off, out of minutes, workflow not triggered) read differently.
func noCILines(ws []watcher, max float64) []string {
	var out []string
	for _, w := range ws {
		if p, ok := w.(*prWatcher); ok && p.last != nil && p.last.state == "OPEN" && p.last.checks.state == "none" {
			out = append(out, fmt.Sprintf("pr %d no-ci: no check ran in %gm; check the repo's Actions settings and minutes %s", p.last.num, max, p.last.url))
		}
	}
	return out
}

// pushedAhead reports whether this checkout's origin/<branch> is strictly ahead of the PR head
// GitHub shows: a push it hasn't processed yet. A head this checkout doesn't have (a fork, another
// repo) or a ref behind the head is never ahead.
func pushedAhead(s *prState) bool {
	if s.branch == "" || s.state != "OPEN" {
		return false
	}
	sha, err := ghx.Run("git", "rev-parse", "--verify", "-q", "refs/remotes/origin/"+s.branch)
	sha = strings.TrimSpace(sha)
	if err != nil || sha == "" || s.head == "" || sha == s.head {
		return false
	}
	_, err = ghx.Run("git", "merge-base", "--is-ancestor", s.head, sha)
	return err == nil
}

// autoWatcher polls the auto sources and wakes for newly queued issues.
type autoWatcher struct{ s auto.Settings }

func newAuto() (*autoWatcher, error) {
	s, err := auto.LoadSettings()
	if err != nil {
		return nil, err
	}
	if srcs, err := s.Open(""); len(srcs) == 0 {
		if err == nil {
			err = fmt.Errorf("auto.sources is empty")
		}
		return nil, err
	}
	return &autoWatcher{s}, nil
}

func (w *autoWatcher) poll() ([]string, error) {
	lines, code := auto.PollLines(w.s, "")
	if code != 0 && len(lines) == 0 {
		return nil, fmt.Errorf("auto poll failed (exit %d)", code)
	}
	return lines, nil
}
