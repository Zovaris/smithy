package agents

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Zovaris/smithy/internal/apiloop"
	"github.com/Zovaris/smithy/internal/apiloop/tools"
	"github.com/Zovaris/smithy/internal/auth"
	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/events"
	"github.com/Zovaris/smithy/internal/proof"
	"github.com/Zovaris/smithy/internal/usage"
)

// APIProvider builds a provider for `smithy agent --backend api`; the harness build sets it
// (internal/apiloop/provider), the skills build leaves it nil.
var APIProvider func(name, key, baseURL string) (apiloop.Provider, error)

// CodeMode adds run_code (a sandboxed JS program over the tools) to the loop's tools; the harness
// build sets it. withCodeMode applies it only when agent.code_mode is on and there are tools.
var CodeMode func(apiloop.Tools) apiloop.Tools

func withCodeMode(tools apiloop.Tools, on bool) apiloop.Tools {
	if tools == nil || !on || CodeMode == nil {
		return tools
	}
	return CodeMode(tools)
}

// APITarget is the "<provider>/<model>" an API run uses: --model under --backend api, or
// AGENT_CLI (auto.agent) api:<provider>/<model>. ok is false for a CLI run.
func APITarget(o Options, cli string) (target string, ok bool) {
	if o.Backend == "api" {
		return o.Model, true
	}
	if strings.HasPrefix(cli, "api:") {
		return strings.TrimPrefix(cli, "api:"), true
	}
	return "", false
}

type apiSink struct {
	out  io.Writer
	item events.Event
	log  string
}

func (s apiSink) event(kind, detail string) {
	e := s.item
	e.Kind, e.Detail = kind, flatDetail(detail)
	_ = events.Append(e)
}

func (s apiSink) Text(t string) { io.WriteString(s.out, t) }

func (s apiSink) Tool(c apiloop.ToolCall, r apiloop.ToolResult) {
	st := "completed"
	if r.IsError {
		st = "failed"
	}
	s.event("tool", c.Name+": "+st)
}

func (s apiSink) Permission(c apiloop.ToolCall, allowed bool, why string) {
	verdict := "allowed"
	if !allowed {
		verdict = "rejected"
		_ = addDecision("let the agent run "+c.Name+"?", verdict, why, s.log)
	}
	s.event("permission", c.Name+": "+verdict)
}

func (s apiSink) Usage(usage.Usage) {}

func runAPI(o Options, cli string) int {
	target, _ := APITarget(o, cli)
	name, model, err := apiloop.ParseTarget(target)
	if err != nil {
		fmt.Fprintln(os.Stderr, "smithy agent:", err)
		return 2
	}
	if APIProvider == nil {
		return needsHarness("agent --backend api")
	}
	mode, err := SandboxMode()
	if err == nil && mode == "container" {
		err = errAPIContainer
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "smithy agent: auto.sandbox: %v; worker not started (fails closed)\n", err)
		return 1
	}
	key := ""
	if svc := apiloop.Providers[name].Service; svc != "" {
		if key = auth.Stored(svc); key == "" {
			fmt.Fprintf(os.Stderr, "smithy agent: no %s key; store one with: smithy auth set %s\n", name, svc)
			return 2
		}
	}
	prompt, err := os.ReadFile(o.PromptFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if o.Cwd == "" {
		o.Cwd, _ = os.Getwd()
	}
	out, log, err := openLog(o)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if o.Bg {
		o.Backend, o.Model = "api", name+"/"+model
		cmd, err := supervisor(o, out.Name())
		if err == nil {
			detach(cmd)
			err = cmd.Start()
		}
		out.Close()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		launchCard(log, o.Cwd, cmd.Process.Pid)
		fmt.Println(strconv.Itoa(cmd.Process.Pid), log)
		return 0
	}
	p, err := APIProvider(name, key, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "smithy agent:", proof.Redact(err.Error()))
		out.Close()
		return 1
	}
	ctx, cancel := context.Background(), context.CancelFunc(func() {})
	if o.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, o.Timeout)
	}
	defer cancel()
	item := wardenItem(o.Log)
	set, err := apiTools(o, model, mode, HostProbe())
	if err != nil {
		fmt.Fprintf(os.Stderr, "smithy agent: auto.sandbox: %v; worker not started (fails closed)\n", err)
		out.Close()
		return 1
	}
	defer set.Close()
	loopTools := withCodeMode(set, config.Get("agent.code_mode", "false") == "true")
	res, err := apiloop.Run(ctx, p, apiloop.Request{Model: model, Messages: []apiloop.Message{apiloop.UserText(strings.TrimSpace(string(prompt)))}},
		loopTools, apiSink{out: out, item: item, log: o.Log}, apiloop.Options{Provider: name})
	fmt.Fprintln(out)
	rc := 0
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		fmt.Fprintf(out, "smithy agent: timed out after %sm\n", minutes(o.Timeout))
		rc = 124
	case err != nil:
		fmt.Fprintf(out, "smithy agent: %s\n", proof.Redact(err.Error()))
		rc = 1
	case res.Stop != apiloop.StopEnd:
		fmt.Fprintf(out, "smithy agent: the agent stopped: %s\n", res.Stop)
	}
	if res.Turns > 0 {
		u := res.Usage
		if o.Log != "" {
			_ = usage.Write(out.Name(), u)
		}
		e := item
		e.Kind, e.Usage = "usage", &u
		if o.Log != "" {
			e.Detail = filepath.Base(o.Log)
		}
		_ = events.Append(e)
	}
	out.Close()
	if b, err := os.ReadFile(out.Name()); err == nil {
		os.Stdout.Write(b)
	}
	if o.Log == "" {
		os.Remove(out.Name())
	}
	return rc
}

// apiTools is the core tool set under the --profile auto rules: AutoTools allowed, AutoDenied
// (with SecretDenied) winning, and fetch limited to the sandbox domain allow-list. A read-only run
// gets only the read tools; auto.sandbox native runs its shell commands under the OS sandbox.
func apiTools(o Options, model, mode string, p Probe) (*tools.Set, error) {
	extra, _ := SandboxSetting("auto.sandbox_domains", "")
	s := &tools.Set{Cwd: o.Cwd, Model: model, Allow: AutoTools(o.SmithyBins), Deny: AutoDenied(o.SmithyBins), Hosts: SandboxDomains(extra), ReadOnly: o.ReadOnly}
	if v := os.Getenv("HTTPS_PROXY"); v != "" {
		if u, err := url.Parse(v); err == nil && u.Host != "" {
			s.Proxy = u
		}
	}
	if mode == "native" {
		paths, err := apiSandboxPaths(o.Cwd)
		if err != nil {
			return nil, err
		}
		if s.Wrap, err = apiShellSandbox(mode, p, paths); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func openLog(o Options) (*os.File, string, error) {
	if o.Log == "" {
		f, err := os.CreateTemp("", "agent.")
		if err != nil {
			return nil, "", err
		}
		return f, f.Name(), nil
	}
	path := o.Log
	if !filepath.IsAbs(path) {
		path = filepath.Join(o.Cwd, path)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	return f, o.Log, err
}

func needsHarness(what string) int {
	fmt.Fprintf(os.Stderr, "smithy %s needs the harness build; this is the skills build. Build it: go build -tags harness ./cmd/smithy\n", what)
	return 2
}
