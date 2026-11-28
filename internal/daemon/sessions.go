//go:build harness

package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/agents"
	"github.com/Zovaris/smithy/internal/apiloop"
	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/events"
	"github.com/Zovaris/smithy/internal/layout"
	"github.com/Zovaris/smithy/internal/proof"
	"github.com/Zovaris/smithy/internal/store"
	"github.com/Zovaris/smithy/internal/text"
	"github.com/Zovaris/smithy/internal/usage"
)

const (
	KindSessionStarted = "session_started"
	KindSessionMessage = "session_message"
	KindSessionText    = "session_text"
	KindSessionTool    = "session_tool"
	KindSessionDiff    = "session_diff"
	KindSessionUsage   = "session_usage"
	KindSessionStatus  = "session_status"
	KindSessionModel   = "session_model"
	KindSessionMail    = "session_mail"
)

const (
	StatusWorking  = "working"
	StatusNeedsYou = "needs_you"
	StatusDone     = "done"
	StatusFailed   = "failed"
)

const (
	MaxSessions     = 16
	MaxMessage      = 64 << 10
	MaxQueued       = 8
	MaxToolField    = 16 << 10
	MaxQuestion     = 4 << 10
	MaxDiff         = 64 << 10
	permitWait      = 24 * time.Hour
	sessionIDPrefix = "s-"
)

// ToolEvent is one finished tool call of a session.
type ToolEvent struct {
	ID      string          `json:"id,omitempty"`
	Name    string          `json:"name"`
	Args    json.RawMessage `json:"args,omitempty"`
	Output  string          `json:"output,omitempty"`
	IsError bool            `json:"is_error,omitempty"`
	Took    string          `json:"took,omitempty"`
}

// Exchange is one past message, oldest first, given to a runner that keeps no history itself.
type Exchange struct{ Role, Text string }

// Emit is how a runner reports a turn. Ask blocks until a person answers a decision (or ctx ends).
type Emit interface {
	Text(t string)
	Tool(t ToolEvent)
	Usage(u usage.Usage)
	Ask(ctx context.Context, question string, options []string) (string, error)
}

// Runner runs the turns of one session, one at a time. Close ends it.
type Runner interface {
	Turn(ctx context.Context, msg string, history []Exchange, e Emit) (string, error)
	Close()
}

// RunnerFor makes the runner for a validated agent in dir; sessionDir is the session's own folder.
type RunnerFor func(agent, model, dir, sessionDir string) (Runner, error)

type live struct {
	s        Session
	runner   Runner
	inbox    chan string
	cancel   context.CancelFunc
	done     chan struct{}
	history  []Exchange
	asks     int
	stopped  bool
	lastDiff string
	links    *SessionLinks
	mail     []Mail
	sent     []time.Time
	goalBase float64
}

var (
	modelChars = regexp.MustCompile(`^[A-Za-z0-9._:/@+-]{1,128}$`)
)

// ValidAgent reports whether agent names something smithy agent can run: "" (the default CLI),
// a CLI name, acp:<cli> or api:<provider>/<model>. Nothing else reaches a command line.
func ValidAgent(agent string) error {
	switch {
	case agent == "":
		return nil
	case strings.HasPrefix(agent, "api:"):
		if !modelChars.MatchString(strings.TrimPrefix(agent, "api:")) {
			return errors.New("agent: api:<provider>/<model> with letters, digits and ._:/@+- only")
		}
		_, _, err := apiloop.ParseTarget(agent)
		return err
	case slices.Contains(agents.CLIs, strings.TrimPrefix(agent, "acp:")):
		return nil
	}
	return errors.New("agent: want a CLI name (claude, codex, …), acp:<cli> or api:<provider>/<model>")
}

func cleanDir(dir string) (string, error) {
	if dir == "" || !filepath.IsAbs(dir) {
		return "", errors.New("dir: an absolute path to an existing folder")
	}
	d, err := filepath.EvalSymlinks(filepath.Clean(dir))
	if err != nil {
		return "", errors.New("dir: no such folder")
	}
	fi, err := os.Stat(d)
	if err != nil || !fi.IsDir() {
		return "", errors.New("dir: not a folder")
	}
	return d, nil
}

func newSessionID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return sessionIDPrefix + hex.EncodeToString(b)
}

func titleOf(msg string) string {
	t := line1(msg, 80)
	if t == "" {
		return "New session"
	}
	return t
}

func (sv *Server) applySession(e store.Event) {
	switch e.Kind {
	case KindSessionStarted:
		var s Session
		if json.Unmarshal(e.Data, &s) == nil && s.ID != "" {
			s.Created, s.Updated = e.Time, e.Time
			sv.sessions[s.ID] = &live{s: s}
			sv.lastAgent, sv.lastModel = s.Agent, s.Model
		}
	case KindSessionModel:
		var c struct{ Model, Effort string }
		if l := sv.sessions[e.Session]; l != nil && json.Unmarshal(e.Data, &c) == nil {
			l.s.Model, l.s.Effort, l.s.Updated = c.Model, c.Effort, e.Time
		}
	case KindSessionGoal:
		if l := sv.sessions[e.Session]; l != nil {
			sv.applyGoal(l, e.Data)
		}
	case KindSessionStatus:
		var st struct{ Status, Detail string }
		if l := sv.sessions[e.Session]; l != nil && json.Unmarshal(e.Data, &st) == nil {
			l.s.Status, l.s.Detail, l.s.Updated = st.Status, st.Detail, e.Time
		}
	case KindSessionMessage:
		var m struct{ Role, Text string }
		if l := sv.sessions[e.Session]; l != nil && json.Unmarshal(e.Data, &m) == nil {
			l.history = append(l.history, Exchange{m.Role, m.Text})
			l.s.Updated = e.Time
		}
	case KindSessionTool:
		var t ToolEvent
		if json.Unmarshal(e.Data, &t) == nil {
			sv.notePRsLocked(e.Session, t)
		}
	case KindSessionLink, KindSessionLinks:
		sv.applyLinks(e)
	case KindSessionMail:
		sv.applyMailLocked(e)
	case KindSessionUsage:
		var u usage.Usage
		if l := sv.sessions[e.Session]; l != nil && json.Unmarshal(e.Data, &u) == nil {
			t := u
			if l.s.Usage != nil {
				t = l.s.Usage.Add(u)
			}
			l.s.Usage = &t
		}
	}
}

func (sv *Server) failOrphans() {
	for id, d := range sv.decisions {
		if strings.HasPrefix(id, "perm:") && d.Outcome == nil {
			_, _ = sv.answerLocked(id, "deny", "smithy")
		}
	}
	for id, l := range sv.sessions {
		if l.s.Status == StatusWorking || l.s.Status == StatusNeedsYou {
			sv.setStatusLocked(id, StatusFailed, "the daemon restarted during the turn; send a message to go on")
		}
	}
}

func (sv *Server) setStatusLocked(id, status, detail string) {
	l := sv.sessions[id]
	if l == nil || (l.s.Status == status && l.s.Detail == detail) {
		return
	}
	if _, err := sv.appendLocked(KindSessionStatus, id, map[string]string{"status": status, "detail": detail}); err == nil {
		l.s.Status, l.s.Detail, l.s.Updated = status, detail, time.Now().UTC()
	}
}

func (sv *Server) setStatus(id, status, detail string) {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	sv.setStatusLocked(id, status, detail)
}

// ValidModel reports whether model may reach a CLI's --model flag.
func ValidModel(model string) error {
	if model != "" && (!modelChars.MatchString(model) || strings.HasPrefix(model, "-")) {
		return errors.New("model: letters, digits and ._:/@+- only, not starting with -")
	}
	return nil
}

// Defaults is the agent and model a session starts with when none is named: the last used, else
// session.agent / session.model from the settings.
func (sv *Server) Defaults() (agent, model string) {
	sv.mu.Lock()
	agent, model = sv.lastAgent, sv.lastModel
	sv.mu.Unlock()
	if agent == "" {
		agent, model = config.Get("session.agent", ""), config.Get("session.model", "")
	}
	return agent, model
}

// StartSession starts a session with agent ("" for the last used, else the default CLI) in dir
// and sends msg first when it is not empty.
func (sv *Server) StartSession(agent, dir, msg, by string) (Session, error) {
	return sv.StartSessionWith(agent, "", dir, msg, by)
}

// StartSessionWith is StartSession with a model for a CLI agent ("" with agent "": the default's).
func (sv *Server) StartSessionWith(agent, model, dir, msg, by string) (Session, error) {
	return sv.StartSessionEffort(agent, model, "", dir, msg, by)
}

// StartSessionEffort is StartSessionWith with an effort for a CLI that takes one ("": agent.effort).
func (sv *Server) StartSessionEffort(agent, model, effort, dir, msg, by string) (Session, error) {
	return sv.newSession("", agent, model, effort, dir, msg, by)
}

func (sv *Server) newSession(parent, agent, model, effort, dir, msg, by string) (Session, error) {
	if len(msg) > MaxMessage {
		return Session{}, fmt.Errorf("%w: message over %d bytes", errBadRequest, MaxMessage)
	}
	if dir == "" {
		dir = Projects().Default
	}
	d, err := cleanDir(dir)
	if err != nil {
		return Session{}, fmt.Errorf("%w: %v", errBadRequest, err)
	}
	if agent == "" {
		da, dm := sv.Defaults()
		agent = da
		if model == "" {
			model = dm
		}
	}
	if err := ValidAgent(agent); err != nil {
		return Session{}, fmt.Errorf("%w: %v", errBadRequest, err)
	}
	if strings.HasPrefix(agent, "api:") {
		model = ""
	}
	if err := ValidModel(model); err != nil {
		return Session{}, fmt.Errorf("%w: %v", errBadRequest, err)
	}
	if err := ValidEffort(agent, effort); err != nil {
		return Session{}, fmt.Errorf("%w: %v", errBadRequest, err)
	}
	id := newSessionID()
	sdir := filepath.Join(SessionsDir(), id)
	if err := os.MkdirAll(sdir, 0o700); err != nil {
		return Session{}, err
	}
	rf := sv.Runners
	if rf == nil {
		rf = DefaultRunner
	}
	sv.mu.Lock()
	if n := sv.liveCountLocked(); n >= MaxSessions {
		sv.mu.Unlock()
		os.RemoveAll(sdir)
		return Session{}, fmt.Errorf("%w: %d sessions are already working; stop one first", errBusy, n)
	}
	sv.starting++
	sv.mu.Unlock()
	r, err := rf(agent, model, d, sdir)
	if err != nil {
		sv.mu.Lock()
		sv.starting--
		sv.mu.Unlock()
		os.RemoveAll(sdir)
		return Session{}, fmt.Errorf("%w: %v", errBadRequest, text.Clean(err.Error()))
	}
	if es, ok := r.(effortSetter); ok && effort != "" {
		es.SetEffort(effort)
	}
	sv.setBus(r, id)
	now := time.Now().UTC()
	s := Session{ID: id, Agent: agent, Model: model, Effort: effort, Dir: d, Title: titleOf(msg), Status: StatusDone, Created: now, Updated: now, Parent: parent}
	ctx, cancel := context.WithCancel(sv.ctx())
	l := &live{s: s, runner: r, inbox: make(chan string, MaxQueued), cancel: cancel, done: make(chan struct{})}
	sv.mu.Lock()
	sv.starting--
	if _, err := sv.appendLocked(KindSessionStarted, id, s); err != nil {
		sv.mu.Unlock()
		cancel()
		r.Close()
		return Session{}, err
	}
	sv.sessions[id] = l
	sv.lastAgent, sv.lastModel = agent, model
	var qerr error
	if strings.TrimSpace(msg) != "" {
		qerr = sv.queueLocked(l, msg, by)
		s = l.s
	}
	sv.mu.Unlock()
	go sv.loop(ctx, l, l.inbox, r, l.done)
	if qerr != nil {
		return s, qerr
	}
	return s, nil
}

func (sv *Server) liveCountLocked() int {
	n := sv.starting
	for _, l := range sv.sessions {
		if l.runner != nil && !l.stopped && (l.s.Status == StatusWorking || l.s.Status == StatusNeedsYou) {
			n++
		}
	}
	return n
}

// SendSession queues msg as the next user message of session id. A session whose runner is gone
// (stopped, or left by an earlier daemon) gets a new one and goes on with its history.
func (sv *Server) SendSession(id, msg, by string) error {
	if strings.TrimSpace(msg) == "" || len(msg) > MaxMessage {
		return fmt.Errorf("%w: text must be 1 to %d bytes", errBadRequest, MaxMessage)
	}
	sv.mu.Lock()
	l := sv.sessions[id]
	if l == nil {
		sv.mu.Unlock()
		return errNoSession
	}
	if l.s.Status != StatusWorking && l.s.Status != StatusNeedsYou {
		if n := sv.liveCountLocked(); n >= MaxSessions {
			sv.mu.Unlock()
			return fmt.Errorf("%w: %d sessions are already working; stop one first", errBusy, n)
		}
	}
	if l.runner == nil || l.stopped {
		rf := sv.Runners
		if rf == nil {
			rf = DefaultRunner
		}
		agent, model, effort, dir := l.s.Agent, l.s.Model, l.s.Effort, l.s.Dir
		sv.mu.Unlock()
		r, err := rf(agent, model, dir, filepath.Join(SessionsDir(), id))
		if err != nil {
			return fmt.Errorf("%w: %v", errBadRequest, text.Clean(err.Error()))
		}
		if es, ok := r.(effortSetter); ok && effort != "" {
			es.SetEffort(effort)
		}
		sv.setBus(r, id)
		sv.mu.Lock()
		if l.runner == nil || l.stopped {
			ctx, cancel := context.WithCancel(sv.ctx())
			l.runner, l.inbox, l.cancel, l.stopped, l.done = r, make(chan string, MaxQueued), cancel, false, make(chan struct{})
			go sv.loop(ctx, l, l.inbox, r, l.done)
		} else {
			r.Close()
		}
	}
	err := sv.queueLocked(l, msg, by)
	sv.mu.Unlock()
	return err
}

func (sv *Server) queueLocked(l *live, msg, by string) error {
	sv.goalResumeLocked(l, by)
	select {
	case l.inbox <- msg:
	default:
		return fmt.Errorf("%w: %d messages already wait for this session", errBusy, MaxQueued)
	}
	_, err := sv.appendLocked(KindSessionMessage, l.s.ID, map[string]string{"role": "user", "text": msg, "by": by})
	if err == nil {
		l.history = append(l.history, Exchange{"user", msg})
	}
	sv.setStatusLocked(l.s.ID, StatusWorking, "")
	return err
}

// ValidEffort reports whether effort may be set on a session of agent: "" (the agent.effort
// setting) or a level agents.ClampEffort keeps as it is, and only for a CLI that takes an effort flag.
func ValidEffort(agent, effort string) error {
	if effort == "" {
		return nil
	}
	if e, capped := agents.ClampEffort(effort); capped || e != effort {
		return fmt.Errorf("effort must be one of %s (smithy caps effort at %s)", strings.Join(agents.Efforts, ", "), agents.MaxEffort)
	}
	if !agents.EffortFlags[agent] {
		return fmt.Errorf("%s takes no effort setting; it keeps its own", agent)
	}
	return nil
}

// SetSessionModel sets the model and effort a session's next turns run with. A CLI agent's
// runner is ended so the next message starts one with them; a session mid-turn is refused.
func (sv *Server) SetSessionModel(id, model, effort string) (Session, error) {
	if err := ValidModel(model); err != nil {
		return Session{}, fmt.Errorf("%w: %v", errBadRequest, err)
	}
	sv.mu.Lock()
	defer sv.mu.Unlock()
	l := sv.sessions[id]
	if l == nil {
		return Session{}, errNoSession
	}
	if strings.HasPrefix(l.s.Agent, "api:") {
		return Session{}, fmt.Errorf("%w: an API session's model is part of its agent; start a new session", errBadRequest)
	}
	if err := ValidEffort(l.s.Agent, effort); err != nil {
		return Session{}, fmt.Errorf("%w: %v", errBadRequest, err)
	}
	if l.s.Status == StatusWorking || l.s.Status == StatusNeedsYou {
		return Session{}, fmt.Errorf("%w: the session is mid-turn; wait for it or /stop it first", errBusy)
	}
	if _, err := sv.appendLocked(KindSessionModel, id, map[string]string{"model": model, "effort": effort}); err != nil {
		return Session{}, err
	}
	l.s.Model, l.s.Effort = model, effort
	if l.runner != nil && !l.stopped {
		l.stopped = true
		l.cancel()
	}
	return l.s, nil
}

func (sv *Server) setSessionModel(w http.ResponseWriter, r *http.Request) {
	if code, msg := sameOrigin(r); code != 0 {
		writeJSON(w, code, map[string]string{"error": msg})
		return
	}
	var req struct {
		Model  string `json:"model"`
		Effort string `json:"effort"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "body must be {\"model\":…,\"effort\":…}"})
		return
	}
	s, err := sv.SetSessionModel(r.PathValue("id"), req.Model, req.Effort)
	if err != nil {
		sessionErr(w, err)
		return
	}
	writeJSON(w, 200, s)
}

// StopSession cancels the running turn, drops queued messages and ends the runner. The session
// stays listed (done) and a later message starts a new runner with the same history.
func (sv *Server) StopSession(id string) error {
	sv.mu.Lock()
	l := sv.sessions[id]
	if l == nil {
		sv.mu.Unlock()
		return errNoSession
	}
	running := l.runner != nil && !l.stopped
	var done chan struct{}
	if running {
		l.stopped = true
		l.cancel()
		done = l.done
	}
	sv.setStatusLocked(id, StatusDone, "stopped")
	sv.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-time.After(stopWait):
		}
	}
	return nil
}

// stopWait bounds how long StopSession waits for the runner's last writes before it returns.
const stopWait = 10 * time.Second

// Sessions lists every session, the most recently updated first.
func (sv *Server) Sessions() []Session {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	out := make([]Session, 0, len(sv.sessions))
	for _, l := range sv.sessions {
		out = append(out, l.s)
	}
	slices.SortFunc(out, func(a, b Session) int { return b.Updated.Compare(a.Updated) })
	return out
}

func (sv *Server) ctx() context.Context {
	if sv.Ctx != nil {
		return sv.Ctx
	}
	return context.Background()
}

func (sv *Server) loop(ctx context.Context, l *live, inbox <-chan string, r Runner, done chan struct{}) {
	defer close(done)
	defer r.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-inbox:
			sv.runTurn(ctx, l, r, msg)
		}
	}
}

func (sv *Server) runTurn(ctx context.Context, l *live, r Runner, msg string) {
	id := l.s.ID
	sv.mu.Lock()
	hist := slices.Clone(l.history)
	if n := len(hist); n > 0 && hist[n-1].Role == "user" && hist[n-1].Text == msg {
		hist = hist[:n-1]
	}
	if l.s.Goal != "" && l.s.GoalState == "" {
		msg = goalPrompt(l.s.Goal, msg)
	}
	sv.mu.Unlock()
	sv.setStatus(id, StatusWorking, "")
	e := &emitter{sv: sv, id: id}
	reply, err := r.Turn(ctx, msg, hist, e)
	if reply = strings.TrimSpace(reply); reply != "" {
		sv.mu.Lock()
		if _, aerr := sv.appendLocked(KindSessionMessage, id, map[string]string{"role": "agent", "text": reply}); aerr == nil {
			l.history = append(l.history, Exchange{"agent", reply})
		}
		sv.mu.Unlock()
	}
	sv.emitDiff(id, l.s.Dir)
	switch {
	case ctx.Err() != nil:
		sv.setStatus(id, StatusDone, "stopped")
	case err != nil:
		sv.setStatus(id, StatusFailed, line1(text.Clean(err.Error()), 300))
	default:
		sv.mu.Lock()
		if len(l.inbox) == 0 && !sv.goalNextLocked(l, reply) {
			sv.setStatusLocked(id, StatusDone, "")
		}
		sv.mu.Unlock()
	}
}

type emitter struct {
	sv *Server
	id string
}

func (e *emitter) Text(t string) {
	if t != "" {
		_, _ = e.sv.Append(KindSessionText, e.id, map[string]string{"text": proof.Redact(t)})
	}
}

func (e *emitter) Tool(t ToolEvent) {
	t.Args = json.RawMessage(proof.Redact(string(t.Args)))
	t.Output = proof.Redact(t.Output)
	if len(t.Args) > MaxToolField || !json.Valid(t.Args) {
		t.Args, _ = json.Marshal(clipBytes(string(t.Args), MaxToolField))
	}
	t.Output = clipBytes(t.Output, MaxToolField)
	sv := e.sv
	sv.mu.Lock()
	defer sv.mu.Unlock()
	if _, err := sv.appendLocked(KindSessionTool, e.id, t); err == nil {
		sv.notePRsLocked(e.id, t)
	}
}

func (e *emitter) Usage(u usage.Usage) {
	sv := e.sv
	sv.mu.Lock()
	if _, err := sv.appendLocked(KindSessionUsage, e.id, u); err != nil {
		sv.mu.Unlock()
		return
	}
	ev := events.Event{Kind: "usage", Usage: &u, Session: e.id}
	if l := sv.sessions[e.id]; l != nil {
		t := u
		if l.s.Usage != nil {
			t = l.s.Usage.Add(u)
		}
		l.s.Usage = &t
		ev.Agent, ev.Model = l.s.Agent, l.s.Model
	}
	sv.mu.Unlock()
	if ev.Agent == "" {
		ev.Agent = "default"
	}
	_ = events.Append(ev)
}

// Ask records a decision perm:<session>/<n> and waits for its answer; the session shows
// needs_you meanwhile. Any connector or client that answers decisions can answer it.
func (e *emitter) Ask(ctx context.Context, question string, options []string) (string, error) {
	sv := e.sv
	sv.mu.Lock()
	l := sv.sessions[e.id]
	if l == nil {
		sv.mu.Unlock()
		return "", errNoSession
	}
	l.asks++
	id := "perm:" + e.id + "/" + strconv.Itoa(l.asks)
	d := Decision{ID: id, Question: line1(question, MaxQuestion), Options: options, Asked: time.Now().UTC()}
	if _, err := sv.appendLocked(KindAsked, e.id, d); err != nil {
		sv.mu.Unlock()
		return "", err
	}
	sv.decisions[id] = &d
	sv.setStatusLocked(e.id, StatusNeedsYou, d.Question)
	sv.mu.Unlock()
	defer sv.setStatus(e.id, StatusWorking, "")
	ctx, cancel := context.WithTimeout(ctx, permitWait)
	defer cancel()
	for {
		sv.mu.Lock()
		wake := sv.wake
		o := d.Outcome
		if cur := sv.decisions[id]; cur != nil {
			o = cur.Outcome
		}
		sv.mu.Unlock()
		if o != nil {
			return o.Answer, nil
		}
		select {
		case <-ctx.Done():
			sv.closeAnswered(id, "deny", "smithy")
			return "", ctx.Err()
		case <-wake:
		}
	}
}

func clipBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	r := []rune(s[:n])
	return string(r[:max(len(r)-1, 0)]) + "…"
}

var (
	errNoSession = errors.New("no such session")
	errBusy      = errors.New("busy")
)

// SessionsDir holds each session's folder (CLI turn logs).
func SessionsDir() string { return filepath.Join(layout.Home(), "sessions") }

func sessionErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errNoSession):
		writeJSON(w, 404, map[string]string{"error": err.Error()})
	case errors.Is(err, errBadRequest):
		writeJSON(w, 400, map[string]string{"error": strings.TrimPrefix(err.Error(), errBadRequest.Error()+": ")})
	case errors.Is(err, errBusy):
		writeJSON(w, 429, map[string]string{"error": strings.TrimPrefix(err.Error(), errBusy.Error()+": ")})
	default:
		writeJSON(w, 500, map[string]string{"error": "store failed"})
	}
}

func (sv *Server) startSession(w http.ResponseWriter, r *http.Request) {
	if code, msg := sameOrigin(r); code != 0 {
		writeJSON(w, code, map[string]string{"error": msg})
		return
	}
	var req struct {
		Agent   string `json:"agent"`
		Model   string `json:"model"`
		Effort  string `json:"effort"`
		Dir     string `json:"dir"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxMessage+4<<10)).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "body must be {\"agent\":…,\"dir\":…,\"message\":…}"})
		return
	}
	s, err := sv.StartSessionEffort(req.Agent, req.Model, req.Effort, req.Dir, req.Message, r.Header.Get("X-Smithy-Who"))
	if err != nil && s.ID == "" {
		sessionErr(w, err)
		return
	}
	writeJSON(w, 201, s)
}

func (sv *Server) sendSession(w http.ResponseWriter, r *http.Request) {
	if code, msg := sameOrigin(r); code != 0 {
		writeJSON(w, code, map[string]string{"error": msg})
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxMessage+4<<10)).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "body must be {\"text\":…}"})
		return
	}
	if err := sv.SendSession(r.PathValue("id"), req.Text, r.Header.Get("X-Smithy-Who")); err != nil {
		sessionErr(w, err)
		return
	}
	writeJSON(w, 202, map[string]string{"id": r.PathValue("id"), "status": StatusWorking})
}

func (sv *Server) stopSession(w http.ResponseWriter, r *http.Request) {
	if code, msg := sameOrigin(r); code != 0 {
		writeJSON(w, code, map[string]string{"error": msg})
		return
	}
	if err := sv.StopSession(r.PathValue("id")); err != nil {
		sessionErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"id": r.PathValue("id"), "status": StatusDone})
}

func (sv *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	l := SessionList{Sessions: sv.Sessions()}
	if d := r.URL.Query().Get("dir"); d != "" {
		if real, err := filepath.EvalSymlinks(d); err == nil {
			d = real
		}
		var in []Session
		for _, s := range l.Sessions {
			if InProject(s.Dir, d) {
				in = append(in, s)
			}
		}
		l.Sessions = in
	}
	l.LastAgent, l.LastModel = sv.Defaults()
	writeJSON(w, 200, l)
}

func (sv *Server) sessionMail(w http.ResponseWriter, r *http.Request) {
	sv.mu.Lock()
	ok := sv.sessions[r.PathValue("id")] != nil
	sv.mu.Unlock()
	if !ok {
		sessionErr(w, errNoSession)
		return
	}
	writeJSON(w, 200, sv.Mail(r.PathValue("id")))
}

func (sv *Server) getSession(w http.ResponseWriter, r *http.Request) {
	sv.mu.Lock()
	l := sv.sessions[r.PathValue("id")]
	var s Session
	if l != nil {
		s = l.s
	}
	sv.mu.Unlock()
	if l == nil {
		sessionErr(w, errNoSession)
		return
	}
	writeJSON(w, 200, s)
}

// SessionsWorking reports whether any session has a turn running or waits on a person.
func (sv *Server) SessionsWorking() bool {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	return sv.liveCountLocked() > 0
}

// InProject reports whether a session started in dir belongs to the project folder root.
func InProject(dir, root string) bool {
	if dir == "" || root == "" {
		return false
	}
	dir, root = filepath.Clean(dir), filepath.Clean(root)
	return dir == root || strings.HasPrefix(dir, strings.TrimSuffix(root, string(filepath.Separator))+string(filepath.Separator))
}
