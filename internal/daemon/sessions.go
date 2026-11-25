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

	"github.com/Zovaris/smithy/internal/apiloop"
	"github.com/Zovaris/smithy/internal/ghx"
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
	MaxDiff         = 64 << 10
	permitWait      = 24 * time.Hour
	sessionIDPrefix = "s-"
)

// Session is one conversation with an agent in a folder.
type Session struct {
	ID      string       `json:"id"`
	Agent   string       `json:"agent"`
	Dir     string       `json:"dir"`
	Title   string       `json:"title"`
	Status  string       `json:"status"`
	Detail  string       `json:"detail,omitempty"`
	Created time.Time    `json:"created"`
	Updated time.Time    `json:"updated"`
	Usage   *usage.Usage `json:"usage,omitempty"`
}

// ToolEvent is one finished tool call of a session.
type ToolEvent struct {
	ID      string          `json:"id,omitempty"`
	Name    string          `json:"name"`
	Args    json.RawMessage `json:"args,omitempty"`
	Output  string          `json:"output,omitempty"`
	IsError bool            `json:"is_error,omitempty"`
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
type RunnerFor func(agent, dir, sessionDir string) (Runner, error)

type live struct {
	s        Session
	runner   Runner
	inbox    chan string
	cancel   context.CancelFunc
	turn     context.CancelFunc
	history  []Exchange
	asks     int
	stopped  bool
	lastDiff string
}

var (
	cliAgent   = regexp.MustCompile(`^(acp:)?[a-z][a-z0-9-]{0,31}$`)
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
	case cliAgent.MatchString(agent):
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
			sv.lastAgent = s.Agent
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
			sv.answerLocked(id, "deny", "smithy")
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

// StartSession starts a session with agent ("" for the last used, else the default CLI) in dir
// and sends msg first when it is not empty.
func (sv *Server) StartSession(agent, dir, msg, by string) (Session, error) {
	if len(msg) > MaxMessage {
		return Session{}, fmt.Errorf("%w: message over %d bytes", errBadRequest, MaxMessage)
	}
	d, err := cleanDir(dir)
	if err != nil {
		return Session{}, fmt.Errorf("%w: %v", errBadRequest, err)
	}
	sv.mu.Lock()
	if agent == "" {
		agent = sv.lastAgent
	}
	sv.mu.Unlock()
	if err := ValidAgent(agent); err != nil {
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
		return Session{}, fmt.Errorf("%w: %d sessions are already running; stop one first", errBusy, n)
	}
	sv.mu.Unlock()
	r, err := rf(agent, d, sdir)
	if err != nil {
		os.RemoveAll(sdir)
		return Session{}, fmt.Errorf("%w: %v", errBadRequest, text.Clean(err.Error()))
	}
	now := time.Now().UTC()
	s := Session{ID: id, Agent: agent, Dir: d, Title: titleOf(msg), Status: StatusDone, Created: now, Updated: now}
	ctx, cancel := context.WithCancel(sv.ctx())
	l := &live{s: s, runner: r, inbox: make(chan string, MaxQueued), cancel: cancel}
	sv.mu.Lock()
	if _, err := sv.appendLocked(KindSessionStarted, id, s); err != nil {
		sv.mu.Unlock()
		cancel()
		r.Close()
		return Session{}, err
	}
	sv.sessions[id] = l
	sv.lastAgent = agent
	sv.mu.Unlock()
	go sv.loop(ctx, l, l.inbox, r)
	if strings.TrimSpace(msg) != "" {
		if err := sv.SendSession(id, msg, by); err != nil {
			return s, err
		}
		s.Status = StatusWorking
	}
	return s, nil
}

func (sv *Server) liveCountLocked() int {
	n := 0
	for _, l := range sv.sessions {
		if l.runner != nil && !l.stopped {
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
	if l.runner == nil || l.stopped {
		if n := sv.liveCountLocked(); n >= MaxSessions {
			sv.mu.Unlock()
			return fmt.Errorf("%w: %d sessions are already running; stop one first", errBusy, n)
		}
		rf := sv.Runners
		if rf == nil {
			rf = DefaultRunner
		}
		agent, dir := l.s.Agent, l.s.Dir
		sv.mu.Unlock()
		r, err := rf(agent, dir, filepath.Join(SessionsDir(), id))
		if err != nil {
			return fmt.Errorf("%w: %v", errBadRequest, text.Clean(err.Error()))
		}
		sv.mu.Lock()
		if l.runner == nil || l.stopped {
			ctx, cancel := context.WithCancel(sv.ctx())
			l.runner, l.inbox, l.cancel, l.stopped = r, make(chan string, MaxQueued), cancel, false
			go sv.loop(ctx, l, l.inbox, r)
		} else {
			r.Close()
		}
	}
	select {
	case l.inbox <- msg:
	default:
		sv.mu.Unlock()
		return fmt.Errorf("%w: %d messages already wait for this session", errBusy, MaxQueued)
	}
	_, err := sv.appendLocked(KindSessionMessage, id, map[string]string{"role": "user", "text": msg, "by": by})
	if err == nil {
		l.history = append(l.history, Exchange{"user", msg})
	}
	sv.setStatusLocked(id, StatusWorking, "")
	sv.mu.Unlock()
	return err
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
	if running {
		l.stopped = true
		l.cancel()
	}
	sv.setStatusLocked(id, StatusDone, "stopped")
	sv.mu.Unlock()
	return nil
}

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

func (sv *Server) loop(ctx context.Context, l *live, inbox <-chan string, r Runner) {
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
		if len(l.inbox) == 0 {
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
		e.sv.Append(KindSessionText, e.id, map[string]string{"text": t})
	}
}

func (e *emitter) Tool(t ToolEvent) {
	if len(t.Args) > MaxToolField || !json.Valid(t.Args) {
		t.Args, _ = json.Marshal(clipBytes(string(t.Args), MaxToolField))
	}
	t.Output = clipBytes(t.Output, MaxToolField)
	e.sv.Append(KindSessionTool, e.id, t)
}

func (e *emitter) Usage(u usage.Usage) {
	sv := e.sv
	sv.mu.Lock()
	defer sv.mu.Unlock()
	if _, err := sv.appendLocked(KindSessionUsage, e.id, u); err != nil {
		return
	}
	if l := sv.sessions[e.id]; l != nil {
		t := u
		if l.s.Usage != nil {
			t = l.s.Usage.Add(u)
		}
		l.s.Usage = &t
	}
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
	d := Decision{ID: id, Question: line1(question, 500), Options: options, Asked: time.Now().UTC()}
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
func SessionsDir() string { return filepath.Join(ghx.Home(), "sessions") }

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
		Dir     string `json:"dir"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxMessage+4<<10)).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "body must be {\"agent\":…,\"dir\":…,\"message\":…}"})
		return
	}
	s, err := sv.StartSession(req.Agent, req.Dir, req.Message, r.Header.Get("X-Smithy-Who"))
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

// SessionList is GET /v1/sessions.
type SessionList struct {
	Sessions  []Session `json:"sessions"`
	LastAgent string    `json:"last_agent"`
}

func (sv *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	l := SessionList{Sessions: sv.Sessions()}
	sv.mu.Lock()
	l.LastAgent = sv.lastAgent
	sv.mu.Unlock()
	writeJSON(w, 200, l)
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
