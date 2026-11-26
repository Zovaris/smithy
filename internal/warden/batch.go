package warden

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/agents"
	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/layout"
	"github.com/Zovaris/smithy/internal/names"
	"github.com/Zovaris/smithy/internal/tasks"
)

const batchUsage = `usage: smithy batch <command>
  init <tasks_dir> --repo <dir> [--base b] [--merge auto|manual] [--max n]
      [--cleanup auto|ask] [--mode "smelt fast|smelt normal|smelt full|plain"] [--risky <id,id>]
      writes <batch-dir>/batch.json (batch = <repo>-<task dir name>, -2, -3 when taken) from the task
      files that aren't done or dropped: id <slug>-NN, title, intent = the file's path (relative to the repo when the file is
      committed on origin/<base>, else absolute), speed,
      prereqs from Blocked by, branch and worktree from smithy names, risky and route: debug from
      the task text (smelt's Risky list; a fix-type title). Prints the batch dir. Blockers that are
      done or dropped aren't prereqs. Refused while another batch has unfinished items from the
      same task dir or on the same branches. Without a flag, max, cleanup, mode and merge come from
      settings (warden.max, warden.cleanup, warden.mode, warden.merge: repo, then user); merge stays
      empty when unset and smithy brief refuses until you set the user's auto/manual answer. --risky marks each listed item id risky
      (an unknown id is an error). An auto task dir (auto-*, from smithy auto take): merge comes
      from auto.merge (default manual), each item's model from its Model: header (route: left to
      smithy route), speed route takes the mode's, and production is the base when it is the
      repo's default branch, else false.
  set <batch> key=value...
      sets batch.json fields: merge auto|manual, production <branch>|false, cleanup auto|ask,
      mode "smelt fast|smelt normal|smelt full|plain", base_sha <base>:<sha> (one base per pair).
  add <batch> --id <id> --title <t> --intent <text> [--base b] [--speed fast|normal|full]
      appends one item (a fix item from the batch check); base defaults to the first item's, speed
      to the batch mode's; refuses a duplicate id, title or branch.
Review batch.json before launch: mode, merge and production are yours to settle with the user.`

type batchItem struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Kind     string   `json:"kind"`
	Intent   string   `json:"intent"`
	Source   string   `json:"source,omitempty"`
	Speed    string   `json:"speed"`
	Model    string   `json:"model,omitempty"`
	Base     string   `json:"base"`
	Prereqs  []string `json:"prereqs"`
	Branch   string   `json:"branch"`
	Worktree string   `json:"worktree"`
	Launches int      `json:"launches"`
	Route    string   `json:"route,omitempty"`
	Risky    bool     `json:"risky,omitempty"`
}

// header is the value of a task file's `<name>:` header line.
func header(text, name string) string {
	for _, l := range strings.Split(text, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(l), name+":"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// newItem fills an item's branch, worktree, risky and route from its id, title and text.
func newItem(repo, id, key, title, intent, text, base, speed string, prereqs []string) (batchItem, error) {
	br := names.Branch(key, title, "")
	wt, err := names.Worktree(br, repo)
	if err != nil {
		return batchItem{}, err
	}
	it := batchItem{ID: id, Title: title, Kind: "ship", Intent: intent, Speed: speed, Base: base,
		Prereqs: prereqs, Branch: br, Worktree: wt, Risky: agents.Risky(text, config.Get("route.domain", ""))}
	if it.Prereqs == nil {
		it.Prereqs = []string{}
	}
	if names.Type(title) == "fix" {
		it.Route = "debug"
	}
	return it, nil
}

func batchOpts(args []string, keys ...string) (map[string]string, []string, error) {
	opts, pos := map[string]string{}, []string{}
	for i := 0; i < len(args); i++ {
		k, v, eq := strings.Cut(args[i], "=")
		known := false
		for _, x := range keys {
			known = known || k == x
		}
		switch {
		case known && eq:
			opts[k] = v
		case known && i+1 < len(args):
			i++
			opts[k] = args[i]
		case known || strings.HasPrefix(args[i], "--"):
			return nil, nil, fmt.Errorf("%s needs a value or is unknown", args[i])
		default:
			pos = append(pos, args[i])
		}
	}
	return opts, pos, nil
}

func validMode(m string) bool {
	switch m {
	case "smelt fast", "smelt normal", "smelt full", "plain":
		return true
	}
	return false
}

// defaultMax is warden.max (5 when unset or not a positive number).
func defaultMax() int {
	var n int
	if _, err := fmt.Sscanf(config.Get("warden.max", "5"), "%d", &n); err != nil || n < 1 {
		return 5
	}
	return n
}

func num(s string) int { n, _ := strconv.Atoi(s); return n }

func defaultBase(repo string) string {
	if b := gitOut(repo, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); b != "" {
		return strings.TrimPrefix(b, "origin/")
	}
	return "main"
}

// lookupTimeout bounds each network lookup of the default branch; tests shorten it.
var lookupTimeout = 10 * time.Second

// knownDefault is the repo's default branch from origin/HEAD, then gh, then ls-remote; "" when
// none answers (callers then treat the base as production: fail safe).
func knownDefault(repo string) string {
	if b := gitOut(repo, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); b != "" {
		return strings.TrimPrefix(b, "origin/")
	}
	run := func(name string, args ...string) string {
		ctx, cancel := context.WithTimeout(context.Background(), lookupTimeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Dir = repo
		cmd.WaitDelay = time.Second
		out, err := cmd.Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	if b := run("gh", "repo", "view", "--json", "defaultBranchRef", "-q", ".defaultBranchRef.name"); b != "" {
		return b
	}
	for _, l := range strings.Split(run("git", "ls-remote", "--symref", "origin", "HEAD"), "\n") {
		if ref, ok := strings.CutPrefix(l, "ref: refs/heads/"); ok {
			if b, _, ok := strings.Cut(ref, "\t"); ok {
				return b
			}
		}
	}
	return ""
}

func batchInit(args []string) error {
	o, pos, err := batchOpts(args, "--repo", "--base", "--merge", "--max", "--risky", "--cleanup", "--mode")
	if err != nil {
		return err
	}
	if len(pos) != 1 || o["--repo"] == "" {
		return fmt.Errorf("init needs <tasks_dir> and --repo")
	}
	repo, err := filepath.Abs(o["--repo"])
	if err != nil {
		return err
	}
	td, err := filepath.Abs(pos[0])
	if err != nil {
		return err
	}
	isAuto := strings.HasPrefix(filepath.Base(td), "auto-")
	merge := o["--merge"]
	if merge == "" && isAuto {
		merge = config.Get("auto.merge", "manual")
	}
	if merge == "" {
		merge = config.Get("warden.merge", "")
		if merge == "ask" {
			merge = ""
		}
	}
	if merge != "" && merge != "auto" && merge != "manual" {
		return fmt.Errorf("merge is %q, want auto or manual", merge)
	}
	cleanup := o["--cleanup"]
	if cleanup == "" {
		cleanup = config.Get("warden.cleanup", "auto")
	}
	if cleanup != "auto" && cleanup != "ask" {
		return fmt.Errorf("cleanup is %q, want auto or ask", cleanup)
	}
	mode := o["--mode"]
	if mode == "" {
		mode = config.Get("warden.mode", "smelt normal")
	}
	if !validMode(mode) {
		return fmt.Errorf("mode is %q, want smelt fast, smelt normal, smelt full or plain", mode)
	}
	if v, has := o["--risky"]; has && strings.TrimSpace(v) == "" {
		return fmt.Errorf("--risky needs at least one item id")
	}
	ts := tasks.Load(td)
	if len(ts) == 0 {
		return fmt.Errorf("no task files in %s", td)
	}
	top := gitOut(repo, "rev-parse", "--show-toplevel")
	if top == "" {
		top = repo
	}
	base := o["--base"]
	if base == "" {
		base = defaultBase(repo)
	}
	slug := filepath.Base(td)
	nn := map[int]string{}
	for _, t := range ts {
		if t.Status != "done" && t.Status != "dropped" {
			nn[num(t.NN)] = t.NN
		}
	}
	var items []batchItem
	for _, t := range ts {
		if t.Status == "done" || t.Status == "dropped" {
			continue
		}
		b, _ := os.ReadFile(t.File)
		text := string(b)
		id := slug + "-" + t.NN
		var pre []string
		for _, n := range t.Blockers {
			if v, ok := nn[num(n)]; ok {
				pre = append(pre, slug+"-"+v)
			}
		}
		key := header(text, "Key")
		if key == "" || key == "-" {
			key = id
		}
		speed := header(text, "Speed")
		if speed == "" {
			speed = "fast"
		}
		if speed == "route" {
			speed = modeSpeed(mode)
		}
		intent := t.File
		if rel, err := filepath.Rel(realPath(top), realPath(t.File)); err == nil && !strings.HasPrefix(rel, "..") {
			rel = filepath.ToSlash(rel)
			if exec.Command("git", "-C", top, "cat-file", "-e", "origin/"+base+":"+rel).Run() == nil {
				intent = rel
			}
		}
		it, err := newItem(repo, id, key, t.Title, intent, text, base, speed, pre)
		if err != nil {
			return err
		}
		if m := header(text, "Model"); m == "cheap" || m == "mid" || m == "strong" {
			it.Model = m
		}
		items = append(items, it)
	}
	if len(items) == 0 {
		return fmt.Errorf("every task in %s is done or dropped", td)
	}
	if r := o["--risky"]; r != "" {
		for _, id := range strings.Split(r, ",") {
			id = strings.TrimSpace(id)
			found := false
			for i := range items {
				if items[i].ID == id {
					items[i].Risky, found = true, true
				}
			}
			if !found {
				return fmt.Errorf("--risky: no item %q in %s", id, td)
			}
		}
	}
	if err := clash(td, items); err != nil {
		return err
	}
	stem := filepath.Base(repo) + "-" + slug
	name := stem
	for n := 2; ; n++ {
		if _, err := os.Stat(filepath.Join(layout.WardenHome(), name)); os.IsNotExist(err) {
			break
		}
		name = fmt.Sprintf("%s-%d", stem, n)
	}
	dir := filepath.Join(layout.WardenHome(), name)
	bt := map[string]any{"batch": name, "repo": repo, "stack": false, "production": "", "mode": mode,
		"merge": merge, "cleanup": cleanup, "tasks_dir": td, "items": items}
	if isAuto {
		bt["production"] = false
		if d := knownDefault(repo); d == "" || d == base {
			bt["production"] = base
		}
	}
	m := o["--max"]
	if m == "" {
		m = config.Get("warden.max", "5")
	}
	var n int
	if _, err := fmt.Sscanf(m, "%d", &n); err != nil || n < 1 {
		return fmt.Errorf("max is %q, want a number above 0", m)
	}
	bt["max"] = n
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := writeBatch(dir, bt); err != nil {
		return err
	}
	fmt.Println(dir)
	return nil
}

// clash refuses a batch whose task dir or branches an existing batch still has unfinished.
func clash(td string, items []batchItem) error {
	mine := map[string]bool{}
	for _, it := range items {
		mine[it.Branch] = true
	}
	entries, _ := os.ReadDir(layout.WardenHome())
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(layout.WardenHome(), e.Name(), "batch.json"))
		if err != nil {
			continue
		}
		var bt briefBatch
		if json.Unmarshal(raw, &bt) != nil {
			continue
		}
		for _, it := range bt.Items {
			if finished(e.Name(), it.ID) {
				continue
			}
			if bt.TasksDir != "" && realPath(bt.TasksDir) == realPath(td) {
				return fmt.Errorf("batch %s still runs %s (item %s); finish or clean it up first", e.Name(), td, it.ID)
			}
			if mine[it.Branch] {
				return fmt.Errorf("batch %s already uses branch %s (item %s)", e.Name(), it.Branch, it.ID)
			}
		}
	}
	return nil
}

// finished says an item's card is final (merged, dropped or reported).
func finished(batch, id string) bool {
	raw, err := os.ReadFile(filepath.Join(layout.WardenHome(), batch, id+".json"))
	if err != nil {
		return false
	}
	var c struct{ State string }
	_ = json.Unmarshal(raw, &c)
	return c.State == "merged" || c.State == "dropped" || c.State == "reported"
}

func writeBatch(dir string, bt map[string]any) error {
	b, err := json.MarshalIndent(bt, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "batch.*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	tmp.Close()
	_ = os.Chmod(tmp.Name(), 0o644)
	return os.Rename(tmp.Name(), filepath.Join(dir, "batch.json"))
}

// editBatch loads, changes and atomically saves <batch>/batch.json under its lock.
func editBatch(dir string, f func(bt map[string]any) error) error {
	file := filepath.Join(dir, "batch.json")
	return config.WithLock(file, func() error {
		raw, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("no batch.json in %s", dir)
		}
		var bt map[string]any
		if err := json.Unmarshal(raw, &bt); err != nil {
			return fmt.Errorf("batch.json unreadable: %v", err)
		}
		if err := f(bt); err != nil {
			return err
		}
		return writeBatch(dir, bt)
	})
}

func batchAdd(args []string) error {
	o, pos, err := batchOpts(args, "--id", "--title", "--intent", "--base", "--speed")
	if err != nil {
		return err
	}
	if len(pos) != 1 || o["--id"] == "" || o["--title"] == "" || o["--intent"] == "" {
		return fmt.Errorf("add needs <batch>, --id, --title and --intent")
	}
	dir := filepath.Join(layout.WardenHome(), pos[0])
	var it batchItem
	if err := editBatch(dir, func(bt map[string]any) error {
		var err error
		it, err = addItem(bt, o)
		return err
	}); err != nil {
		return err
	}
	fmt.Println(it.Branch, it.Worktree)
	return nil
}

// addItem appends the item `batch add` describes to bt.
func addItem(bt map[string]any, o map[string]string) (batchItem, error) {
	list, _ := bt["items"].([]any)
	base := o["--base"]
	for _, x := range list {
		m, _ := x.(map[string]any)
		if m["id"] == o["--id"] || m["title"] == o["--title"] {
			return batchItem{}, fmt.Errorf("item %s already exists (same id or title)", m["id"])
		}
		if b, _ := m["base"].(string); base == "" {
			base = b
		}
	}
	repo, _ := bt["repo"].(string)
	if base == "" {
		base = defaultBase(repo)
	}
	speed := o["--speed"]
	if speed == "" {
		mode, _ := bt["mode"].(string)
		speed = modeSpeed(mode)
	}
	if speed != "fast" && speed != "normal" && speed != "full" {
		return batchItem{}, fmt.Errorf("--speed is %q, want fast, normal or full", speed)
	}
	it, err := newItem(repo, o["--id"], "-", o["--title"], o["--intent"], o["--title"]+"\n"+o["--intent"], base, speed, nil)
	if err != nil {
		return batchItem{}, err
	}
	for _, x := range list {
		if m, _ := x.(map[string]any); m["branch"] == it.Branch {
			return batchItem{}, fmt.Errorf("item %s already uses branch %s", m["id"], it.Branch)
		}
	}
	bt["items"] = append(list, it)
	return it, nil
}

// modeSpeed is the smelt speed a batch mode implies (plain: fast; unknown: normal).
func modeSpeed(mode string) string {
	if f := strings.Fields(mode); len(f) == 2 && f[0] == "smelt" {
		return f[1]
	}
	if strings.HasPrefix(mode, "plain") {
		return "fast"
	}
	return "normal"
}

// batchSet sets validated batch.json fields, so nobody hand-edits the file.
func batchSet(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("set needs <batch> and key=value pairs")
	}
	if strings.ContainsAny(args[0], `/\`) || strings.HasPrefix(args[0], ".") {
		return fmt.Errorf("bad batch name %q", args[0])
	}
	dir := filepath.Join(layout.WardenHome(), args[0])
	if err := editBatch(dir, func(bt map[string]any) error { return setFields(bt, args[1:]) }); err != nil {
		return err
	}
	fmt.Println("batch", args[0], "set")
	return nil
}

// branchExists reports whether name is a local or origin branch of repo.
func branchExists(repo, name string) bool {
	for _, ref := range []string{"refs/heads/" + name, "refs/remotes/origin/" + name} {
		if exec.Command("git", "-C", repo, "rev-parse", "-q", "--verify", ref).Run() == nil {
			return true
		}
	}
	return false
}

func setFields(bt map[string]any, pairs []string) error {
	repo, _ := bt["repo"].(string)
	bases := map[string]bool{}
	list, _ := bt["items"].([]any)
	for _, x := range list {
		if m, _ := x.(map[string]any); m != nil {
			if b, _ := m["base"].(string); b != "" {
				bases[b] = true
			}
		}
	}
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			return fmt.Errorf("not key=value: %s", p)
		}
		switch k {
		case "merge":
			if v != "auto" && v != "manual" {
				return fmt.Errorf("merge is %q, want auto or manual", v)
			}
			bt[k] = v
		case "cleanup":
			if v != "auto" && v != "ask" {
				return fmt.Errorf("cleanup is %q, want auto or ask", v)
			}
			bt[k] = v
		case "mode":
			if !validMode(v) {
				return fmt.Errorf("mode is %q, want smelt fast, smelt normal, smelt full or plain", v)
			}
			bt[k] = v
		case "production":
			switch {
			case v == "false":
				bt[k] = false
			case v != "" && branchExists(repo, v):
				bt[k] = v
			default:
				return fmt.Errorf("production is %q, want false or an existing branch", v)
			}
		case "base_sha":
			b, sha, ok := strings.Cut(v, ":")
			if !ok || b == "" || sha == "" {
				return fmt.Errorf("base_sha is %q, want <base>:<sha>", v)
			}
			if !bases[b] {
				return fmt.Errorf("base_sha: no item in the batch has base %q", b)
			}
			m, _ := bt[k].(map[string]any)
			if m == nil {
				m = map[string]any{}
			}
			m[b] = sha
			bt[k] = m
		default:
			return fmt.Errorf("unknown field %q (merge, production, cleanup, mode, base_sha)", k)
		}
	}
	return nil
}

// Batch runs `smithy batch`.
func Batch(args []string) int {
	var err error
	switch {
	case len(args) > 0 && args[0] == "init":
		err = batchInit(args[1:])
	case len(args) > 0 && args[0] == "set":
		err = batchSet(args[1:])
	case len(args) > 0 && args[0] == "add":
		err = batchAdd(args[1:])
	default:
		fmt.Fprintln(os.Stderr, batchUsage)
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "smithy batch:", err)
		return 2
	}
	return 0
}
