package auto

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/ghx"
	"github.com/Zovaris/smithy/internal/plan"
)

// Usage is the text for `smithy auto --help`.
const Usage = `usage: smithy auto poll [--source <s>] | take [--max <n>] | report <source:id> <state> [--pr <url>] | status [--json]
Autonomous intake (settings auto.*; state in ~/.smithy/auto/state.json).
  poll    polls each source in auto.sources once and queues new qualified issues, one line each:
          auto <source> <id> queued <model|-> <speed|-> <url>
  take    moves the issues the daily cap (auto.max_per_day) and window (auto.hours) allow into a
          new task dir out of git (~/.smithy/plans/<repo>/auto-<date>-<n>); prints "<dir> <count>" or
          "auto none: <why>"
  report  writes back to the issue: started, completed or blocked (GitHub: a comment; Linear: a comment,
          the workflow state and the PR attached); the issue is <source>:<id> (linear:TES-1 or
          linear:<uuid>) or a bare id taken by auto take
  status  queue, today's count and cap, hours, last poll per source, watcher alive or stale
Exit 2 = bad usage or settings, 3 = no access to a source.`

// taskDir makes the task dir for take, always out of git (~/.smithy/plans/<repo>/<slug>), so
// intake never leaves files in the target repo; tests replace it.
var taskDir = plan.LocalTaskDir

// Main runs `smithy auto`.
func Main(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, Usage)
		return 2
	}
	s, err := LoadSettings()
	if err != nil {
		fmt.Fprintln(os.Stderr, "smithy auto:", errText(err))
		return 2
	}
	flags := map[string]string{}
	var pos []string
	for i := 1; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--source", "--max", "--pr":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "smithy auto: %s needs a value\n", a)
				return 2
			}
			i++
			flags[a] = args[i]
		case "--json":
			flags[a] = "1"
		default:
			pos = append(pos, a)
		}
	}
	switch args[0] {
	case "poll":
		if len(pos) > 0 {
			break
		}
		lines, code := PollLines(s, flags["--source"])
		for _, l := range lines {
			fmt.Println(l)
		}
		return code
	case "take":
		max := 0
		if v, ok := flags["--max"]; ok {
			if max, err = strconv.Atoi(v); err != nil || max < 1 {
				fmt.Fprintln(os.Stderr, "smithy auto: --max takes a number >= 1")
				return 2
			}
		}
		if len(pos) > 0 {
			break
		}
		r, err := Take(s, max, taskDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "smithy auto:", errText(err))
			return 1
		}
		if r.Count == 0 {
			fmt.Println("auto none:", r.Why)
		} else {
			fmt.Println(r.Dir, r.Count)
		}
		return 0
	case "report":
		if len(pos) != 2 {
			break
		}
		srcs, openErr := s.Open("")
		if len(srcs) == 0 {
			fmt.Fprintln(os.Stderr, "smithy auto:", errText(openErr))
			return openCode(openErr)
		}
		if err := Report(srcs, pos[0], pos[1], flags["--pr"]); err != nil {
			fmt.Fprintln(os.Stderr, "smithy auto:", Clean(err.Error()))
			if openErr != nil {
				fmt.Fprintln(os.Stderr, "smithy auto:", Clean(openErr.Error()))
			}
			if errors.Is(err, ghx.ErrNoGitHub) || errors.Is(err, ErrNoAccess) {
				return 3
			}
			if errors.Is(err, ErrAmbiguous) {
				return 2
			}
			return 1
		}
		fmt.Println("auto reported", pos[0], pos[1])
		return 0
	case "status":
		if len(pos) > 0 {
			break
		}
		st, err := StatusOf(s)
		if err != nil {
			fmt.Fprintln(os.Stderr, "smithy auto: state:", errText(err))
			return 1
		}
		if flags["--json"] != "" {
			b, _ := json.MarshalIndent(st, "", "  ")
			fmt.Println(string(b))
			return 0
		}
		printStatus(st)
		return 0
	}
	fmt.Fprintln(os.Stderr, Usage)
	return 2
}

// PollLines polls once (all sources, or only) and returns the queued lines and an exit code.
// smithy wait auto uses it too.
func PollLines(s Settings, only string) ([]string, int) {
	srcs, openErr := s.Open(only)
	if openErr != nil {
		fmt.Fprintln(os.Stderr, "smithy auto:", Clean(openErr.Error()))
	}
	if len(srcs) == 0 {
		return nil, openCode(openErr)
	}
	got, err := Poll(s, srcs, os.Stderr)
	var lines []string
	for _, q := range got {
		lines = append(lines, q.Line())
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "smithy auto: poll:", Clean(err.Error()))
		if errors.Is(err, ErrNoAccess) || errors.Is(err, ghx.ErrNoGitHub) || strings.Contains(err.Error(), ghx.ErrNoGitHub.Error()) {
			return lines, 3
		}
		return lines, 1
	}
	return lines, 0
}

func printStatus(st Status) {
	alive := "stale"
	if st.Alive {
		alive = "alive"
	}
	fmt.Printf("watcher: %s (every %ds)\n", alive, st.Every)
	for _, src := range st.Sources {
		last := "never"
		if t, ok := st.LastPoll[src]; ok {
			last = t.Format(time.RFC3339)
		}
		fmt.Printf("source %s: last poll %s\n", src, last)
	}
	fmt.Printf("today: %d/%d\n", st.Today, st.Cap)
	hours, open := "any time", "open"
	if st.Hours != "" {
		hours = st.Hours
	}
	if !st.OpenNow {
		open = "closed"
	}
	fmt.Printf("hours: %s (%s now)\n", hours, open)
	fmt.Printf("queue: %d\n", st.Queued)
	for _, q := range st.Queue {
		fmt.Printf("  %s %s %s %s %s\n", q.Source, q.ID, dash(q.Model), dash(q.Speed), q.URL)
	}
}

// openCode is the exit code when no source opened: 3 for no access, else 1.
func openCode(err error) int {
	if err == nil || errors.Is(err, ErrNoAccess) {
		return 3
	}
	return 1
}

func errText(err error) string {
	if err == nil {
		return "no source opened"
	}
	return Clean(err.Error())
}
