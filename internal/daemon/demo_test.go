//go:build harness

package daemon

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/usage"
)

type demoEmit struct {
	texts, tools []string
	asked        string
	answer       string
}

func (r *demoEmit) Text(t string)     { r.texts = append(r.texts, t) }
func (r *demoEmit) Tool(t ToolEvent)  { r.tools = append(r.tools, t.Name) }
func (r *demoEmit) Usage(usage.Usage) {}
func (r *demoEmit) Ask(_ context.Context, q string, _ []string) (string, error) {
	r.asked = q
	return r.answer, nil
}

func TestDemoRunnerShowsToolsAndAsks(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, demoFile), []byte(demoStub), 0o644)
	r, err := DemoRunner("claude", "", dir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	e := &demoEmit{answer: "approve"}
	out, err := r.Turn(context.Background(), "add a health check", nil, e)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.tools) < 2 || !slices.Contains(e.tools, "Read") || !slices.Contains(e.tools, "Bash") {
		t.Errorf("tools %v: want Read and Bash", e.tools)
	}
	if e.asked == "" {
		t.Error("the demo turn never asks a decision")
	}
	if b, err := os.ReadFile(filepath.Join(dir, demoFile)); err != nil || string(b) != demoHealth {
		t.Errorf("the demo edit left no %s in the folder: %v", demoFile, err)
	}
	if out == "" {
		t.Error("empty final answer")
	}
}

func TestDemoFlag(t *testing.T) {
	f, rest, err := splitFlags([]string{"--demo", "--port", "0"})
	if err != nil || !f.demo || len(rest) != 0 {
		t.Fatalf("flags %+v rest %v err %v", f, rest, err)
	}
}

func TestDemoRunnerLeavesOtherFilesAlone(t *testing.T) {
	dir := t.TempDir()
	mine := "package main\n\nfunc mine() {}\n"
	os.WriteFile(filepath.Join(dir, demoFile), []byte(mine), 0o644)
	r, _ := DemoRunner("claude", "", dir, t.TempDir())
	if _, err := r.Turn(context.Background(), "x", nil, &demoEmit{answer: "commit"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, demoFile)); string(b) != mine {
		t.Errorf("demo overwrote a real %s: %q", demoFile, b)
	}
}

// The demo lists only the agents on its PATH, never the host's from its usual install folders.
func TestDemoReadyLooksOnlyOnPath(t *testing.T) {
	home, bin := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", bin)
	os.MkdirAll(filepath.Join(home, ".local", "bin"), 0o755)
	os.WriteFile(filepath.Join(home, ".local", "bin", "opencode"), []byte("#!/bin/sh\n"), 0o755)
	os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\n"), 0o755)
	var ready []string
	for _, r := range DemoReady() {
		if r.Ready && r.Kind == "cli" {
			ready = append(ready, r.Name)
		}
	}
	if !slices.Equal(ready, []string{"claude"}) {
		t.Fatalf("demo ready CLIs = %v, want only claude from PATH", ready)
	}
}

func TestDemoRunnerRefineEndsWithARefinedBlock(t *testing.T) {
	r, _ := DemoRunner("claude", "", t.TempDir(), t.TempDir())
	defer r.Close()
	e := &demoEmit{}
	out, err := r.Turn(context.Background(), "refine: add dark mode", nil, e)
	if err != nil || !strings.Contains(out, "```refined\n# Goal") || e.asked != "" || len(e.tools) != 0 {
		t.Fatalf("refine turn: %q %v asked=%q tools=%v", out, err, e.asked, e.tools)
	}
}

func TestDemoLinkerLinksAPRAndIssuesWithoutNetwork(t *testing.T) {
	k := DemoLinker()
	l := k.collect("s-1", t.TempDir(), nil, nil, []string{"fix the health check"})
	if len(l.PRs) != 1 || l.PRs[0].Number != 42 || len(l.Issues) != 2 || l.Issues[0].ID != "TK-112" || l.Issues[1].ID != "#12" || l.Error != "" {
		t.Fatalf("demo links %+v", l)
	}
}
