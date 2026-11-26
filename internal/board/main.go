//go:build harness

package board

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/auto"
	"github.com/Zovaris/smithy/internal/linear"
	"github.com/Zovaris/smithy/internal/store"
)

// Usage is the help text of smithy board.
const Usage = `usage: smithy board <add|list|show|set|claim|sync> ...

The built-in tracker (harness build), in ~/.smithy/smithy.db. smithy auto reads it with
auto.sources board; the auto label and filters apply as for any source.

  add "<title>" [--body <text>] [--priority 0-4] [--labels a,b] [--blocked-by 3,4]
  list [--state <state>] [--json]        states: todo in_progress blocked review done canceled
  show <id> [--json]                     id is 12 or B-12
  set <id> [--state s] [--title t] [--body b] [--priority p] [--labels a,b] [--blocked-by ids]
           [--branch b] [--pr <url>]     a Linear task's title, body, priority, labels come from Linear
  claim <id> --by <name> [--ttl 30m] | claim <id> --release --by <name>
  sync                                   pull Linear edits, push states and PRs (board.linear_sync true)`

// Main runs smithy board.
func Main(args []string, out, errw io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprintln(out, Usage)
		return 0
	}
	sub, pos, flags, err := parse(args)
	if err != nil {
		fmt.Fprintln(errw, err)
		return 2
	}
	st, err := store.Open(store.DefaultPath())
	if err != nil {
		fmt.Fprintln(errw, "board:", err)
		return 1
	}
	defer st.Close()
	b := New(st)
	if err := run(b, sub, pos, flags, out); err != nil {
		fmt.Fprintln(errw, auto.Clean(err.Error()))
		if strings.HasPrefix(err.Error(), "usage") {
			return 2
		}
		return 1
	}
	return 0
}

func parse(args []string) (string, []string, map[string]string, error) {
	sub := args[0]
	flags := map[string]string{}
	var pos []string
	for i := 1; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			pos = append(pos, a)
			continue
		}
		k, v, eq := strings.Cut(strings.TrimPrefix(a, "--"), "=")
		if !eq && k != "json" && k != "release" {
			if i+1 >= len(args) {
				return "", nil, nil, fmt.Errorf("usage: --%s needs a value", k)
			}
			i++
			v = args[i]
		}
		if !eq && (k == "json" || k == "release") {
			v = "true"
		}
		flags[strings.ReplaceAll(k, "-", "_")] = v
	}
	return sub, pos, flags, nil
}

func one(pos []string, what string) (int64, error) {
	if len(pos) != 1 {
		return 0, fmt.Errorf("usage: smithy board %s <id>", what)
	}
	return ParseID(pos[0])
}

func run(b *Board, sub string, pos []string, f map[string]string, out io.Writer) error {
	switch sub {
	case "add":
		if len(pos) != 1 {
			return fmt.Errorf("usage: smithy board add \"<title>\" [--body ...]")
		}
		id, err := b.Add(Task{Title: pos[0]})
		if err != nil {
			return err
		}
		delete(f, "title")
		if len(f) > 0 {
			if err := b.Set(id, f); err != nil {
				return err
			}
		}
		fmt.Fprintf(out, "B-%d\n", id)
	case "list":
		ts, err := b.List(f["state"])
		if err != nil {
			return err
		}
		if f["json"] == "true" {
			return json.NewEncoder(out).Encode(orEmpty(ts))
		}
		for _, t := range ts {
			fmt.Fprintln(out, line(t))
		}
	case "show":
		id, err := one(pos, "show")
		if err != nil {
			return err
		}
		t, err := b.Get(id)
		if err != nil {
			return err
		}
		if f["json"] == "true" {
			return json.NewEncoder(out).Encode(t)
		}
		fmt.Fprintln(out, line(t))
		for _, kv := range [][2]string{{"labels", auto.Clean(strings.Join(t.Labels, ","))}, {"blocked by", ids(t.BlockedBy)},
			{"branch", auto.Clean(t.Branch)}, {"prs", auto.Clean(strings.Join(t.PRs, " "))}, {"claim", claim(t)}} {
			if kv[1] != "" {
				fmt.Fprintf(out, "%s: %s\n", kv[0], kv[1])
			}
		}
		if t.Body != "" {
			fmt.Fprintf(out, "\n%s\n", auto.Clean(t.Body))
		}
	case "set":
		id, err := one(pos, "set")
		if err != nil {
			return err
		}
		if len(f) == 0 {
			return errors.New("usage: smithy board set <id> --state <state>")
		}
		return b.Set(id, f)
	case "claim":
		id, err := one(pos, "claim")
		if err != nil {
			return err
		}
		if f["by"] == "" {
			return fmt.Errorf("usage: smithy board claim <id> --by <name> [--ttl 30m]")
		}
		if f["release"] == "true" {
			return b.Release(id, f["by"])
		}
		ttl := 30 * time.Minute
		if v := f["ttl"]; v != "" {
			if ttl, err = time.ParseDuration(v); err != nil || ttl <= 0 {
				return fmt.Errorf("usage: --ttl is a positive duration like 30m, not %q", v)
			}
		}
		return b.Claim(id, f["by"], ttl)
	case "sync":
		if !LinearSync() {
			return fmt.Errorf("board: Linear sync is off; turn it on with smithy config set board.linear_sync true")
		}
		lin, err := linear.Open(auto.Settings{})
		if err != nil {
			return err
		}
		return b.Sync(lin, out)
	default:
		return fmt.Errorf("usage: unknown subcommand %q\n%s", sub, Usage)
	}
	return nil
}

func orEmpty(ts []Task) []Task {
	if ts == nil {
		return []Task{}
	}
	return ts
}

func line(t Task) string {
	ref := ""
	if t.LinearKey != "" {
		ref = " " + auto.Clean(t.LinearKey)
	}
	return fmt.Sprintf("B-%d  %-11s p%d%s  %s", t.ID, t.State, t.Priority, ref, auto.Clean(t.Title))
}

func ids(v []int64) string {
	var s []string
	for _, x := range v {
		s = append(s, fmt.Sprintf("B-%d", x))
	}
	return strings.Join(s, ",")
}

func claim(t Task) string {
	if t.ClaimedBy == "" || !t.ClaimExpires.After(clock()) {
		return ""
	}
	return auto.Clean(t.ClaimedBy) + " until " + t.ClaimExpires.Local().Format(time.RFC3339)
}
