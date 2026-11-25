//go:build harness

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Zovaris/smithy/internal/usage"
)

// DemoRunner is the scripted agent of `smithy daemon --demo`: every turn reads a file, runs the
// tests, asks one decision and answers, with no CLI, key or network. The recorded demo uses it.
func DemoRunner(agent, model, dir, sessionDir string) (Runner, error) {
	return demoRunner{dir: dir}, nil
}

type demoRunner struct{ dir string }

func (demoRunner) Close() {}

// demoFile is what the scripted Write fills in, so the session's Changes show a real diff: it is
// created, or replaced only while it holds just demoStub (demo/run.sh commits that stub so git
// diff HEAD shows the change). Any other content is left alone.
const demoFile, demoStub = "health.go", "package main\n"

const demoHealth = `package main

import "net/http"

// health answers the load balancer's probe.
func health(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}
`

func (r demoRunner) Turn(ctx context.Context, msg string, _ []Exchange, e Emit) (string, error) {
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
	e.Tool(ToolEvent{ID: "t1", Name: "Read", Args: args(map[string]string{"path": "main.go"}), Output: "1 line", Took: "0.1s"})
	if err := pause(); err != nil {
		return "", err
	}
	edit := ToolEvent{ID: "t2", Name: "Write", Args: args(map[string]string{"path": demoFile}), Output: "+8", Took: "0.2s"}
	path := filepath.Join(r.dir, demoFile)
	if b, err := os.ReadFile(path); err == nil && string(b) != demoStub {
		edit.Output = demoFile + " exists, left as is"
	} else if err := os.WriteFile(path, []byte(demoHealth), 0o644); err != nil {
		edit.Output, edit.IsError = err.Error(), true
	}
	e.Tool(edit)
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
