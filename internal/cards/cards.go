// Package cards holds warden's status cards: one JSON file per batch item, so nobody
// hand-writes JSON, plus the board with the stage the warden acts on.
package cards

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/events"
	"github.com/Zovaris/smithy/internal/layout"
	tok "github.com/Zovaris/smithy/internal/usage"
)

// RaiseImpact, when set, may raise a new question's impact (never lower it); it reports whether it did.
var RaiseImpact func(question, impact string) (string, bool)

// States a worker may report.
var States = []string{"waiting", "working", "needs_decision", "blocked", "failed", "pr_open", "ready", "merged", "dropped", "reported"}

var idRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// staleMin is the minutes without a heartbeat before a working item is stuck:
// $WARDEN_STALE_MIN, else warden.stuck_minutes, else 15.
func staleMin() float64 {
	if v, err := strconv.ParseFloat(os.Getenv("WARDEN_STALE_MIN"), 64); err == nil {
		return v
	}
	if v, err := strconv.ParseFloat(config.Get("warden.stuck_minutes", "15"), 64); err == nil && v > 0 {
		return v
	}
	return 15
}

func stampNow() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }

// Beat stamps last_beat on an existing card without touching updated, so a worker's heartbeat
// keeps it off the stuck list without waking a batch wait.
func Beat(batch, id string) error {
	if !idRe.MatchString(batch) || !idRe.MatchString(id) {
		return fmt.Errorf("batch and id must match [A-Za-z0-9._-]+")
	}
	file := filepath.Join(layout.WardenHome(), batch, id+".json")
	if read(file) == nil {
		return fmt.Errorf("no card %s in batch %s (%s)", id, batch, file)
	}
	return config.WithLock(file, func() error {
		cur := read(file)
		if cur == nil {
			return fmt.Errorf("no card %s in batch %s (%s)", id, batch, file)
		}
		cur["last_beat"] = stampNow()
		return write(file, cur)
	})
}

// Launch records a started worker's pid on its card, creating the card as `working` (last_beat =
// now) when the worker hasn't written one, so a worker that dies before its first write still
// leaves a card that a batch wait and the stuck rule can see.
func Launch(batch, id string, pid int) error {
	if !idRe.MatchString(batch) || !idRe.MatchString(id) {
		return fmt.Errorf("batch and id must match [A-Za-z0-9._-]+")
	}
	file := filepath.Join(layout.WardenHome(), batch, id+".json")
	return config.WithLock(file, func() error {
		cur := read(file)
		if cur == nil {
			cur = map[string]any{"id": id}
		}
		if s(cur, "state") == "" {
			cur["state"] = "working"
		}
		cur["pid"] = pid
		cur["updated"] = stampNow()
		cur["last_beat"] = cur["updated"]
		return write(file, cur)
	})
}

// Dead marks a card failed when its recorded worker pid is no longer running and its state
// isn't final, pr_open, ready or already stuck (blocked, failed, needs_decision): the worker exited without
// reporting. alive reports whether a pid runs; log is the worker's log file. It returns true
// when it changed the card.
func Dead(batch, id string, alive func(int) bool, log string) bool {
	file := filepath.Join(layout.WardenHome(), batch, id+".json")
	changed := false
	_ = config.WithLock(file, func() error {
		cur := read(file)
		pid, ok := cur["pid"].(float64)
		if cur == nil || !ok || pid <= 0 || alive(int(pid)) {
			return nil
		}
		switch s(cur, "state") {
		case "merged", "reported", "dropped", "pr_open", "ready", "failed", "blocked", "needs_decision":
			return nil
		}
		_, err := set(file, id, []string{"state=failed", "note=worker exited: " + lastLine(log)})
		changed = err == nil
		return err
	})
	return changed
}

func lastLine(file string) string {
	b, err := os.ReadFile(file)
	if err != nil {
		return "(no log)"
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	l := lines[len(lines)-1]
	if rs := []rune(l); len(rs) > 300 {
		l = string(rs[:300])
	}
	if l == "" {
		return "(empty log)"
	}
	return l
}

func write(file string, cur map[string]any) error {
	b, _ := json.MarshalIndent(cur, "", "  ")
	tmp := fmt.Sprintf("%s.%d.tmp", file, os.Getpid())
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

func read(file string) map[string]any {
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

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func s(m map[string]any, k string) string {
	if m == nil {
		return ""
	}
	switch v := m[k].(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

// Set merges key=value pairs into <batch>/<id>.json (values parsed as JSON when they are JSON)
// and stamps `updated`. A decision stays open until the warden closes it with decision_key=.
func Set(batch, id string, pairs []string) (map[string]any, error) {
	if !idRe.MatchString(batch) || !idRe.MatchString(id) {
		return nil, fmt.Errorf("batch and id must match [A-Za-z0-9._-]+")
	}
	dir := filepath.Join(layout.WardenHome(), batch)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	file := filepath.Join(dir, id+".json")
	var out map[string]any
	err := config.WithLock(file, func() error {
		var err error
		out, err = set(file, id, pairs)
		return err
	})
	return out, err
}

func set(file, id string, pairs []string) (map[string]any, error) {
	cur := read(file)
	if cur == nil {
		cur = map[string]any{"id": id}
	}
	prevState, prevCI := s(cur, "state"), s(cur, "ci")
	closing := false
	for _, p := range pairs {
		i := strings.IndexByte(p, '=')
		if i < 1 {
			return nil, fmt.Errorf("not key=value: %s", p)
		}
		k, raw := p[:i], p[i+1:]
		if k == "decision_key" && raw == "" {
			closing = true
		}
		var v any
		if json.Unmarshal([]byte(raw), &v) != nil {
			v = raw
		}
		if raw == "" {
			v = nil
		}
		cur[k] = v
	}
	state := s(cur, "state")
	if state != "" && !has(States, state) {
		return nil, fmt.Errorf("unknown state %q: state must be one of %s", state, strings.Join(States, ", "))
	}
	if state != "needs_decision" && s(cur, "decision_key") != "" {
		return nil, fmt.Errorf("decision %s is still open; the warden closes it with decision_key=", s(cur, "decision_key"))
	}
	if state != "needs_decision" && closing {
		for _, k := range []string{"decision_key", "question", "options", "recommendation", "impact"} {
			delete(cur, k)
		}
	}
	if state == "pr_open" && s(cur, "pr") == "" {
		return nil, fmt.Errorf("pr_open requires pr=<url>")
	}
	if state == "needs_decision" {
		for _, k := range []string{"decision_key", "question", "impact"} {
			if s(cur, k) == "" {
				return nil, fmt.Errorf("needs_decision requires %s", k)
			}
		}
		if !has([]string{"low", "medium", "high"}, s(cur, "impact")) {
			return nil, fmt.Errorf("impact must be low, medium or high")
		}
		if RaiseImpact != nil && (hasKey(pairs, "question") || hasKey(pairs, "impact")) {
			if v, ok := RaiseImpact(s(cur, "question"), s(cur, "impact")); ok {
				cur["impact"] = v
				cur["impact_by"] = "decide"
			}
		}
	}
	cur["updated"] = stampNow()
	cur["last_beat"] = cur["updated"]
	if err := write(file, cur); err != nil {
		return cur, err
	}
	batch := filepath.Base(filepath.Dir(file))
	if ns := s(cur, "state"); ns != "" && ns != prevState {
		kind := "stage"
		if ns == "merged" {
			kind = "merged"
		}
		_ = events.Append(events.Event{Kind: kind, Batch: batch, Item: id, PR: s(cur, "pr"), Detail: ns})
	}
	if nc := s(cur, "ci"); nc != "" && nc != prevCI {
		_ = events.Append(events.Event{Kind: "ci", Batch: batch, Item: id, PR: s(cur, "pr"), Detail: nc})
	}
	return cur, nil
}

// Row is one board line.
type Row struct {
	ID       string  `json:"id"`
	Stage    string  `json:"stage"`
	State    string  `json:"state"`
	Phase    string  `json:"phase"`
	Progress string  `json:"progress"`
	PR       string  `json:"pr"`
	CI       string  `json:"ci"`
	Age      string  `json:"age"`
	Beat     string  `json:"beat"`
	LastBeat *string `json:"last_beat"`
	Updated  *string `json:"updated"`
	Text     string  `json:"text"`
	// Usage is what the item's worker run reported (<batch>/<id>.log.usage.json); nil when unknown.
	Usage *tok.Usage `json:"usage,omitempty"`
}

func ageMin(card map[string]any) (float64, bool) { return since(card, "updated") }

// beatMin is the minutes since the worker's last heartbeat (last_beat, else updated).
func beatMin(card map[string]any) (float64, bool) {
	if a, ok := since(card, "last_beat"); ok {
		return a, true
	}
	return ageMin(card)
}

func since(card map[string]any, key string) (float64, bool) {
	t, err := time.Parse(time.RFC3339, s(card, key))
	if err != nil {
		return 0, false
	}
	return time.Since(t).Minutes(), true
}

// held reports whether an item is on hold: `"hold": true` or a non-empty reason string.
func held(item map[string]any) bool {
	switch v := item["hold"].(type) {
	case bool:
		return v
	case string:
		return v != ""
	}
	return false
}

// Stage is what the warden has to do with an item now; first match wins.
func Stage(card, item map[string]any) string {
	if held(item) {
		return "held"
	}
	if card == nil {
		if s(item, "session") != "" {
			return "starting"
		}
		return "queued"
	}
	switch s(card, "state") {
	case "waiting":
		return "waiting"
	case "needs_decision":
		return "decide"
	case "blocked", "failed":
		return "stuck"
	case "pr_open", "ready":
		return "verify"
	case "merged", "reported", "dropped":
		return s(card, "state")
	}
	if a, ok := beatMin(card); ok && a > staleMin() {
		return "stuck"
	}
	return "working"
}

// Batches lists batch names, newest first.
func Batches() []string {
	entries, _ := os.ReadDir(layout.WardenHome())
	type bt struct {
		name string
		mod  time.Time
	}
	var all []bt
	for _, e := range entries {
		if e.IsDir() {
			if fi, err := e.Info(); err == nil {
				all = append(all, bt{e.Name(), fi.ModTime()})
			}
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].mod.After(all[j].mod) })
	out := make([]string, len(all))
	for i, b := range all {
		out[i] = b.name
	}
	return out
}

// Rows reads a batch: the plan's items plus any card files, each with its stage.
func Rows(batch string) ([]Row, error) {
	dir := filepath.Join(layout.WardenHome(), batch)
	if _, err := os.Stat(dir); err != nil {
		return nil, fmt.Errorf("no batch %s in %s", batch, layout.WardenHome())
	}
	plan := read(filepath.Join(dir, "batch.json"))
	items := map[string]map[string]any{}
	var ids []string
	if plan != nil {
		if list, ok := plan["items"].([]any); ok {
			for _, it := range list {
				if m, ok := it.(map[string]any); ok && s(m, "id") != "" {
					items[s(m, "id")] = m
					ids = append(ids, s(m, "id"))
				}
			}
		}
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		n := e.Name()
		if IsCardFile(n) {
			id := strings.TrimSuffix(n, ".json")
			if _, ok := items[id]; !ok {
				items[id] = map[string]any{}
				ids = append(ids, id)
			}
		}
	}
	rows := make([]Row, 0, len(ids))
	for _, id := range ids {
		card := read(filepath.Join(dir, id+".json"))
		item := items[id]
		r := Row{ID: id, Stage: Stage(card, item), State: or(s(card, "state")), Phase: or(s(card, "phase")), Progress: or(s(card, "progress")),
			PR: or(s(card, "pr")), CI: or(s(card, "ci")), Age: "-", Text: s(card, "question")}
		if r.Text == "" {
			r.Text = s(card, "note")
			if rs := []rune(r.Text); len(rs) > 160 {
				r.Text = string(rs[:157]) + "..."
			}
		}
		if held(item) {
			r.Text = "hold"
			if h, ok := item["hold"].(string); ok {
				r.Text = "hold: " + h
			}
		}
		if u := s(card, "updated"); u != "" {
			r.Updated = &u
			if a, ok := ageMin(card); ok {
				r.Age = fmt.Sprintf("%dm", int(a+0.5))
			}
		}
		if u, ok := tok.Read(filepath.Join(dir, id+".log")); ok {
			r.Usage = &u
		}
		r.Beat = "-"
		if b := s(card, "last_beat"); b != "" {
			r.LastBeat = &b
		}
		if a, ok := beatMin(card); ok {
			r.Beat = fmt.Sprintf("%dm", int(a+0.5))
		}
		rows = append(rows, r)
	}
	return rows, nil
}

func or(v string) string {
	if v == "" {
		return "-"
	}
	return v
}

// Total sums the reported usage of rows; ok is false when none reported any.
func Total(rows []Row) (tok.Usage, bool) {
	var t tok.Usage
	ok := false
	for _, r := range rows {
		if r.Usage != nil {
			t, ok = t.Add(*r.Usage), true
		}
	}
	return t, ok
}

// Board renders a batch as a table, or JSON.
func Board(batch string, asJSON bool) (string, error) {
	rows, err := Rows(batch)
	if err != nil {
		return "", err
	}
	if asJSON {
		out := map[string]any{"batch": batch, "items": rows}
		if t, ok := Total(rows); ok {
			out["usage"] = t
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		return string(b), nil
	}
	head := []string{"item", "stage", "state", "phase", "done", "pr", "ci", "age", "beat", "question / note"}
	table := [][]string{head}
	for _, r := range rows {
		table = append(table, []string{r.ID, r.Stage, r.State, r.Phase, r.Progress, r.PR, r.CI, r.Age, r.Beat, r.Text})
	}
	return "batch " + batch + "\n" + Table(table), nil
}

// Table pads every column but the last.
func Table(rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}
	w := make([]int, len(rows[0]))
	for _, r := range rows {
		for i, c := range r {
			if len(c) > w[i] {
				w[i] = len(c)
			}
		}
	}
	var b strings.Builder
	for n, r := range rows {
		for i, c := range r {
			if i == len(r)-1 {
				b.WriteString(c)
			} else {
				b.WriteString(c + strings.Repeat(" ", w[i]-len(c)) + "  ")
			}
		}
		if n < len(rows)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

const usage = `usage: smithy cards <command>
  set <batch> <id> key=value...          merge into <id>.json (JSON values parsed), stamps updated
                                         and last_beat
  beat <batch> <id>                      heartbeat: stamps last_beat only; a working item with no
                                         beat for warden.stuck_minutes (15) is stuck
  get <batch> <id>                       print one card
  board [batch] [--changed-since <iso>] [--json]   one line per item with its stage; newest batch by default
                                         (--changed-since exits 4 when nothing changed)
  live [batch] [--every 5]               redraws the board in a terminal for humans
  gate <batch> <id>                      exit 1 when batch.json marks the item risky and its card
                                         lacks security=ran (never merge it); 0 otherwise
Cards live in $WARDEN_HOME (default ~/.smithy/warden)/<batch>/.`

// Main runs `smithy cards`.
func Main(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}
	flag := func(name string) string {
		for i, a := range args {
			if a == name && i+1 < len(args) {
				v := args[i+1]
				args = append(args[:i], args[i+2:]...)
				return v
			}
		}
		return ""
	}
	cmd := args[0]
	args = args[1:]
	switch cmd {
	case "set":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, usage)
			return 2
		}
		cur, err := Set(args[0], args[1], args[2:])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		b, _ := json.Marshal(cur)
		fmt.Println(string(b))
	case "beat":
		if len(args) != 2 {
			fmt.Fprintln(os.Stderr, usage)
			return 2
		}
		if err := Beat(args[0], args[1]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	case "get":
		if len(args) != 2 {
			fmt.Fprintln(os.Stderr, usage)
			return 2
		}
		f := filepath.Join(layout.WardenHome(), args[0], args[1]+".json")
		if _, err := os.Stat(f); err != nil {
			fmt.Fprintf(os.Stderr, "no card %s in batch %s (%s)\n", args[1], args[0], f)
			return 1
		}
		b, _ := json.MarshalIndent(read(f), "", "  ")
		fmt.Println(string(b))
	case "gate":
		if len(args) != 2 {
			fmt.Fprintln(os.Stderr, usage)
			return 2
		}
		if err := Gate(args[0], args[1]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("ok")
	case "board", "live":
		asJSON := false
		for i, a := range args {
			if a == "--json" {
				asJSON = true
				args = append(args[:i], args[i+1:]...)
				break
			}
		}
		since, every := flag("--changed-since"), flag("--every")
		batch := ""
		if len(args) > 0 {
			batch = args[0]
		}
		if batch == "" {
			if b := Batches(); len(b) > 0 {
				batch = b[0]
			} else {
				fmt.Fprintf(os.Stderr, "no batches in %s\n", layout.WardenHome())
				return 1
			}
		}
		if cmd == "live" {
			sec, err := strconv.Atoi(every)
			if err != nil || sec < 1 {
				sec = 5
			}
			for {
				out, err := Board(batch, false)
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					return 1
				}
				fmt.Printf("\x1b[2J\x1b[H%s\n\n(every %ds, Ctrl-C to quit)\n", out, sec)
				time.Sleep(time.Duration(sec) * time.Second)
			}
		}
		if since != "" {
			rows, err := Rows(batch)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			changed := false
			for _, r := range rows {
				if r.Updated != nil && *r.Updated > since {
					changed = true
				}
			}
			if !changed {
				return 4
			}
		}
		out, err := Board(batch, asJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println(out)
	default:
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}
	return 0
}

// Gate refuses a risky item (batch.json) whose card lacks security=ran.
func Gate(batch, id string) error {
	bt := read(filepath.Join(layout.WardenHome(), batch, "batch.json"))
	if bt == nil {
		return fmt.Errorf("no batch.json in batch %s", batch)
	}
	items, _ := bt["items"].([]any)
	for _, x := range items {
		it, _ := x.(map[string]any)
		if s(it, "id") != id {
			continue
		}
		if it["risky"] != true {
			return nil
		}
		sec := s(read(filepath.Join(layout.WardenHome(), batch, id+".json")), "security")
		if sec == "ran" {
			return nil
		}
		if sec == "" {
			sec = "unset"
		}
		return fmt.Errorf("risky item %s: security=%s; never merge it (run the security agent, or list a not-run reason for the user)", id, sec)
	}
	return fmt.Errorf("no item %s in batch %s", id, batch)
}

func hasKey(pairs []string, k string) bool {
	for _, p := range pairs {
		if strings.HasPrefix(p, k+"=") {
			return true
		}
	}
	return false
}

// IsCardFile reports whether a batch dir entry is an item's card: a .json file other than
// batch.json and other than a usage sidecar (<id>.log.usage.json).
func IsCardFile(name string) bool {
	return strings.HasSuffix(name, ".json") && name != "batch.json" && !strings.HasSuffix(name, ".usage.json")
}
