package warden

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Zovaris/smithy/internal/agents"
	"github.com/Zovaris/smithy/internal/cards"
	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/names"
	"github.com/Zovaris/smithy/internal/tasks"
)

const batchUsage = `usage: smithy batch <command>
  init <tasks_dir> --repo <dir> [--base b] [--merge auto|manual] [--max n]
      writes <batch-dir>/batch.json (batch = <repo>-<task dir name>, -2, -3 when taken) from the task
      files that aren't done or dropped: id <slug>-NN, title, intent = the file's path, speed,
      prereqs from Blocked by, branch and worktree from smithy names, risky and route: debug from
      the task text (smelt's Risky list; a fix-type title). Prints the batch dir. Blockers that are
      done or dropped aren't prereqs. Refused while another batch has unfinished items from the
      same task dir or on the same branches. Without --merge, merge stays empty and smithy brief
      refuses until you set the user's auto/manual answer.
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

func num(s string) int { n, _ := strconv.Atoi(s); return n }

func defaultBase(repo string) string {
	if b := gitOut(repo, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); b != "" {
		return strings.TrimPrefix(b, "origin/")
	}
	return "main"
}

func batchInit(args []string) error {
	o, pos, err := batchOpts(args, "--repo", "--base", "--merge", "--max")
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
	if m := o["--merge"]; m != "" && m != "auto" && m != "manual" {
		return fmt.Errorf("--merge is %q, want auto or manual", m)
	}
	ts := tasks.Load(td)
	if len(ts) == 0 {
		return fmt.Errorf("no task files in %s", td)
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
		intent := t.File
		if rel, err := filepath.Rel(realPath(repo), realPath(t.File)); err == nil && !strings.HasPrefix(rel, "..") {
			intent = filepath.ToSlash(rel)
		}
		it, err := newItem(repo, id, key, t.Title, intent, text, base, speed, pre)
		if err != nil {
			return err
		}
		items = append(items, it)
	}
	if len(items) == 0 {
		return fmt.Errorf("every task in %s is done or dropped", td)
	}
	if err := clash(td, items); err != nil {
		return err
	}
	stem := filepath.Base(repo) + "-" + slug
	name := stem
	for n := 2; ; n++ {
		if _, err := os.Stat(filepath.Join(cards.Home(), name)); os.IsNotExist(err) {
			break
		}
		name = fmt.Sprintf("%s-%d", stem, n)
	}
	dir := filepath.Join(cards.Home(), name)
	bt := map[string]any{"batch": name, "repo": repo, "stack": false, "production": "", "mode": "smelt normal",
		"merge": o["--merge"], "cleanup": "auto", "tasks_dir": td, "items": items}
	if m := o["--max"]; m != "" {
		var n int
		if _, err := fmt.Sscanf(m, "%d", &n); err != nil || n < 1 {
			return fmt.Errorf("--max is %q, want a number above 0", m)
		}
		bt["max"] = n
	}
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
	entries, _ := os.ReadDir(cards.Home())
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(cards.Home(), e.Name(), "batch.json"))
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
	raw, err := os.ReadFile(filepath.Join(cards.Home(), batch, id+".json"))
	if err != nil {
		return false
	}
	var c struct{ State string }
	json.Unmarshal(raw, &c)
	return c.State == "merged" || c.State == "dropped" || c.State == "reported"
}

func writeBatch(dir string, bt map[string]any) error {
	b, err := json.MarshalIndent(bt, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "batch.json"), append(b, '\n'), 0o644)
}

func batchAdd(args []string) error {
	o, pos, err := batchOpts(args, "--id", "--title", "--intent", "--base", "--speed")
	if err != nil {
		return err
	}
	if len(pos) != 1 || o["--id"] == "" || o["--title"] == "" || o["--intent"] == "" {
		return fmt.Errorf("add needs <batch>, --id, --title and --intent")
	}
	dir := filepath.Join(cards.Home(), pos[0])
	raw, err := os.ReadFile(filepath.Join(dir, "batch.json"))
	if err != nil {
		return fmt.Errorf("no batch.json in %s", dir)
	}
	var bt map[string]any
	if err := json.Unmarshal(raw, &bt); err != nil {
		return fmt.Errorf("batch.json unreadable: %v", err)
	}
	list, _ := bt["items"].([]any)
	base := o["--base"]
	for _, x := range list {
		m, _ := x.(map[string]any)
		if m["id"] == o["--id"] || m["title"] == o["--title"] {
			return fmt.Errorf("item %s already exists (same id or title)", m["id"])
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
		speed = "normal"
		if f := strings.Fields(mode); len(f) == 2 && f[0] == "smelt" {
			speed = f[1]
		} else if strings.HasPrefix(mode, "plain") {
			speed = "fast"
		}
	}
	if speed != "fast" && speed != "normal" && speed != "full" {
		return fmt.Errorf("--speed is %q, want fast, normal or full", speed)
	}
	it, err := newItem(repo, o["--id"], "-", o["--title"], o["--intent"], o["--title"]+"\n"+o["--intent"], base, speed, nil)
	if err != nil {
		return err
	}
	for _, x := range list {
		if m, _ := x.(map[string]any); m["branch"] == it.Branch {
			return fmt.Errorf("item %s already uses branch %s", m["id"], it.Branch)
		}
	}
	bt["items"] = append(list, it)
	if err := writeBatch(dir, bt); err != nil {
		return err
	}
	fmt.Println(it.Branch, it.Worktree)
	return nil
}

// Batch runs `smithy batch`.
func Batch(args []string) int {
	var err error
	switch {
	case len(args) > 0 && args[0] == "init":
		err = batchInit(args[1:])
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
