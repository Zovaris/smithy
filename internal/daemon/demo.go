//go:build harness

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Zovaris/smithy/internal/usage"
)

// DemoRunner is the scripted agent of `smithy daemon --demo`: every turn reads a file, runs the
// tests, asks one decision and answers, with no CLI, key or network. The recorded demo uses it.
func DemoRunner(agent, model, dir, sessionDir string) (Runner, error) {
	return demoRunner{}, nil
}

type demoRunner struct{}

func (demoRunner) Close() {}

func (demoRunner) Turn(ctx context.Context, msg string, _ []Exchange, e Emit) (string, error) {
	pause := func() error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(400 * time.Millisecond):
			return nil
		}
	}
	args := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	e.Text("Looking at the server first.")
	if err := pause(); err != nil {
		return "", err
	}
	e.Tool(ToolEvent{ID: "t1", Name: "Read", Args: args(map[string]string{"path": "cmd/server/main.go"}), Output: "142 lines", Took: "0.1s"})
	if err := pause(); err != nil {
		return "", err
	}
	e.Tool(ToolEvent{ID: "t2", Name: "Edit", Args: args(map[string]string{"path": "cmd/server/main.go"}), Output: "+12 −1", Took: "0.2s"})
	if err := pause(); err != nil {
		return "", err
	}
	e.Tool(ToolEvent{ID: "t3", Name: "Bash", Args: args(map[string]string{"command": "go test ./..."}), Output: "ok  ./cmd/server  0.4s", Took: "1.2s"})
	e.Usage(usage.Usage{Input: 1800, Output: 240})
	a, err := e.Ask(ctx, "Tests pass. Commit the change?", []string{"commit", "not yet"})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Done: %q is handled, tests pass, you answered %q.", msg, a), nil
}
