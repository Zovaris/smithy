package agents

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/cards"
	"github.com/Zovaris/smithy/internal/events"
	"github.com/Zovaris/smithy/internal/pool"
)

// ExitLimited is smithy agent's exit code when the run hit a usage limit and no pool account
// was left to retry on (or every account was already limited).
const ExitLimited = 75

// poolNow is the clock the pool uses; tests replace it.
var poolNow = time.Now

// PoolHarness is the harness the pool serves for cli ("pool" means any), "" when no pool account
// fits it, so the run goes on as before.
func PoolHarness(cli string) string {
	h := cli
	if cli != "pool" {
		h = Kind(cli)
		if pool.Harnesses[h] == "" {
			return ""
		}
	}
	as, _ := pool.Load()
	for _, a := range as {
		if pool.Matches(a, h) {
			return h
		}
	}
	return ""
}

// peekHarness is the harness of the first account serving h, for a --bg parent that only
// validates the command (its supervised child acquires the account).
func peekHarness(h string) string {
	as, _ := pool.Load()
	for _, a := range as {
		if pool.Matches(a, h) {
			return a.Harness
		}
	}
	return h
}

// acquire picks the run's account. rc != 0 means stop with that code (every account limited).
func acquire(h string, o *Options) (pool.Account, int) {
	now := poolNow()
	a, err := pool.Acquire(h, now, o.poolSkip...)
	var lim *pool.Limited
	switch {
	case err == nil:
		o.PoolEnv = []string{a.Env()}
		o.Account = a.Name
		fmt.Fprintf(os.Stderr, "smithy agent: pool account %s (%s)\n", a.Name, a.Harness)
		recordCard(o.Log, o.Cwd, a.Name)
		return a, 0
	case errors.As(err, &lim):
	case errors.Is(err, pool.ErrNone) && len(o.poolSkip) > 0:
		lim = &pool.Limited{Until: earliestOr(h, now)}
	default:
		fmt.Fprintf(os.Stderr, "smithy agent: pool: %v; running without an account\n", err)
		return pool.Account{}, 0
	}
	msg := lim.Error()
	fmt.Fprintln(os.Stderr, "smithy agent:", msg)
	_ = events.Append(events.Event{Kind: "limited", Detail: msg})
	return pool.Account{}, ExitLimited
}

func earliestOr(h string, now time.Time) time.Time {
	if t, ok := pool.Earliest(h, now); ok {
		return t
	}
	return now.Add(pool.DefaultLimit)
}

// PoolDropEnv are credentials that would override the account's own login in a pooled run.
var PoolDropEnv = []string{"CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY", "OPENAI_API_KEY", "CODEX_API_KEY", "CLAUDE_CONFIG_DIR", "CODEX_HOME",
	"GEMINI_API_KEY", "GOOGLE_API_KEY", "CURSOR_API_KEY", "GEMINI_CLI_HOME", "CURSOR_CONFIG_DIR", "PI_CODING_AGENT_DIR", "DSH_HOME"}

// head is cwd's HEAD commit ("" outside a git repo).
func head(cwd string) string {
	out, err := exec.Command("git", "-C", cwd, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// untouched reports whether a write run left no trace a retry could redo: same HEAD as before,
// a clean status, and its branch neither tracking a remote nor present on origin. Outside git
// (start "") it is never untouched.
func untouched(cwd, start string) bool {
	if start == "" || head(cwd) != start {
		return false
	}
	st, err := exec.Command("git", "-C", cwd, "status", "--porcelain").Output()
	if err != nil || strings.TrimSpace(string(st)) != "" {
		return false
	}
	if exec.Command("git", "-C", cwd, "rev-parse", "--verify", "--quiet", "@{u}").Run() == nil {
		return false
	}
	br, err := exec.Command("git", "-C", cwd, "symbolic-ref", "--quiet", "--short", "HEAD").Output()
	if err != nil {
		return false
	}
	return exec.Command("git", "-C", cwd, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+strings.TrimSpace(string(br))).Run() != nil
}

// blockCard marks a warden item blocked on a limited account, so warden relaunches it in the
// same worktree once an account is free.
func blockCard(o Options, account string) {
	until := pool.State()[account].LimitedUntil
	note := fmt.Sprintf("account %s limited until %s; relaunch on another account", account, until.Local().Format("2006-01-02 15:04"))
	setCard(o.Log, o.Cwd, "state=blocked", "note="+note)
}

// limited checks a finished run's log for a usage limit and, when hit, marks the account.
func limited(a pool.Account, log, kind string, rc int) bool {
	until, ok := pool.Detect(log, kind, rc, poolNow())
	if !ok {
		return false
	}
	_ = pool.MarkLimited(a.Name, until)
	_ = events.Append(events.Event{Kind: "limited", Account: a.Name, Detail: "until " + until.UTC().Format(time.RFC3339)})
	fmt.Fprintf(os.Stderr, "smithy agent: pool account %s hit its usage limit; limited until %s\n", a.Name, until.Local().Format("2006-01-02 15:04"))
	return true
}

// recordCard writes the account on the warden card of a <warden home>/<batch>/<item>.log run.
func recordCard(log, cwd, account string) { setCard(log, cwd, "account="+account) }

// setCard sets pairs on the warden card of a <warden home>/<batch>/<item>.log run.
func setCard(log, cwd string, pairs ...string) {
	if batch, id := cardOf(log, cwd); batch != "" {
		_, _ = cards.Set(batch, id, pairs)
	}
}

// launchCard records a --bg worker's pid on its warden card, creating the card if needed.
func launchCard(log, cwd string, pid int) {
	if batch, id := cardOf(log, cwd); batch != "" {
		_ = cards.Launch(batch, id, pid)
	}
}

// cardOf is the batch and item of a <warden home>/<batch>/<item>.log run; "" when it isn't one.
func cardOf(log, cwd string) (string, string) {
	if log == "" {
		return "", ""
	}
	if !filepath.IsAbs(log) {
		log = filepath.Join(cwd, log)
	}
	dir := filepath.Dir(log)
	if realPath(filepath.Dir(dir)) != realPath(cards.Home()) {
		return "", ""
	}
	return filepath.Base(dir), strings.TrimSuffix(filepath.Base(log), ".log")
}

// poolDenied are deny rules for every pool account dir, wherever the user put it.
func poolDenied() []string {
	as, _ := pool.Load()
	var d []string
	for _, a := range as {
		if !filepath.IsAbs(a.Dir) {
			continue
		}
		p := filepath.ToSlash(a.Dir)
		d = append(d, "Read(/"+p+"/**)", "Edit(/"+p+"/**)", "Bash(*"+p+"*)")
	}
	return d
}
