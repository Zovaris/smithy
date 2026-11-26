//go:build harness

package daemon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Zovaris/smithy/internal/agents"
	"github.com/Zovaris/smithy/internal/apiloop"
	"github.com/Zovaris/smithy/internal/proc"
	"github.com/Zovaris/smithy/internal/text"
	"github.com/Zovaris/smithy/internal/usage"
)

// DefaultRunner runs api:<provider>/<model> in the daemon (smithy's own loop, so tool calls,
// usage and permission requests reach the session live) and every other agent through
// `smithy agent`, one process per turn.
func DefaultRunner(agent, model, dir, sessionDir string) (Runner, error) {
	if strings.HasPrefix(agent, "api:") {
		c, err := agents.NewAPIChat(agent, dir)
		if err != nil {
			return nil, err
		}
		return &apiRunner{chat: c}, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return &cliRunner{exe: exe, agent: agent, model: model, dir: dir, sdir: sessionDir}, nil
}

type apiRunner struct {
	chat *agents.APIChat
	msgs []apiloop.Message
}

func (r *apiRunner) Close() { r.chat.Close() }

func (r *apiRunner) Turn(ctx context.Context, msg string, _ []Exchange, e Emit) (string, error) {
	req := apiloop.Request{Model: r.chat.Model, System: SystemPrompt("api:", hasRunCode(r.chat.Tools)), Messages: append(r.msgs, apiloop.UserText(msg))}
	s := &apiSink{e: e}
	t := &askTools{Tools: r.chat.Tools, hard: r.chat.Hard, ctx: ctx, e: e}
	res, err := apiloop.Run(ctx, r.chat.Provider, req, t, s, apiloop.Options{Provider: r.chat.Name})
	if len(res.Messages) > 0 {
		r.msgs = res.Messages
	}
	return s.reply.String(), err
}

type askTools struct {
	apiloop.Tools
	hard apiloop.Tools
	ctx  context.Context
	e    Emit
}

func (t *askTools) Permit(c apiloop.ToolCall) (bool, string) {
	ok, why := t.Tools.Permit(c)
	if ok {
		return true, ""
	}
	if hok, hwhy := t.hard.Permit(c); !hok {
		return false, hwhy
	}
	if len(c.Args) > MaxAskArgs {
		return false, why + "; too long to show a person in full, so not asked"
	}
	q := "Let the agent run " + c.Name + " " + string(c.Args) + "? (" + line1(why, 200) + ")"
	a, err := t.e.Ask(t.ctx, q, []string{"allow", "deny"})
	if err != nil {
		return false, "no answer: " + err.Error()
	}
	if a != "allow" {
		return false, "a person denied it"
	}
	return true, ""
}

type apiSink struct {
	e     Emit
	reply strings.Builder
}

func (s *apiSink) Text(t string) { s.reply.WriteString(t); s.e.Text(t) }

func (s *apiSink) Tool(c apiloop.ToolCall, r apiloop.ToolResult) {
	s.e.Tool(ToolEvent{ID: c.ID, Name: c.Name, Args: c.Args, Output: r.Output, IsError: r.IsError})
}

func (s *apiSink) Permission(apiloop.ToolCall, bool, string) {}

func (s *apiSink) Usage(u usage.Usage) { s.e.Usage(u) }

type cliRunner struct {
	exe, agent, model, dir, sdir string
	effort                       string
	n                            int
}

func (r *cliRunner) Close() {}

const stopGrace = 5 * time.Second

// MaxAskArgs is the longest tool call shown in full in a permission question; a longer one is refused.
const MaxAskArgs = 2 << 10

// MaxTranscript caps the earlier conversation put in a CLI turn's prompt (the newest is kept).
const MaxTranscript = 48 << 10

// CLIPrompt is the prompt of one CLI turn: the conversation so far, then the new message.
func CLIPrompt(history []Exchange, msg string) string {
	if len(history) == 0 {
		return msg
	}
	var b strings.Builder
	for _, x := range history {
		who := "User"
		if x.Role == "agent" {
			who = "You"
		}
		fmt.Fprintf(&b, "%s: %s\n\n", who, x.Text)
	}
	h := b.String()
	if len(h) > MaxTranscript {
		h = "…" + h[len(h)-MaxTranscript:]
	}
	return "This continues an earlier conversation in this folder. Conversation so far:\n\n" + h +
		"---\nThe user's new message:\n\n" + msg
}

// effortSetter is a runner that passes a reasoning effort to its agent.
type effortSetter interface{ SetEffort(string) }

// SetEffort sets the effort the next turns pass as $AGENT_EFFORT ("": the agent.effort setting).
func (r *cliRunner) SetEffort(e string) { r.effort = e }

// env is a turn's environment: the agent, its model and effort, the claude stream flags.
func (r *cliRunner) env() []string {
	env := agents.Scrub(os.Environ())
	env = append(env, "SMITHY_EVENTS=off")
	if a := strings.TrimPrefix(r.agent, "acp:"); a != "" {
		env = append(env, "AGENT_CLI="+a)
	}
	if strings.HasPrefix(r.agent, "acp:") {
		env = append(env, "AGENT_PROTOCOL=acp")
	}
	if r.model != "" {
		env = append(env, "AGENT_MODEL="+r.model)
	}
	if r.effort != "" {
		env = append(env, "AGENT_EFFORT="+r.effort)
	}
	if r.agent == "claude" {
		env = append(env, "AGENT_ARGS=--output-format stream-json --verbose --append-system-prompt "+quoteArg(SystemPrompt(r.agent, false)))
	}
	return append(env, "PATH="+agents.CLIPath(os.Getenv("PATH"), os.Getenv))
}

func (r *cliRunner) Turn(ctx context.Context, msg string, history []Exchange, e Emit) (string, error) {
	r.n++
	base := filepath.Join(r.sdir, "turn-"+strconv.Itoa(r.n))
	prompt, log := base+".prompt.md", base+".log"
	if err := os.WriteFile(prompt, []byte(withSystem(r.agent, false, CLIPrompt(history, msg))), 0o600); err != nil {
		return "", err
	}
	cmd := exec.Command(r.exe, "agent", "--cwd", r.dir, "--log", log, prompt)
	cmd.Dir = r.dir
	var cs *claudeStream
	if r.agent == "claude" {
		cs = newClaudeStream()
	}
	cmd.Env = r.env()
	var errOut capped
	errOut.n = 4 << 10
	cmd.Stdout, cmd.Stderr = io.Discard, &errOut
	proc.Tree(cmd)
	cmd.Cancel = nil
	if err := cmd.Start(); err != nil {
		return "", err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var off int64
	follow := func(all bool) {
		f, err := os.Open(log)
		if err != nil {
			return
		}
		defer f.Close()
		if fi, err := f.Stat(); err != nil || fi.Size() < off {
			return
		}
		_, _ = f.Seek(off, io.SeekStart)
		b, _ := io.ReadAll(io.LimitReader(f, 1<<20))
		if i := bytes.LastIndexByte(b, '\n'); !all {
			b = b[:i+1]
		}
		off += int64(len(b))
		var keep []byte
		for _, l := range bytes.SplitAfter(b, []byte("\n")) {
			if cs != nil && cs.line(l, e) {
				continue
			}
			if !bytes.HasPrefix(l, []byte(`{"type":`)) {
				keep = append(keep, l...)
			}
		}
		if len(keep) > 0 {
			e.Text(string(keep))
		}
	}
	tk := time.NewTicker(250 * time.Millisecond)
	defer tk.Stop()
	var werr error
loop:
	for {
		select {
		case werr = <-done:
			break loop
		case <-ctx.Done():
			_ = cmd.Process.Signal(syscall.SIGTERM)
			select {
			case werr = <-done:
			case <-time.After(stopGrace):
				_ = proc.Kill(cmd)
				werr = <-done
			}
			break loop
		case <-tk.C:
			follow(false)
		}
	}
	follow(true)
	if cs != nil && cs.usage != nil {
		e.Usage(*cs.usage)
	} else if u, ok := usage.Read(log); ok {
		e.Usage(u)
	}
	var out capped
	out.n = MaxPlanOutput
	if f, err := os.Open(log); err == nil {
		_, _ = io.Copy(&out, f)
		f.Close()
	}
	reply := out.b.String()
	if cs != nil {
		reply = cs.result
	}
	if ctx.Err() != nil {
		return reply, ctx.Err()
	}
	if werr != nil {
		msg := strings.TrimSpace(text.Clean(errOut.b.String()))
		if msg == "" {
			msg = werr.Error()
		}
		return reply, errors.New("smithy agent: " + line1(msg, 300))
	}
	return reply, nil
}

var diffMu sync.Mutex

func (sv *Server) emitDiff(id, dir string) {
	git := func(args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		c := exec.CommandContext(ctx, "git", append([]string{"-C", dir, "--no-pager", "-c", "core.quotepath=off"}, args...)...)
		var out capped
		out.n = MaxDiff + 1
		c.Stdout = &out
		err := c.Run()
		return out.b.String(), err
	}
	if _, err := git("rev-parse", "--verify", "-q", "HEAD"); err != nil {
		return
	}
	stat, err := git("diff", "HEAD", "--stat")
	if err != nil {
		return
	}
	patch, _ := git("diff", "HEAD", "--no-color", "--no-ext-diff")
	sum := sha256.Sum256([]byte(patch))
	key := string(sum[:])
	diffMu.Lock()
	defer diffMu.Unlock()
	sv.mu.Lock()
	l := sv.sessions[id]
	if l == nil || l.lastDiff == key || (l.lastDiff == "" && strings.TrimSpace(patch) == "") {
		sv.mu.Unlock()
		return
	}
	l.lastDiff = key
	d := map[string]any{"stat": stat, "patch": clipBytes(patch, MaxDiff), "truncated": len(patch) > MaxDiff}
	_, _ = sv.appendLocked(KindSessionDiff, id, d)
	sv.mu.Unlock()
}
