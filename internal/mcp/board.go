//go:build harness

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/board"
)

// BoardTools read and update the built-in tracker; claims are held by holder (the run's card,
// or "smithy-mcp"), so an agent can only release its own.
func BoardTools(b *board.Board, holder string) []Tool {
	if holder == "" {
		holder = "smithy-mcp-" + strconv.Itoa(os.Getpid())
	}
	id := func(a Args) (int64, error) {
		s, err := a.Line("id", 20, true)
		if err != nil {
			return 0, err
		}
		return board.ParseID(s)
	}
	show := func(v any) (string, error) {
		out, err := json.MarshalIndent(v, "", "  ")
		return string(out), err
	}
	return []Tool{
		{Name: "board_list", Description: "List board tasks, optionally by state (todo, in_progress, blocked, review, done, canceled).",
			Schema: Object(map[string]any{"state": Str("filter by state")}),
			Call: func(_ context.Context, a Args) (string, error) {
				st, err := a.Line("state", 20, false)
				if err != nil {
					return "", err
				}
				ts, err := b.List(st)
				if err != nil {
					return "", err
				}
				if ts == nil {
					ts = []board.Task{}
				}
				return show(ts)
			}},
		{Name: "board_show", Description: "Show one board task (id 12 or B-12).", Schema: Object(map[string]any{"id": Str("task id")}, "id"),
			Call: func(_ context.Context, a Args) (string, error) {
				n, err := id(a)
				if err != nil {
					return "", err
				}
				t, err := b.Get(n)
				if err != nil {
					return "", err
				}
				return show(t)
			}},
		{Name: "board_claim", Description: "Claim a board task for this run (or release it), so no other run takes it.",
			Schema: Object(map[string]any{"id": Str("task id"), "ttl_minutes": Int("claim length, 1-1440 (default 60)"), "release": Bool("release this run's claim")}, "id"),
			Call: func(_ context.Context, a Args) (string, error) {
				n, err := id(a)
				if err != nil {
					return "", err
				}
				rel, err := a.Bool("release")
				if err != nil {
					return "", err
				}
				if rel {
					return "released", b.Release(n, holder)
				}
				m, err := a.Int("ttl_minutes", 1, 1440, 60)
				if err != nil {
					return "", err
				}
				return "claimed by " + holder, b.Claim(n, holder, time.Duration(m)*time.Minute)
			}},
		{Name: "board_set", Description: "Set a board task's state, branch or add a PR url.",
			Schema: Object(map[string]any{"id": Str("task id"), "state": Str("new state"), "branch": Str("branch name"), "pr": Str("https PR url to add")}, "id"),
			Call: func(_ context.Context, a Args) (string, error) {
				n, err := id(a)
				if err != nil {
					return "", err
				}
				f := map[string]string{}
				for _, k := range []string{"state", "branch", "pr"} {
					v, err := a.Line(k, 500, false)
					if err != nil {
						return "", err
					}
					if v != "" {
						f[k] = v
					}
				}
				if pr, ok := f["pr"]; ok {
					if u, err := url.Parse(pr); err != nil || u.Scheme != "https" || u.Host == "" {
						return "", errors.New("pr must be an https url")
					}
				}
				if br, ok := f["branch"]; ok && (strings.HasPrefix(br, "-") || strings.ContainsAny(br, " ~^:?*[\\") || strings.Contains(br, "..")) {
					return "", errors.New("branch is not a valid git branch name")
				}
				if len(f) == 0 {
					return "", errors.New("set state, branch or pr")
				}
				t, err := b.Get(n)
				if err != nil {
					return "", err
				}
				if t.ClaimedBy != "" && t.ClaimedBy != holder && t.ClaimExpires.After(time.Now()) {
					return "", errors.New("task is claimed by another run; claim it first")
				}
				if err := b.Set(n, f); err != nil {
					return "", err
				}
				return "updated", nil
			}},
	}
}
