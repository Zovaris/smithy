package workflow

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/guard"
	"github.com/Zovaris/smithy/internal/layout"
	"github.com/Zovaris/smithy/internal/usage"
)

// Shell runs cmd with sh -c (cmd /C on Windows) in dir (home when empty) and returns its
// combined output and exit code; a timeout kills it.
func Shell(ctx context.Context, dir, cmd string) (string, int, error) {
	c := exec.CommandContext(ctx, "sh", "-c", cmd)
	if runtime.GOOS == "windows" {
		c = exec.CommandContext(ctx, "cmd", "/C", cmd)
	}
	c.Dir = dirOr(dir)
	c.WaitDelay = 5 * time.Second
	var out bytes.Buffer
	c.Stdout, c.Stderr = &out, &out
	err := c.Run()
	if ctx.Err() != nil {
		return out.String(), -1, fmt.Errorf("timed out")
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return out.String(), ee.ExitCode(), nil
	}
	return out.String(), 0, err
}

func dirOr(dir string) string {
	if dir == "" {
		return layout.UserHome()
	}
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		return filepath.Join(layout.UserHome(), dir[1:])
	}
	return dir
}

// Agent runs one read-only agent through `smithy agent --profile auto` (the unattended
// allow-list and auto.sandbox, failing closed) and returns its answer and known cost.
func Agent(ctx context.Context, dir, prompt string, timeout time.Duration) (string, float64, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", 0, err
	}
	tmp, err := os.MkdirTemp("", "smithy-workflow-")
	if err != nil {
		return "", 0, err
	}
	defer os.RemoveAll(tmp)
	pf, log := filepath.Join(tmp, "prompt.md"), filepath.Join(tmp, "agent.log")
	if err := os.WriteFile(pf, []byte(prompt), 0o600); err != nil {
		return "", 0, err
	}
	mins := int(timeout.Minutes())
	if mins < 1 {
		mins = 1
	}
	c := exec.CommandContext(ctx, exe, "agent", "--readonly", "--profile", "auto", "--timeout", strconv.Itoa(mins),
		"--cwd", dirOr(dir), "--log", log, pf)
	c.Dir = dirOr(dir)
	c.WaitDelay = 5 * time.Second
	var out, errb bytes.Buffer
	c.Stdout, c.Stderr = &out, &errb
	err = c.Run()
	cost := 0.0
	if u, ok := usage.Read(log); ok && u.CostUSD != nil {
		cost = *u.CostUSD
	}
	if err != nil {
		msg := bytes.TrimSpace(errb.Bytes())
		if len(msg) > 300 {
			msg = msg[len(msg)-300:]
		}
		return out.String(), cost, fmt.Errorf("agent: %v: %s", err, msg)
	}
	return out.String(), cost, nil
}

// Guard is the guard's block check (guard.block, guard.allow); a bad setting fails closed on
// the built-in defaults.
func Guard(cmd string) string {
	r, _ := guard.Load()
	return r.Match(cmd)
}
