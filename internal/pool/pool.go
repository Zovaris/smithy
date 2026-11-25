// Package pool keeps the user's own agent accounts (~/.smithy/pool.json) and when each one is
// rate limited (~/.smithy/pool-state.json), so smithy agent can run on the least recently used
// account that is not limited and fail over once when a run hits a usage limit. Each account is
// an official CLI pointed at its own config dir (see Harnesses); smithy never
// reads, copies or logs the tokens inside it.
package pool

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/ghx"
)

// Usage is the text for `smithy pool --help`.
const Usage = `usage: smithy pool add <name> <harness> [<dir>] | list | remove <name> | login <name> | clear <name>
A pool of your own accounts for smithy agent (off until one is added). Each account is an official
CLI with its own config dir: claude CLAUDE_CONFIG_DIR, codex CODEX_HOME, opencode XDG_DATA_HOME,
gemini GEMINI_CLI_HOME, cursor CURSOR_CONFIG_DIR, pi PI_CODING_AGENT_DIR, dsh DSH_HOME. No proxies or relays,
and only accounts you own: the providers' terms apply to each one.
  add     <name> <harness> [<dir>]  harness claude, codex, opencode, gemini, cursor, pi or dsh; dir defaults to ~/.smithy/accounts/<name>
  login   <name>                    runs the CLI interactively with that account's dir so you sign in
  list                              accounts, status (ok or limited until), last used
  remove  <name>                    drops the account (its dir and login stay on disk)
  clear   <name>                    forgets a recorded limit
smithy agent picks the least recently used account that is not limited, for the CLI it runs
(AGENT_CLI=pool: any harness). A run that hits a usage limit marks the account limited until its
reset (5h when the CLI does not say); limits are detected for claude, codex, opencode,
gemini and pi; cursor and dsh only rotate. A read-only run, or one that left the worktree untouched,
reruns once on the next account; any other exits 75 and blocks its warden card so warden
relaunches it in the same worktree. With every account limited it exits 75 at once. Files: ~/.smithy/pool.json, ~/.smithy/pool-state.json.`

// Harnesses are the CLIs a pool account can use, with the env var that points at its config dir.
var Harnesses = map[string]string{
	"claude":   "CLAUDE_CONFIG_DIR",
	"codex":    "CODEX_HOME",
	"opencode": "XDG_DATA_HOME",
	"gemini":   "GEMINI_CLI_HOME",
	"cursor":   "CURSOR_CONFIG_DIR",
	"pi":       "PI_CODING_AGENT_DIR",
	"dsh":      "DSH_HOME",
}

// DefaultLimit is how long an account stays limited when the CLI gives no reset time.
const DefaultLimit = 5 * time.Hour

// Account is one pool entry.
type Account struct {
	Name    string `json:"name"`
	Harness string `json:"harness"`
	Dir     string `json:"dir"`
}

// Env is the variable that selects the account for its CLI.
func (a Account) Env() string { return Harnesses[a.Harness] + "=" + a.Dir }

// Status is the recorded state of one account.
type Status struct {
	LimitedUntil time.Time `json:"limited_until,omitzero"`
	LastUsed     time.Time `json:"last_used,omitzero"`
}

// File is ~/.smithy/pool.json; StateFile is ~/.smithy/pool-state.json.
func File() string      { return filepath.Join(ghx.Home(), "pool.json") }
func StateFile() string { return filepath.Join(ghx.Home(), "pool-state.json") }

var nameRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]*$`)

// MakeDir creates an account dir and forces it to 0700 (MkdirAll keeps an existing dir's mode).
func MakeDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.Chmod(dir, 0o700)
}

// Load reads the accounts; none (and no error) when the file is missing.
func Load() ([]Account, error) {
	var p struct {
		Accounts []Account `json:"accounts"`
	}
	b, err := os.ReadFile(File())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("%s: %w", File(), err)
	}
	return p.Accounts, nil
}

func save(as []Account) error {
	b, _ := json.MarshalIndent(map[string]any{"accounts": as}, "", "  ")
	return writeAtomic(File(), append(b, '\n'))
}

func writeAtomic(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		_ = os.Chmod(f.Name(), 0o600)
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}

// Add records a new account; dir "" means ~/.smithy/accounts/<name>. The dir is created 0700.
func Add(name, harness, dir string) (Account, error) {
	if !nameRe.MatchString(name) {
		return Account{}, fmt.Errorf("name must start with a letter, digit or _ and use only [A-Za-z0-9._-]")
	}
	if Harnesses[harness] == "" {
		return Account{}, fmt.Errorf("harness %q has no per-account dir smithy knows (claude, codex, opencode, gemini, cursor, pi, dsh)", harness)
	}
	for _, seg := range strings.FieldsFunc(dir, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			return Account{}, fmt.Errorf("dir %q has a .. segment; give a plain path", dir)
		}
	}
	if dir == "" {
		dir = filepath.Join(ghx.Home(), "accounts", name)
	}
	if strings.HasPrefix(dir, "~/") {
		h, _ := os.UserHomeDir()
		dir = filepath.Join(h, dir[2:])
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return Account{}, err
	}
	a := Account{name, harness, dir}
	err = config.WithLock(File(), func() error {
		as, err := Load()
		if err != nil {
			return err
		}
		for _, x := range as {
			if x.Name == name {
				return fmt.Errorf("account %s already exists", name)
			}
		}
		if err := MakeDir(dir); err != nil {
			return err
		}
		return save(append(as, a))
	})
	return a, err
}

// Remove drops an account and its state; its dir stays.
func Remove(name string) error {
	err := config.WithLock(File(), func() error {
		as, err := Load()
		if err != nil {
			return err
		}
		var keep []Account
		for _, x := range as {
			if x.Name != name {
				keep = append(keep, x)
			}
		}
		if len(keep) == len(as) {
			return fmt.Errorf("no account %s", name)
		}
		return save(keep)
	})
	if err != nil {
		return err
	}
	return edit(func(st map[string]Status) { delete(st, name) })
}

// Find returns the named account.
func Find(name string) (Account, error) {
	as, err := Load()
	if err != nil {
		return Account{}, err
	}
	for _, a := range as {
		if a.Name == name {
			return a, nil
		}
	}
	return Account{}, fmt.Errorf("no account %s (smithy pool list)", name)
}

// State reads every account's status.
func State() map[string]Status {
	st := map[string]Status{}
	if b, err := os.ReadFile(StateFile()); err == nil {
		_ = json.Unmarshal(b, &st)
	}
	return st
}

// edit changes the state under the lock and writes it atomically.
func edit(f func(map[string]Status)) error {
	return config.WithLock(StateFile(), func() error {
		st := State()
		f(st)
		b, _ := json.MarshalIndent(st, "", "  ")
		return writeAtomic(StateFile(), append(b, '\n'))
	})
}

// Matches reports whether an account serves harness ("pool" or "" means any).
func Matches(a Account, harness string) bool {
	return harness == "" || harness == "pool" || a.Harness == harness
}

// Limited is returned by Acquire when every matching account is limited.
type Limited struct{ Until time.Time }

func (e *Limited) Error() string {
	return "every pool account is limited; earliest reset " + e.Until.Local().Format("2006-01-02 15:04")
}

// ErrNone means the pool has no account for the harness: run without one.
var ErrNone = errors.New("no pool account for this harness")

// Acquire picks the least recently used matching account that is not limited (skipping skip),
// marks it used now and returns it. ErrNone when nothing matches; *Limited when all are limited.
func Acquire(harness string, now time.Time, skip ...string) (Account, error) {
	return AcquireIn(harness, nil, now, skip...)
}

// AcquireIn is Acquire with harnesses ranked by order first (unlisted ones last), so a pooled
// run stays on the first listed harness with a free account and falls back down the list.
func AcquireIn(harness string, order []string, now time.Time, skip ...string) (Account, error) {
	as, err := Load()
	if err != nil {
		return Account{}, err
	}
	var got Account
	var lim *Limited
	err = edit(func(st map[string]Status) {
		var cands []Account
		for _, a := range as {
			if !Matches(a, harness) || contains(skip, a.Name) {
				continue
			}
			if u := st[a.Name].LimitedUntil; u.After(now) {
				if lim == nil || u.Before(lim.Until) {
					lim = &Limited{u}
				}
				continue
			}
			cands = append(cands, a)
		}
		if len(cands) == 0 {
			return
		}
		sort.SliceStable(cands, func(i, j int) bool {
			if ri, rj := rank(order, cands[i].Harness), rank(order, cands[j].Harness); ri != rj {
				return ri < rj
			}
			return st[cands[i].Name].LastUsed.Before(st[cands[j].Name].LastUsed)
		})
		got = cands[0]
		s := st[got.Name]
		s.LastUsed = now
		st[got.Name] = s
	})
	switch {
	case err != nil:
		return Account{}, err
	case got.Name != "":
		return got, nil
	case lim != nil:
		return Account{}, lim
	}
	return Account{}, ErrNone
}

func rank(order []string, h string) int {
	for i, o := range order {
		if o == h {
			return i
		}
	}
	return len(order)
}

// Preferred is the first harness in order that has a pool account, "" when none does.
func Preferred(order []string) string {
	as, _ := Load()
	for _, h := range order {
		for _, a := range as {
			if a.Harness == h {
				return h
			}
		}
	}
	return ""
}

// Earliest is the earliest reset among the matching limited accounts; ok false when one is free.
func Earliest(harness string, now time.Time) (time.Time, bool) {
	as, _ := Load()
	st := State()
	var min time.Time
	any := false
	for _, a := range as {
		if !Matches(a, harness) {
			continue
		}
		any = true
		u := st[a.Name].LimitedUntil
		if !u.After(now) {
			return time.Time{}, false
		}
		if min.IsZero() || u.Before(min) {
			min = u
		}
	}
	return min, any
}

// MarkLimited records that name is limited until until.
func MarkLimited(name string, until time.Time) error {
	return edit(func(st map[string]Status) {
		s := st[name]
		s.LimitedUntil = until.UTC()
		st[name] = s
	})
}

// Clear forgets name's limit.
func Clear(name string) error {
	return edit(func(st map[string]Status) {
		s := st[name]
		s.LimitedUntil = time.Time{}
		st[name] = s
	})
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}
