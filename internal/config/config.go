// Package config reads and writes smithy settings: <repo>/.smithy/config.json first, then
// ~/.smithy/config.json ($SMITHY_CONFIG), then built-in defaults. Models are stored per harness.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/ghx"
	"github.com/Zovaris/smithy/internal/harness"
)

// Setting is one known key: its default ("ask" means no default, ask the user once), who uses
// it and what it does.
type Setting struct{ Key, Default, UsedBy, Meaning string }

// Known lists every setting `smithy config list` shows.
var Known = []Setting{
	{"tasks.location", "repo", "smelt plan", "where task lists live: repo, local (~/.smithy/plans) or board"},
	{"tasks.commit", "ask", "smelt plan", "commit specs and tasks (docs/specs) or keep them out of git (.docs/specs)"},
	{"git.branch", "{type}/{key}-{slug}", "smelt warden", "branch name template: {type} feat/fix/docs/refactor/test/chore, {key} ticket or task id, {slug} from the title"},
	{"git.worktree", "{parent}/worktrees/{repo}-{branch}", "smelt warden tend", "worktree path template ({branch} with / as -)"},
	{"tasks.ignore", "ask", "smelt plan", "with tasks.commit false: gitignore (commit a .gitignore line) or exclude (local .git/info/exclude)"},
	{"warden.merge", "ask", "warden", "auto: warden merges green PRs (workers never do); manual: warden lists green PRs for the user; unset: warden asks per batch"},
	{"warden.cleanup", "auto", "warden", "auto: end finished workers and remove worktrees and local branches without asking"},
	{"warden.max", "5", "warden", "parallel workers (a token-cost cap)"},
	{"warden.mode", "smelt normal", "warden", "batch check speed: smelt fast, smelt normal, smelt full or plain"},
	{"warden.stuck_minutes", "15", "warden", "minutes without a worker heartbeat (cards beat or cards set) before its item is stuck"},
	{"warden.worker_model", "route", "warden", "route: pick cheap/mid/strong per task; session: same model as the parent session; or one model name for every worker"},
	{"route.domain", "", "smithy route", "comma list of the product's own nouns (secret for a secret scanner) that never count as risk"},
	{"agent.effort", "medium", "smelt warden scout tend", "reasoning effort for every agent smithy starts: low or medium; anything higher is capped at medium"},
	{"ask.timeout", "10", "use-smithy smelt warden", "minutes to wait for the user's answer to a decision before smithy consult picks (0 = don't start the timer: wait for the answer)"},
	{"agent.timeout", "90", "smelt warden", "minutes before smithy agent kills a headless agent (0 = no limit)"},
	{"watch.events", "checks,merged,closed,conflict,review,comment,push,new,updated", "smithy wait", "which events wake an agent waiting on GitHub"},
	{"watch.every", "30", "smithy wait", "seconds between GitHub polls"},
	{"watch.max", "50", "smithy wait", "minutes before a heartbeat exit (4) when nothing happened"},
	{"tend.watch", "prs", "tend", "what /tend watch follows: a comma list of prs, issues, actions, base, alerts, or all"},
	{"tend.label", "agent", "tend", "label of issues tend may queue for warden"},
	{"stack.test", "", "smelt warden", "this repo's test command when smithy stack can't detect it (also stack.single, stack.lint, stack.typecheck, stack.dev, …); set with --repo"},
	{"decide.url", "", "smithy decide", "decision model endpoint; set with decide.model to turn smithy decide on (a path ending in /scores uses the behavior format, else System One)"},
	{"decide.model", "", "smithy decide", "decision model id, e.g. span-01-free, jev-latest, kev-latest"},
	{"decide.key_env", "", "smithy decide", "name of the env var holding the key (never the key); empty for a local model"},
	{"decide.enabled", "true", "smithy decide", "false after three failed calls in a row; set true to turn it back on"},
	{"decide.timeout", "3000", "smithy decide", "milliseconds before a decide call is dropped"},
	{"auto.sources", "github", "smithy auto", "comma list of issue sources to poll: github, linear"},
	{"auto.mode", "labeled", "smithy auto", "labeled: only issues with the auto.label label; all: every issue that passes the source's filters"},
	{"auto.label", "auto", "smithy auto", "label that opts an issue in (auto.mode labeled)"},
	{"auto.model_prefix", "model:", "smithy auto", "label prefix for the worker tier (model:cheap, model:mid, model:strong)"},
	{"auto.speed_prefix", "speed:", "smithy auto", "label prefix for the smelt speed (speed:fast, speed:normal, speed:full)"},
	{"auto.max_per_day", "20", "smithy auto", "issues launched per local calendar day; the rest stay queued (an auto.hours window across midnight can launch up to 2x this overnight)"},
	{"auto.max_cost_per_day", "", "smithy auto serve", "USD of reported agent cost per local day; at or over it serve starts nothing new until tomorrow (running workers finish); only reported cost counts (claude), unknown cost counts as 0; empty = no cap"},
	{"auto.hours", "", "smithy auto", "launch window HH:MM-HH:MM in local time (22:00-07:00 wraps midnight); empty = always"},
	{"auto.every", "60", "smithy auto", "seconds between polls of each source"},
	{"auto.paused", "false", "smithy auto", "true holds smithy auto take: nothing launches until it is false again (p in smithy tui)"},
	{"auto.agent", "", "smithy auto serve", "agent CLI for serve's orchestrator (claude, codex, opencode…); empty = the one smithy agent detects"},
	{"auto.session_hours", "3", "smithy auto serve", "hours one orchestrator session may run before smithy agent ends it (its workers keep going)"},
	{"auto.merge", "manual", "tend auto", "merge mode for auto batches (production stays the user's, always)"},
	{"notify.telegram.chat", "", "smithy auto serve", "Telegram chat id serve notifies (bot token: smithy auth set telegram); empty = no notifications"},
	{"notify.kinds", "pr_ready,blocked,stuck,decision,orchestrator_failed,serve_down,cap_reached", "smithy auto serve", "comma list of notification kinds sent to Telegram"},
	{"linear.key_env", "LINEAR_API_KEY", "smithy auto", "name of the env var holding the Linear personal API key (never the key); wins over smithy auth set linear (~/.smithy/auth.json)"},
	{"linear.team", "", "smithy auto", "required: Linear team keys smithy may work in (e.g. ENG,OPS); empty, the Linear source refuses to poll"},
	{"linear.project", "", "smithy auto", "Linear project name filter; comma list"},
	{"linear.milestone", "", "smithy auto", "Linear project milestone name filter"},
	{"linear.cycle", "", "smithy auto", "Linear cycle filter: current or a cycle number"},
	{"linear.initiative", "", "smithy auto", "Linear initiative name filter (via the issue's project)"},
	{"linear.states", "unstarted,backlog", "smithy auto", "Linear workflow state types that count as new work"},
}

// ErrUnset means the key has no value and no default: ask the user once, then save it.
var ErrUnset = errors.New("unset")

// UserFile is the user's settings file.
func UserFile() string {
	if f := os.Getenv("SMITHY_CONFIG"); f != "" {
		return f
	}
	return filepath.Join(ghx.Home(), "config.json")
}

// RepoFile is this repo's settings file ("" outside a repo). In a linked worktree it is the main
// checkout's, unless the worktree tracks its own, so one setting holds for every worktree. Before 0.8 it was
// .agents/config.json, still read until someone moves it.
func RepoFile() string {
	cwd, _ := os.Getwd()
	top := ghx.RepoRoot(cwd)
	if top == "" {
		return ""
	}
	f := filepath.Join(top, ".smithy", "config.json")
	if _, err := ghx.Run("git", "-C", top, "ls-files", "--error-unmatch", ".smithy/config.json"); err != nil {
		if common, err := ghx.Run("git", "-C", top, "rev-parse", "--path-format=absolute", "--git-common-dir"); err == nil && filepath.Base(common) == ".git" {
			if main := filepath.Dir(common); main != top {
				top = main
				f = filepath.Join(top, ".smithy", "config.json")
			}
		}
	}
	if _, err := os.Stat(f); err != nil {
		if old := filepath.Join(top, ".agents", "config.json"); exists(old) {
			return old
		}
	}
	return f
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// loadFile reads a settings file; a missing or empty file is an empty object, and one that
// exists but isn't a JSON object is an error naming the file, so it is never taken as unset.
func loadFile(file string) (map[string]any, error) {
	m := map[string]any{}
	if file == "" {
		return m, nil
	}
	b, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return m, nil
	}
	if err := json.Unmarshal(b, &m); err != nil || m == nil {
		if err == nil {
			err = errors.New("null")
		}
		return nil, fmt.Errorf("%s is not a JSON object; fix or move it first: %v", file, err)
	}
	return m, nil
}

// ValidKey reports whether key is non-empty with no empty dot-separated segment.
func ValidKey(key string) error {
	for _, p := range strings.Split(key, ".") {
		if strings.TrimSpace(p) == "" {
			return fmt.Errorf("bad key %q: it needs non-empty parts separated by single dots", key)
		}
	}
	return nil
}

const lockWait = 10 * time.Second

// WithLock runs f holding the lock file next to file; see withLock.
func WithLock(file string, f func() error) error { return withLock(file, f) }

// withLock runs f holding an advisory lock file next to file (created with O_EXCL, removed after).
func withLock(file string, f func() error) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	lock := file + ".lock"
	deadline := time.Now().Add(lockWait)
	for {
		l, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			fmt.Fprintf(l, "%d\n", os.Getpid())
			l.Close()
			break
		}
		if !os.IsExist(err) {
			return err
		}
		if staleLock(lock) {
			os.Remove(lock)
			continue
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s is locked by another smithy config write; remove it if no write is running", lock)
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer os.Remove(lock)
	return f()
}

const lockStale = 30 * time.Second

// staleLock reports whether lock was left by a writer that is gone (its pid is dead) or
// is older than lockStale.
func staleLock(lock string) bool {
	fi, err := os.Stat(lock)
	if err != nil {
		return false
	}
	if time.Since(fi.ModTime()) > lockStale {
		return true
	}
	b, err := os.ReadFile(lock)
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return false
	}
	return !pidAlive(pid)
}

// target resolves a symlinked config to the file it points at (so a write keeps the link)
// and refuses a read-only file.
func target(file string) (string, error) {
	if r, err := filepath.EvalSymlinks(file); err == nil {
		file = r
	}
	if fi, err := os.Stat(file); err == nil && fi.Mode().Perm()&0o200 == 0 {
		return "", fmt.Errorf("%s is read-only; make it writable to change it", file)
	}
	return file, nil
}

func lookup(m map[string]any, key string) (any, bool) {
	var cur any = m
	for _, p := range strings.Split(key, ".") {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = obj[p]; !ok || cur == nil {
			return nil, false
		}
	}
	return cur, true
}

func str(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

func readKey(file, key string) (string, bool) {
	v, ok, _ := readKeyErr(file, key)
	return v, ok
}

func readKeyErr(file, key string) (string, bool, error) {
	m, err := loadFile(file)
	if err != nil {
		return "", false, err
	}
	v, ok := lookup(m, key)
	if !ok {
		return "", false, nil
	}
	return str(v), true, nil
}

func defaultOf(key string) string {
	for _, s := range Known {
		if s.Key == key {
			return s.Default
		}
	}
	return ""
}

// Effective returns a key's value and where it came from (repo, user or default); a settings
// file that isn't a JSON object is an error, not ErrUnset.
func Effective(key string) (string, string, error) {
	for _, s := range [][2]string{{RepoFile(), "repo"}, {UserFile(), "user"}} {
		v, ok, err := readKeyErr(s[0], key)
		if err != nil {
			return "", "", err
		}
		if ok {
			return v, s[1], nil
		}
	}
	if d := defaultOf(key); d != "" && d != "ask" {
		return d, "default", nil
	}
	return "", "", ErrUnset
}

// Get returns a key's effective value, or fallback when it is unset.
func Get(key, fallback string) string {
	if v, _, err := Effective(key); err == nil {
		return v
	}
	return fallback
}

// Set writes key = value into file; value is stored as JSON when it parses as JSON.
func Set(file, key, value string) error {
	if err := ValidKey(key); err != nil {
		return err
	}
	file, err := target(file)
	if err != nil {
		return err
	}
	return withLock(file, func() error { return set(file, key, value) })
}

func set(file, key, value string) error {
	m, err := loadFile(file)
	if err != nil {
		return err
	}
	var v any
	if err := json.Unmarshal([]byte(value), &v); err != nil {
		v = value
	}
	cur := m
	parts := strings.Split(key, ".")
	for _, p := range parts[:len(parts)-1] {
		next, ok := cur[p].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[p] = next
		}
		cur = next
	}
	cur[parts[len(parts)-1]] = v
	return writeJSON(file, m)
}

func writeJSON(file string, m map[string]any) error {
	b, _ := json.MarshalIndent(m, "", "  ")
	f, err := os.CreateTemp(filepath.Dir(file), filepath.Base(file)+".*.tmp")
	if err != nil {
		return err
	}
	if fi, err := os.Stat(file); err == nil {
		f.Chmod(fi.Mode().Perm())
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		os.Remove(f.Name())
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return err
	}
	return os.Rename(f.Name(), file)
}

// Unset removes key from file; it reports whether the key was there.
func Unset(file, key string) (bool, error) {
	if err := ValidKey(key); err != nil {
		return false, err
	}
	file, err := target(file)
	if err != nil {
		return false, err
	}
	var found bool
	err = withLock(file, func() error {
		var err error
		found, err = unset(file, key)
		return err
	})
	return found, err
}

func unset(file, key string) (bool, error) {
	m, err := loadFile(file)
	if err != nil {
		return false, err
	}
	cur := m
	parts := strings.Split(key, ".")
	for _, p := range parts[:len(parts)-1] {
		next, ok := cur[p].(map[string]any)
		if !ok {
			return false, nil
		}
		cur = next
	}
	if _, ok := cur[parts[len(parts)-1]]; !ok {
		return false, nil
	}
	delete(cur, parts[len(parts)-1])
	return true, writeJSON(file, m)
}

// Normalize returns the value to save for key and a note for the user.
func Normalize(key, value string) (string, string, error) {
	if key == "ask.timeout" {
		v := strings.TrimSpace(value)
		if n, err := strconv.ParseFloat(v, 64); err != nil || n < 0 || math.IsNaN(n) || math.IsInf(n, 0) {
			return "", "", fmt.Errorf("ask.timeout must be a non-negative number of minutes, got %q", value)
		}
		return v, "", nil
	}
	if key != "agent.effort" {
		v, err := typed(key, value)
		return v, "", err
	}
	switch v := strings.ToLower(strings.TrimSpace(value)); v {
	case "low", "medium":
		return v, "", nil
	case "high", "xhigh", "max":
		return "medium", fmt.Sprintf("agent.effort is capped at medium: saved medium, not %s", v), nil
	}
	return "", "", fmt.Errorf("agent.effort must be low or medium, got %q", value)
}

func tier(t string) bool { return t == "cheap" || t == "mid" || t == "strong" || t == "judge" }

// Model returns the user's model for a tier in the current harness.
func Model(t string) (string, error) {
	h, _ := harness.Detect()
	return ModelFor(h, t)
}

// ModelFor returns the user's model for a tier in harness h.
func ModelFor(h, t string) (string, error) {
	v, ok, err := readKeyErr(UserFile(), "models."+h+"."+t)
	if err != nil {
		return "", err
	}
	if ok {
		return v, nil
	}
	return "", ErrUnset
}

// Migrate moves pre-0.8 state (~/.agents/{config.json,plans,foreman,tend}) to ~/.smithy.
func Migrate() []string {
	home, _ := os.UserHomeDir()
	old := filepath.Join(home, ".agents")
	var moved []string
	for _, p := range [][2]string{{"config.json", "config.json"}, {"plans", "plans"}, {"foreman", "warden"}, {"tend", "tend"}} {
		a, b := filepath.Join(old, p[0]), filepath.Join(ghx.Home(), p[1])
		if exists(a) && !exists(b) {
			_ = os.MkdirAll(ghx.Home(), 0o755)
			if os.Rename(a, b) == nil {
				moved = append(moved, a+" -> "+b)
			}
		}
	}
	return moved
}

// AutoMigrate runs Migrate once, the first time smithy runs after an upgrade.
func AutoMigrate() {
	home, _ := os.UserHomeDir()
	if os.Getenv("SMITHY_CONFIG") != "" || exists(ghx.Home()) || !exists(filepath.Join(home, ".agents", "config.json")) {
		return
	}
	for _, m := range Migrate() {
		fmt.Fprintln(os.Stderr, "moved", m)
	}
}

const usage = `usage: smithy config <command>
  model <cheap|mid|strong|judge>              model for this harness; exit 3 when unset
  set-model <cheap|mid|strong|judge> <name> [harness]
  get <key>                             effective value (repo, user, default); exit 3 when unset
  set [--repo] <key> <value>            value stored as JSON when it is JSON
  unset [--repo] <key>                  remove a saved value (back to the default)
  list                                  every setting: value, source, what it does
  migrate                               move pre-0.8 state from ~/.agents to ~/.smithy
Unset with no default: ask the user once, then save with set (--repo when it only applies here).`

// readFail returns 3 for an unset value, else prints err and returns 1.
func readFail(err error) int {
	if errors.Is(err, ErrUnset) {
		return 3
	}
	fmt.Fprintln(os.Stderr, err)
	return 1
}

// Main runs `smithy config` and returns the exit code.
func Main(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}
	h, _ := harness.Detect()
	switch args[0] {
	case "model":
		if len(args) < 2 || !tier(args[1]) {
			fmt.Fprintln(os.Stderr, "usage: smithy config model <cheap|mid|strong|judge>")
			return 2
		}
		m, err := Model(args[1])
		if err != nil {
			return readFail(err)
		}
		fmt.Println(m)
	case "set-model":
		if len(args) < 3 || !tier(args[1]) {
			fmt.Fprintln(os.Stderr, "usage: smithy config set-model <cheap|mid|strong|judge> <name> [harness]")
			return 2
		}
		if len(args) > 3 {
			h = args[3]
		}
		if h == "" || strings.ContainsAny(h, "/.") || strings.TrimSpace(h) != h {
			fmt.Fprintf(os.Stderr, "bad harness name %q: no /, . or spaces, not empty\n", h)
			return 2
		}
		if strings.TrimSpace(args[2]) == "" {
			fmt.Fprintln(os.Stderr, "the model name is empty")
			return 2
		}
		key := "models." + h + "." + args[1]
		if err := Set(UserFile(), key, strconv.Quote(args[2])); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Printf("saved %s = %s (%s)\n", key, args[2], UserFile())
	case "get":
		if len(args) < 2 {
			return 2
		}
		if err := ValidKey(args[1]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		v, _, err := Effective(args[1])
		if err != nil {
			return readFail(err)
		}
		fmt.Println(v)
	case "set", "unset":
		rest, repo := args[1:], false
		if len(rest) > 0 && rest[0] == "--repo" {
			if _, err := scopeFile(true); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 2
			}
			rest, repo = rest[1:], true
		}
		if args[0] == "unset" {
			if len(rest) != 1 {
				fmt.Fprintln(os.Stderr, "usage: smithy config unset [--repo] <key>")
				return 2
			}
			if err := ValidKey(rest[0]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 2
			}
			file, ok, err := Remove(repo, rest[0])
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			if !ok {
				fmt.Printf("%s was not set (%s)\n", rest[0], file)
				return 0
			}
			fmt.Printf("removed %s (%s)\n", rest[0], file)
			return 0
		}
		if len(rest) != 2 {
			fmt.Fprintln(os.Stderr, "usage: smithy config set [--repo] <key> <value>")
			return 2
		}
		if err := ValidKey(rest[0]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		if _, _, err := Normalize(rest[0], rest[1]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		file, val, note, err := Apply(repo, rest[0], rest[1])
		if note != "" {
			fmt.Fprintln(os.Stderr, note)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Printf("saved %s = %s (%s)\n", rest[0], val, file)
	case "migrate":
		moved := Migrate()
		if len(moved) == 0 {
			fmt.Println("nothing to migrate")
		}
		for _, m := range moved {
			fmt.Println("moved", m)
		}
	case "list":
		for _, f := range []string{RepoFile(), UserFile()} {
			if _, err := loadFile(f); err != nil {
				return readFail(err)
			}
		}
		for _, t := range []string{"cheap", "mid", "strong", "judge"} {
			v, ok := readKey(UserFile(), "models."+h+"."+t)
			src := ""
			if ok {
				src = "user"
			} else {
				v = "unset"
			}
			fmt.Printf("%-16s %-14s %-8s %s model in %s\n", "models."+t, v, src, t, h)
		}
		for _, s := range Known {
			v, src, err := Effective(s.Key)
			if err != nil {
				v, src = "unset", "-"
			}
			fmt.Printf("%-16s %-14s %-8s %s (used by %s)\n", s.Key, v, src, s.Meaning, s.UsedBy)
		}
	default:
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}
	return 0
}
