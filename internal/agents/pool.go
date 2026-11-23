package agents

import (
	"errors"
	"fmt"
	"os"
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

// limited checks a finished run's log for a usage limit and, when hit, marks the account.
func limited(a pool.Account, log string, json bool, rc int) bool {
	until, ok := pool.Detect(log, json, rc, poolNow())
	if !ok {
		return false
	}
	_ = pool.MarkLimited(a.Name, until)
	_ = events.Append(events.Event{Kind: "limited", Account: a.Name, Detail: "until " + until.UTC().Format(time.RFC3339)})
	fmt.Fprintf(os.Stderr, "smithy agent: pool account %s hit its usage limit; limited until %s\n", a.Name, until.Local().Format("2006-01-02 15:04"))
	return true
}

// recordCard writes the account on the warden card of a <warden home>/<batch>/<item>.log run.
func recordCard(log, cwd, account string) {
	if log == "" {
		return
	}
	if !filepath.IsAbs(log) {
		log = filepath.Join(cwd, log)
	}
	dir := filepath.Dir(log)
	if realPath(filepath.Dir(dir)) != realPath(cards.Home()) {
		return
	}
	_, _ = cards.Set(filepath.Base(dir), strings.TrimSuffix(filepath.Base(log), ".log"), []string{"account=" + account})
}
