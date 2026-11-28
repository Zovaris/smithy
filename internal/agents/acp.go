package agents

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/acp"
	"github.com/Zovaris/smithy/internal/decisions"
	"github.com/Zovaris/smithy/internal/events"
	"github.com/Zovaris/smithy/internal/layout"
	"github.com/Zovaris/smithy/internal/usage"
)

var acpGrace = 5 * time.Second

var addDecision = func(q, choice, why, ref string) error {
	safe := strings.NewReplacer("·", "-", "→", "->")
	return decisions.Add(safe.Replace(q), choice, why, "self", safe.Replace(ref))
}

type acpSink struct {
	out   io.Writer
	allow bool
	log   string
	item  events.Event // batch and item of a warden log, copied onto each event
	auto  bool         // --profile auto: each request goes through autoPermit
	cwd   string
	bins  []string
}

func (s acpSink) event(kind, detail string) {
	e := s.item
	e.Kind, e.Detail = kind, flatDetail(detail)
	_ = events.Append(e)
}

func flatDetail(s string) string {
	if r := []rune(s); len(r) > 200 {
		return string(r[:199]) + "…"
	}
	return s
}

func (s acpSink) Text(t string) { _, _ = io.WriteString(s.out, t) }

func (s acpSink) Tool(t acp.Tool) {
	if t.Status == "completed" || t.Status == "failed" {
		s.event("tool", t.Title+": "+t.Status)
	}
}

func (s acpSink) Permit(p acp.Permission) string {
	allow, why := s.allow, "read-only run: smithy refuses every ACP tool permission"
	if allow && s.auto {
		var reason string
		if allow, reason = autoPermit(p, s.cwd, s.bins); !allow {
			why = "--profile auto: " + reason
		}
	}
	id := acp.Choose(p.Options, allow)
	verdict := "allowed"
	if !allow {
		verdict = "rejected"
		_ = addDecision("let the agent run "+p.Title+"?", verdict, why, s.log)
	}
	s.event("permission", p.Title+": "+verdict)
	return id
}

func wardenItem(log string) events.Event {
	var e events.Event
	if dir := filepath.Dir(log); log != "" && realPath(filepath.Dir(dir)) == realPath(layout.WardenHome()) {
		e.Batch, e.Item = filepath.Base(dir), strings.TrimSuffix(filepath.Base(log), ".log")
	}
	return e
}

func runACP(cmd *exec.Cmd, o Options, prompt string, out *os.File) int {
	inR, inW, err := os.Pipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		inR.Close()
		inW.Close()
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer outR.Close()
	cmd.Stdin, cmd.Stdout = inR, outW
	item := wardenItem(o.Log)
	sink := acpSink{out: out, allow: !o.ReadOnly, log: o.Log, item: item, auto: o.Profile == "auto", cwd: o.Cwd, bins: o.SmithyBins}
	type result struct {
		r   acp.Result
		err error
	}
	done := make(chan result, 1)
	ended := make(chan struct{})
	go func() {
		r, err := acp.Run(outR, inW, acp.Session{Cwd: o.Cwd, Prompt: prompt}, sink)
		inW.Close()
		close(ended) // on a protocol error too, so the child is stopped after the grace
		done <- result{r, err}
	}()
	rc := waitUntil(cmd, o.Timeout, out, ended)
	inR.Close()
	outW.Close()
	var res result
	select {
	case res = <-done:
	case <-time.After(acpGrace):
		outR.Close() // a child the agent left holds its stdout open
		res = <-done
	}
	if res.err != nil {
		fmt.Fprintf(out, "\nsmithy agent: %v\n", res.err)
		if rc == 0 {
			rc = 1
		}
	} else {
		fmt.Fprintln(out)
		if res.r.StopReason != "end_turn" {
			fmt.Fprintf(out, "smithy agent: the agent stopped: %s\n", res.r.StopReason)
		}
	}
	if res.r.HasUsage {
		u := res.r.Usage
		if o.Log != "" {
			_ = usage.Write(out.Name(), u)
		}
		e := item
		e.Kind, e.Usage, e.Account, e.Model = "usage", &u, o.Account, o.Model
		e.Agent = Kind(filepath.Base(cmd.Path))
		if o.Log != "" {
			e.Detail = filepath.Base(o.Log)
		}
		_ = events.Append(e)
	}
	return rc
}
