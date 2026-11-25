// Package guard checks each shell command an agent runs against a blocklist of regexes. A match
// asks the user (run once, always allow this pattern, deny) through smithy daemon; an allowlist
// skips the check. Everything else runs: this is the only prompt in the YOLO default.
package guard

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/config"
)

// Defaults is the built-in blocklist; guard.block adds to it.
var Defaults = []string{
	`\brm\s+(?:-\S+\s+)*-[a-zA-Z]*[rR][a-zA-Z]*\s+(?:-\S+\s+)*(?:/\*?|~/?\*?|\$\{?HOME\}?/?\*?|\*)(?:\s|$|[;&|])`,
	`\bgit\s+push\b[^;&|]*\s(?:--force(?:-with-lease)?|-f)\b[^;&|]*[\s:+](?:main|master)(?:\s|$|[;&|])`,
	`\bgit\s+push\b[^;&|]*[\s:+](?:main|master)\s[^;&|]*(?:--force(?:-with-lease)?|-f)\b`,
	`\bgit\s+push\b[^;&|]*\s\+(?:\S+:)?(?:main|master)(?:\s|$|[;&|])`,
	`\bgit\s+reset\b[^;&|]*\s--hard\b`,
	`\b(?:mkfs(?:\.\w+)?|fdisk|sfdisk|wipefs|diskutil\s+(?:erase\w*|partitionDisk|zeroDisk|randomDisk|secureErase))\b`,
	`\bdd\b[^;&|]*\bof=/dev/`,
	`\b(?:curl|wget)\b[^;&]*\|\s*(?:sudo\s+)?(?:ba|z|k|da)?sh\b`,
	`\bchmod\s+(?:-\S+\s+)*(?:-[a-zA-Z]*R[a-zA-Z]*\s+(?:-\S+\s+)*0?777|0?777\s+(?:-\S+\s+)*-[a-zA-Z]*R)\b`,
	`\b(?:rm|rmdir|unlink|shred|trash|mv)\b[^;&|]*(?:~|\$\{?HOME\}?|/Users/[^/\s]+|/home/[^/\s]+)/\.smithy\b`,
}

// Answers a user can give to a guard question.
const (
	Once   = "run once"
	Always = "always allow this pattern"
	Deny   = "deny"
)

// Rules is the guard's effective configuration.
type Rules struct {
	Enabled      bool
	Block, Allow []string
}

// List parses a guard.block / guard.allow value: a JSON array of regexes, or one per line.
func List(v string) ([]string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil, nil
	}
	var out []string
	if strings.HasPrefix(v, "[") {
		if err := json.Unmarshal([]byte(v), &out); err != nil {
			return nil, fmt.Errorf("not a JSON list of regexes: %v", err)
		}
	} else {
		out = strings.Split(v, "\n")
	}
	kept := out[:0]
	for _, p := range out {
		if p = strings.TrimSpace(p); p != "" {
			kept = append(kept, p)
		}
	}
	return kept, nil
}

// Load reads guard.enabled, guard.block and guard.allow. A bad value is an error; callers fail
// closed on it (the defaults still block).
func Load() (Rules, error) {
	r := Rules{Enabled: config.Get("guard.enabled", "true") != "false", Block: append([]string{}, Defaults...)}
	b, err := List(config.Get("guard.block", ""))
	if err != nil {
		return r, fmt.Errorf("guard.block: %w", err)
	}
	r.Block = append(r.Block, b...)
	if r.Allow, err = List(config.Get("guard.allow", "")); err != nil {
		return r, fmt.Errorf("guard.allow: %w", err)
	}
	return r, nil
}

// Match returns the block pattern cmd matches, or "" when it may run (guard off, allowlisted or
// no match). An invalid block regex is reported as a match so a typo never opens the gate; an
// invalid allow regex is skipped.
func (r Rules) Match(cmd string) string {
	if !r.Enabled || strings.TrimSpace(cmd) == "" {
		return ""
	}
	for _, p := range r.Allow {
		if re, err := regexp.Compile(p); err == nil && re.MatchString(cmd) {
			return ""
		}
	}
	for _, p := range r.Block {
		re, err := regexp.Compile(p)
		if err != nil || re.MatchString(cmd) {
			return p
		}
	}
	return ""
}

// Asker records a question on smithy daemon and reads its answer back from the daemon's record.
type Asker interface {
	Ask(ctx context.Context, id, question string, options []string) error
	Outcome(ctx context.Context, id string) (answer string, answered bool, err error)
}

// Guard decides one command. Connect returns the daemon's asker (nil or an error: nobody to
// ask, so a match is denied). Remember saves an "always" answer (default: append to the user's
// guard.allow). Wait bounds the question (default ask.timeout; 0 waits for the answer).
type Guard struct {
	Connect  func() (Asker, error)
	Poll     time.Duration
	Wait     time.Duration
	Remember func(pattern string) error
	Load     func() (Rules, error)
}

// Check reports whether cmd may run and, when not, why.
func (g Guard) Check(ctx context.Context, cmd string) (bool, string) {
	load := g.Load
	if load == nil {
		load = Load
	}
	r, err := load()
	if err != nil {
		r.Allow = nil
	}
	pat := r.Match(cmd)
	if pat == "" {
		return true, ""
	}
	why := "blocked by the dangerous-command guard (" + pat + ")"
	if g.Connect == nil {
		return false, why + "; no smithy daemon to ask the user"
	}
	a, err := g.Connect()
	if err != nil || a == nil {
		return false, why + "; no smithy daemon to ask the user"
	}
	b := make([]byte, 4)
	rand.Read(b)
	id := "guard:" + hex.EncodeToString(b)
	q := "Run this command? " + oneLine(cmd, 300)
	if err := a.Ask(ctx, id, q, []string{Once, Always, Deny}); err != nil {
		return false, why + "; the daemon refused the question (" + err.Error() + ")"
	}
	wait := g.Wait
	if wait == 0 {
		wait = AskTimeout()
	}
	if wait > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, wait)
		defer cancel()
	}
	poll := g.Poll
	if poll <= 0 {
		poll = time.Second
	}
	t := time.NewTicker(poll)
	defer t.Stop()
	for {
		if ans, ok, err := a.Outcome(ctx, id); err == nil && ok {
			switch strings.TrimSpace(ans) {
			case Once:
				return true, ""
			case Always:
				rem := g.Remember
				if rem == nil {
					rem = Remember
				}
				rem(pat)
				return true, ""
			}
			return false, why + "; the user denied it"
		}
		select {
		case <-ctx.Done():
			return false, why + "; no answer in time"
		case <-t.C:
		}
	}
}

// AskTimeout is ask.timeout in minutes (default 10); negative means wait for the answer.
func AskTimeout() time.Duration {
	m, err := strconv.ParseFloat(config.Get("ask.timeout", "10"), 64)
	if err != nil || m < 0 {
		m = 10
	}
	if m == 0 {
		return -1
	}
	return time.Duration(m * float64(time.Minute))
}

// Remember appends pattern to the user's guard.allow.
func Remember(pattern string) error {
	file := config.UserFile()
	return config.WithLock(file+".guard", func() error {
		cur, err := config.UserGet("guard.allow", "")
		if err != nil {
			return err
		}
		l, err := List(cur)
		if err != nil {
			return err
		}
		for _, p := range l {
			if p == pattern {
				return nil
			}
		}
		b, _ := json.Marshal(append(l, pattern))
		return config.Set(file, "guard.allow", string(b))
	})
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		s = s[:n] + "…"
	}
	return s
}

// Hook is a Claude Code PreToolUse hook: it reads the event on in and, for a Bash call the guard
// stops, writes a deny decision to out. Anything else prints nothing, so the CLI's own rules apply.
func Hook(ctx context.Context, g Guard, in io.Reader, out io.Writer) error {
	var ev struct {
		Tool  string `json:"tool_name"`
		Input struct {
			Command string `json:"command"`
		} `json:"tool_input"`
	}
	if err := json.NewDecoder(io.LimitReader(in, 1<<20)).Decode(&ev); err != nil {
		return errors.New("guard hook: bad PreToolUse event: " + err.Error())
	}
	if ev.Tool != "Bash" {
		return nil
	}
	ok, why := g.Check(ctx, ev.Input.Command)
	if ok {
		return nil
	}
	return json.NewEncoder(out).Encode(map[string]any{"hookSpecificOutput": map[string]string{
		"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": why}})
}

// HookSettings is the Claude Code settings JSON that runs `<exe> guard hook` before every Bash
// call; "" when the guard is off.
func HookSettings(exe string) string {
	if config.Get("guard.enabled", "true") == "false" || exe == "" {
		return ""
	}
	secs := 600
	if d := AskTimeout(); d > 0 {
		secs = int(d.Seconds()) + 30
	} else if d < 0 {
		secs = 24 * 3600
	}
	b, _ := json.Marshal(map[string]any{"hooks": map[string]any{"PreToolUse": []any{map[string]any{
		"matcher": "Bash", "hooks": []any{map[string]any{"type": "command", "command": shellQuote(exe) + " guard hook", "timeout": secs}}}}}})
	return string(b)
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// Usage is the text for `smithy guard --help`.
const Usage = `usage: smithy guard check "<command>"   prints the block pattern it matches (exit 1) or nothing (exit 0)
       smithy guard hook                  Claude Code PreToolUse hook (event JSON on stdin)
       smithy guard list                  the effective blocklist and allowlist
The dangerous-command guard (on by default) checks every shell command an agent runs: the API
loop's shell tool, and Claude Code through this PreToolUse hook. A match asks the user through
smithy daemon (TUI, web, chat): run once, always allow this pattern (added to guard.allow), or
deny; no daemon or no answer within ask.timeout denies. Nothing else asks.
Settings: guard.enabled (default true), guard.block (regexes added to the defaults), guard.allow
(regexes that skip the check); both a JSON list or one regex per line.`

// Main runs smithy guard.
func Main(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, Usage)
		return 2
	}
	switch args[0] {
	case "check":
		if len(args) != 2 {
			fmt.Fprintln(os.Stderr, Usage)
			return 2
		}
		r, err := Load()
		if err != nil {
			fmt.Fprintln(os.Stderr, "smithy guard:", err)
			r.Allow = nil
		}
		if p := r.Match(args[1]); p != "" {
			fmt.Println(p)
			return 1
		}
		return 0
	case "list":
		r, err := Load()
		if err != nil {
			fmt.Fprintln(os.Stderr, "smithy guard:", err)
			return 1
		}
		fmt.Printf("enabled: %v\n", r.Enabled)
		for _, p := range r.Block {
			fmt.Println("block:", p)
		}
		for _, p := range r.Allow {
			fmt.Println("allow:", p)
		}
		return 0
	case "hook":
		if err := Hook(context.Background(), HookGuard(), os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	fmt.Fprintln(os.Stderr, Usage)
	return 2
}

// HookGuard is the Guard smithy guard hook and the API shell tool use; the harness build sets
// Connect to the local daemon's asker.
var HookGuard = func() Guard { return Guard{} }
