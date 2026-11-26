package models

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/acp"
	"github.com/Zovaris/smithy/internal/agents"
	"github.com/Zovaris/smithy/internal/auth"
	"github.com/Zovaris/smithy/internal/layout"
)

const probeTimeout = 30 * time.Second

// DefaultDeps runs the real commands, probes and requests, caching under smithy's home.
func DefaultDeps() Deps {
	return Deps{
		Dir:         filepath.Join(layout.Home(), "cache", "models"),
		Now:         time.Now,
		Output:      output,
		ACP:         probeACP,
		Get:         get,
		Key:         auth.Stored,
		ClaudeModel: claudeModel,
	}
}

func command(ctx context.Context, argv []string) (*exec.Cmd, error) {
	path := agents.CLIPath(os.Getenv("PATH"), os.Getenv)
	bin := argv[0]
	for _, dir := range filepath.SplitList(path) {
		if p := filepath.Join(dir, argv[0]); isExec(p) {
			bin = p
			break
		}
	}
	if !filepath.IsAbs(bin) {
		return nil, fmt.Errorf("%s not found", argv[0])
	}
	cmd := exec.CommandContext(ctx, bin, argv[1:]...)
	cmd.Env = append(agents.Scrub(os.Environ()), "PATH="+path)
	cmd.Dir = os.TempDir()
	cmd.WaitDelay = 2 * time.Second
	return cmd, nil
}

func isExec(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}

func output(ctx context.Context, argv ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	cmd, err := command(ctx, argv)
	if err != nil {
		return nil, err
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", strings.Join(argv, " "), err)
	}
	return out, nil
}

// probeACP starts the agent, opens one session to read its models and stops it before any prompt.
func probeACP(ctx context.Context, argv []string) (acp.ModelList, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	cmd, err := command(ctx, argv)
	if err != nil {
		return acp.ModelList{}, err
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		return acp.ModelList{}, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return acp.ModelList{}, err
	}
	if err := cmd.Start(); err != nil {
		return acp.ModelList{}, err
	}
	defer func() {
		in.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	type res struct {
		l   acp.ModelList
		err error
	}
	ch := make(chan res, 1)
	go func() {
		l, err := acp.Models(out, in, os.TempDir())
		ch <- res{l, err}
	}()
	select {
	case r := <-ch:
		return r.l, r.err
	case <-ctx.Done():
		return acp.ModelList{}, fmt.Errorf("%s: no answer in %s", argv[0], probeTimeout)
	}
}

func get(ctx context.Context, u string, h http.Header) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header = h
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("models endpoint: " + resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 32<<20))
}

// claudeModel is the model set in Claude Code's user settings, "" when none.
func claudeModel() string {
	h := layout.UserHome()
	if h == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(h, ".claude", "settings.json"))
	if err != nil {
		return ""
	}
	var s struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(b, &s)
	return s.Model
}
