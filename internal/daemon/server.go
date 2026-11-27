//go:build harness

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Zovaris/smithy/internal/agents"
	"github.com/Zovaris/smithy/internal/connect"
	"github.com/Zovaris/smithy/internal/models"
	"github.com/Zovaris/smithy/internal/store"
	"github.com/Zovaris/smithy/internal/web"
)

const (
	KindAsked    = "decision_asked"
	KindAnswered = "decision_answered"
	// KindRouted records that an answer reached its asker (or came from it), so it is never sent twice.
	KindRouted = "decision_routed"
)

// Decision is a question waiting on (or answered by) someone.
type Decision struct {
	ID       string    `json:"id"`
	Question string    `json:"question"`
	Options  []string  `json:"options,omitempty"`
	Asked    time.Time `json:"asked"`
	Outcome  *Outcome  `json:"outcome,omitempty"`
}

// Outcome is the recorded answer. Won is true only in the response to the answer that won.
type Outcome struct {
	ID     string    `json:"id"`
	Answer string    `json:"answer"`
	By     string    `json:"by"`
	At     time.Time `json:"at"`
	Won    bool      `json:"won"`
}

// State is GET /v1/state.
type State struct {
	PID     int        `json:"pid"`
	Started time.Time  `json:"started"`
	LastSeq int64      `json:"last_seq"`
	Pending []Decision `json:"pending"`
	Serve   any        `json:"serve,omitempty"`
	Version string     `json:"version,omitempty"`
}

// Server serves the API over one store.
type Server struct {
	// Connectors builds the chat connectors from the current config (nil: connect.FromConfig).
	Connectors func() ([]connect.Connector, error)
	chatBuf    map[string]string // a chat session's answer so far, sent when its turn ends
	st         *store.Store
	token      string
	allow      []string
	log        func(string)
	started    time.Time
	// Tend, when set, is the PR tender GET /v1/tend reports.
	Tend *Tender
	// ServeState, when set, fills State.Serve (the auto loop's heartbeat).
	ServeState func() any
	// Route, when set, sends a won answer back to its asker (RouteBack); an error leaves it for Resume.
	Route func(Decision, Outcome) error
	// Plan, when set, replaces AgentPlan for chat intake (tests).
	Plan func(ctx context.Context, idea string) ([]PlanTask, error)
	// BenchDir is where bench runs are read from; empty: bench.Dir().
	BenchDir string
	// BenchRun, when set, replaces running `smithy <args>` for a web-started bench run (tests).
	BenchRun func(args []string) error
	// Runners makes each session's runner; nil: DefaultRunner.
	Runners RunnerFor
	// Version is the daemon binary's version, reported in State so a client can warn on a mismatch.
	Version string
	// Ready, when set, replaces agent detection (tests).
	Ready func() []agents.Ready
	// Doctor, when set, replaces running `smithy doctor` (tests).
	Doctor func(context.Context) []string
	// Models, when set, replaces model discovery (tests).
	Models func(ctx context.Context, agent string, refresh bool) models.List
	// SkillsInstall, when set, replaces running `smithy skills install` (tests).
	SkillsInstall func(ctx context.Context, harness string) (string, error)
	// Ctx is the daemon's lifetime (cancelled on shutdown) for background work like planning; nil: Background.
	Ctx context.Context
	// Pair hands out pairing codes and checks device tokens; nil: pairing is off.
	Pair *Pairing
	// Servers lists the saved servers the web switcher reaches through this daemon; nil: none.
	Servers func() ([]RemoteServer, error)
	// ServersFile is servers.json: the web adds and removes servers there, and lists it when Servers is nil.
	ServersFile string

	mu         sync.Mutex
	decisions  map[string]*Decision
	routed     map[string]bool
	intakes    map[string]*intakePlan
	planning   atomic.Bool
	installing atomic.Bool
	benchMu    sync.Mutex
	benchNow   BenchStatus
	lastSeq    int64
	wake       chan struct{}
	sessions   map[string]*live
	lastAgent  string
	lastModel  string
	starting   int
	clients    atomic.Int64 // open event streams
	seen       atomic.Int64 // unix nanos of the last API request or stream end
}

// New loads the decisions recorded in st. log may be nil.
func New(st *store.Store, token string, allow []string, log func(string)) (*Server, error) {
	if token == "" {
		return nil, errors.New("daemon: empty token")
	}
	sv := &Server{st: st, token: token, allow: allow, log: log, started: time.Now(),
		decisions: map[string]*Decision{}, routed: map[string]bool{}, intakes: map[string]*intakePlan{}, wake: make(chan struct{}), sessions: map[string]*live{}}
	sv.touch()
	evs, err := st.Since(0)
	if err != nil {
		return nil, err
	}
	for _, e := range evs {
		sv.apply(e)
		sv.lastSeq = e.Seq
	}
	sv.mu.Lock()
	sv.failOrphans()
	sv.mu.Unlock()
	return sv, nil
}

func (sv *Server) apply(e store.Event) {
	switch e.Kind {
	case KindAsked:
		var d Decision
		if json.Unmarshal(e.Data, &d) == nil && d.ID != "" {
			if _, ok := sv.decisions[d.ID]; !ok {
				d.Asked = e.Time
				sv.decisions[d.ID] = &d
			}
		}
	case KindAnswered:
		var o Outcome
		if json.Unmarshal(e.Data, &o) == nil {
			if d := sv.decisions[o.ID]; d != nil && d.Outcome == nil {
				o.Won = false
				d.Outcome = &o
			}
		}
	case KindIntake:
		sv.applyIntake(e.Data)
	case KindSessionStarted, KindSessionStatus, KindSessionMessage, KindSessionUsage, KindSessionModel, KindSessionTool:
		sv.applySession(e)
	case KindRouted:
		var r struct{ ID string }
		if json.Unmarshal(e.Data, &r) == nil {
			sv.routed[r.ID] = true
		}
	}
}

// Append writes one event and wakes every stream.
func (sv *Server) Append(kind, session string, data any) (int64, error) {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	return sv.appendLocked(kind, session, data)
}

func (sv *Server) appendLocked(kind, session string, data any) (int64, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return 0, err
	}
	seq, err := sv.st.Append(store.Event{Kind: kind, Session: session, Data: b})
	if err != nil {
		return 0, err
	}
	sv.lastSeq = seq
	close(sv.wake)
	sv.wake = make(chan struct{})
	return seq, nil
}

// Ask records a pending decision; asking an existing id again is a no-op.
func (sv *Server) Ask(id, question string, options []string) error {
	if id == "" {
		return errors.New("daemon: decision id is empty")
	}
	sv.mu.Lock()
	defer sv.mu.Unlock()
	if _, ok := sv.decisions[id]; ok {
		return nil
	}
	d := Decision{ID: id, Question: question, Options: options, Asked: time.Now().UTC()}
	if _, err := sv.appendLocked(KindAsked, "", d); err != nil {
		return err
	}
	sv.decisions[id] = &d
	return nil
}

// Answer records the first answer to id. Later answers get the recorded outcome with Won false.
// A won answer goes back to its asker through Route.
// FreeAnswer reports whether decision id takes an answer in the user's own words besides its
// options: only the ask tool's questions (ask:<key>) do.
func FreeAnswer(id string) bool { return strings.HasPrefix(id, "ask:") }

func (sv *Server) Answer(id, answer, by string) (Outcome, error) {
	sv.mu.Lock()
	o, err := sv.answerLocked(id, answer, by)
	var d Decision
	if err == nil && o.Won {
		d = *sv.decisions[id]
	}
	sv.mu.Unlock()
	if err == nil && o.Won {
		sv.route(d, o)
	}
	return o, err
}

func (sv *Server) answerLocked(id, answer, by string) (Outcome, error) {
	d := sv.decisions[id]
	if d == nil {
		return Outcome{}, errNotFound
	}
	if d.Outcome != nil {
		o := *d.Outcome
		o.Won = false
		return o, nil
	}
	if len(d.Options) > 0 && !slices.Contains(d.Options, answer) && !FreeAnswer(id) {
		return Outcome{}, fmt.Errorf("%w: answer must be one of %s", errBadRequest, strings.Join(d.Options, ", "))
	}
	o := Outcome{ID: id, Answer: answer, By: by, At: time.Now().UTC()}
	if _, err := sv.appendLocked(KindAnswered, "", o); err != nil {
		return Outcome{}, err
	}
	d.Outcome = &o
	o.Won = true
	return o, nil
}

// route sends one answer back through Route and records it; a failure is retried by Resume.
func (sv *Server) route(d Decision, o Outcome) {
	if sv.Route == nil {
		return
	}
	if err := sv.Route(d, o); err != nil {
		if sv.log != nil {
			sv.log("route " + d.ID + ": " + err.Error())
		}
		return
	}
	sv.mu.Lock()
	defer sv.mu.Unlock()
	sv.markRoutedLocked(d.ID)
}

func (sv *Server) markRoutedLocked(id string) {
	if sv.routed[id] {
		return
	}
	if _, err := sv.appendLocked(KindRouted, "", map[string]string{"id": id}); err == nil {
		sv.routed[id] = true
	}
}

// Resume routes every answered decision whose answer never reached its asker (a crash or a
// failed Route between the answer and the hand-back).
func (sv *Server) Resume() error {
	sv.mu.Lock()
	var todo []Decision
	for id, d := range sv.decisions {
		if d.Outcome != nil && !sv.routed[id] && routable(id) {
			todo = append(todo, *d)
		}
	}
	sv.mu.Unlock()
	for _, d := range todo {
		sv.route(d, *d.Outcome)
	}
	return nil
}

// Pending lists unanswered decisions, oldest first.
func (sv *Server) Pending() []Decision {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	out := []Decision{}
	for _, d := range sv.decisions {
		if d.Outcome == nil {
			out = append(out, *d)
		}
	}
	slices.SortFunc(out, func(a, b Decision) int { return a.Asked.Compare(b.Asked) })
	return out
}

var (
	errNotFound   = errors.New("no such decision")
	errBadRequest = errors.New("bad request")
)

func (sv *Server) apiMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/state", sv.state)
	mux.HandleFunc("GET /v1/events", sv.events)
	mux.HandleFunc("POST /v1/decisions", sv.decide)
	mux.HandleFunc("GET /v1/decisions/{id...}", sv.decision)
	mux.HandleFunc("POST /v1/asks", sv.ask)
	mux.HandleFunc("GET /v1/guard", sv.guardView)
	mux.HandleFunc("POST /v1/guard/suggested", sv.guardAddSuggested)
	mux.HandleFunc("GET /v1/sessions", sv.listSessions)
	mux.HandleFunc("POST /v1/sessions", sv.startSession)
	mux.HandleFunc("GET /v1/sessions/{id}", sv.getSession)
	mux.HandleFunc("POST /v1/sessions/{id}/messages", sv.sendSession)
	mux.HandleFunc("POST /v1/sessions/{id}/stop", sv.stopSession)
	mux.HandleFunc("PUT /v1/sessions/{id}/model", sv.setSessionModel)
	mux.HandleFunc("GET /v1/projects", sv.listProjects)
	mux.HandleFunc("POST /v1/projects", sv.addProject)
	mux.HandleFunc("DELETE /v1/projects", sv.removeProject)
	mux.HandleFunc("GET /v1/fs", sv.browse)
	mux.HandleFunc("GET /v1/setup", sv.setup)
	mux.HandleFunc("PUT /v1/setup/default", sv.setDefault)
	mux.HandleFunc("PUT /v1/setup/kit", sv.setKit)
	mux.HandleFunc("POST /v1/setup/skills/install", sv.installSkills)
	mux.HandleFunc("GET /v1/agents/{id}/models", sv.agentModels)
	mux.HandleFunc("GET /v1/bench", sv.benchRuns)
	mux.HandleFunc("GET /v1/settings", sv.listSettings)
	mux.HandleFunc("POST /v1/connect/test", sv.connectTest)
	mux.HandleFunc("PUT /v1/settings/{key}", sv.putSetting)
	mux.HandleFunc("DELETE /v1/settings/{key}", sv.deleteSetting)
	mux.HandleFunc("PUT /v1/keys/{service}", sv.putKey)
	mux.HandleFunc("DELETE /v1/keys/{service}", sv.deleteKey)
	mux.HandleFunc("PUT /v1/serve", sv.putServe)
	mux.HandleFunc("GET /v1/doctor", sv.doctor)
	mux.HandleFunc("GET /v1/tend", sv.tendView)
	mux.HandleFunc("GET /v1/bench/compare", sv.benchCompare)
	mux.HandleFunc("POST /v1/bench/run", sv.benchStart)
	mux.HandleFunc("GET /v1/bench/status", sv.benchStatus)
	mux.HandleFunc("GET /v1/tasks", sv.listTasks)
	mux.HandleFunc("PUT /v1/tasks/status", sv.moveTask)
	return mux
}

// public answers the routes that need no auth: hello and redeeming a pairing code.
func (sv *Server) public(w http.ResponseWriter, r *http.Request) bool {
	switch {
	case r.Method == "GET" && r.URL.Path == "/v1/hello":
		writeJSON(w, 200, map[string]any{"smithy": true, "version": sv.Version})
	case r.Method == "POST" && r.URL.Path == "/v1/pair":
		sv.redeem(w, r)
	default:
		return false
	}
	return true
}

// Handler is the loopback API, every route but the public ones behind Authorize.
func (sv *Server) Handler() http.Handler {
	mux := sv.apiMux()
	mux.HandleFunc("POST /v1/pair/code", sv.pairCode)
	mux.HandleFunc("GET /v1/servers", sv.listServers)
	mux.HandleFunc("POST /v1/servers", sv.addServer)
	mux.HandleFunc("DELETE /v1/servers/{name}", sv.removeServer)
	mux.HandleFunc("POST /v1/sessions/{id}/shell", sv.sessionShell)
	mux.HandleFunc("POST /v1/images", sv.saveImage)
	mux.HandleFunc("/v1/servers/{name}/{rest...}", sv.proxyServer)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1" && !strings.HasPrefix(r.URL.Path, "/v1/") {
			if !ShellAllowed(r, sv.allow) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			web.Handler().ServeHTTP(w, r)
			return
		}
		if sv.public(w, r) {
			return
		}
		who, ok := Authorize(r, sv.token, sv.allow)
		if !ok {
			if loopback, _ := hostKind(r); loopback {
				who, ok = sv.device(r)
			}
		}
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="smithy"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		r.Header.Set("X-Smithy-Who", who)
		sv.touch()
		mux.ServeHTTP(w, r)
	})
}

// RemoteHandler is the daemon.listen API: a paired device's bearer token (never the daemon's) on
// every route but hello and pair, whatever the Host, and the Tailscale login header is ignored
// (anyone on the network could send it). Codes are minted only on the loopback listener.
func (sv *Server) RemoteHandler() http.Handler {
	mux := sv.apiMux()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1" && !strings.HasPrefix(r.URL.Path, "/v1/") {
			web.Handler().ServeHTTP(w, r)
			return
		}
		if sv.public(w, r) {
			return
		}
		who, ok := sv.device(r)
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="smithy"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		r.Header.Set("X-Smithy-Who", who)
		sv.touch()
		mux.ServeHTTP(w, r)
	})
}

// device is the paired device whose token the request carries.
func (sv *Server) device(r *http.Request) (string, bool) {
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return "", false
	}
	name, ok := sv.Pair.Who(strings.TrimSpace(got))
	if !ok {
		return "", false
	}
	return "device:" + name, true
}

// pairCode is POST /v1/pair/code: a one-time code, for the daemon token holder only.
func (sv *Server) pairCode(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Smithy-Who") != "local" {
		writeJSON(w, 403, map[string]string{"error": "only the daemon token on this machine can make pairing codes"})
		return
	}
	if code, msg := sameOrigin(r); code != 0 {
		writeJSON(w, code, map[string]string{"error": msg})
		return
	}
	if sv.Pair == nil {
		writeJSON(w, 503, map[string]string{"error": "pairing is off"})
		return
	}
	c, exp := sv.Pair.NewCode()
	_, _ = sv.Append("log", "", map[string]string{"line": "daemon: pairing code made, valid until " + exp.Local().Format("15:04:05")})
	writeJSON(w, 200, map[string]any{"code": c, "expires": exp})
}

// redeem is POST /v1/pair {code, name}: trades a code for a device token.
func (sv *Server) redeem(w http.ResponseWriter, r *http.Request) {
	if code, msg := sameOrigin(r); code != 0 {
		writeJSON(w, code, map[string]string{"error": msg})
		return
	}
	var b struct{ Code, Name string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad request"})
		return
	}
	if sv.Pair == nil {
		writeJSON(w, 403, map[string]string{"error": "pairing is off"})
		return
	}
	addr, _, _ := net.SplitHostPort(r.RemoteAddr)
	tok, err := sv.Pair.RedeemFrom(addr, b.Code, b.Name)
	if err != nil {
		writeJSON(w, 403, map[string]string{"error": err.Error()})
		return
	}
	_, _ = sv.Append("log", "", map[string]string{"line": "daemon: paired device " + strings.TrimSpace(b.Name)})
	writeJSON(w, 200, map[string]string{"token": tok, "version": sv.Version})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (sv *Server) state(w http.ResponseWriter, r *http.Request) {
	sv.mu.Lock()
	last := sv.lastSeq
	sv.mu.Unlock()
	st := State{PID: os.Getpid(), Started: sv.started, LastSeq: last, Pending: sv.Pending(), Version: sv.Version}
	if sv.ServeState != nil {
		st.Serve = sv.ServeState()
	}
	writeJSON(w, 200, st)
}

// sameOrigin refuses a request a browser could forge from another site: the body must be JSON
// (not a simple form or text post), the fetch not cross-site and any Origin the daemon's own host.
// A body-less DELETE needs no Content-Type: DELETE is never a CORS simple request, so a browser
// preflights it cross-site, and the Sec-Fetch-Site and Origin checks below still apply.
func sameOrigin(r *http.Request) (int, string) {
	bodyless := r.Method == http.MethodDelete && r.ContentLength == 0
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" && !bodyless {
		return 415, "Content-Type must be application/json"
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return 403, "cross-site request"
	}
	if o := r.Header.Get("Origin"); o != "" {
		u, err := url.Parse(o)
		if err != nil || !strings.EqualFold(u.Host, r.Host) {
			return 403, "foreign origin"
		}
	}
	return 0, ""
}

// decision is GET /v1/decisions/{id}: one decision with its outcome once answered.
func (sv *Server) decision(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sv.mu.Lock()
	d := sv.decisions[id]
	var out Decision
	if d != nil {
		out = *d
	}
	sv.mu.Unlock()
	if d == nil {
		writeJSON(w, 404, map[string]string{"error": errNotFound.Error()})
		return
	}
	writeJSON(w, 200, out)
}

// ask is POST /v1/asks: smithy mcp's ask tool records a warden decision (decide:<batch>/<item>/<key>),
// or a plain chat question outside warden (ask:<key>), here, so its answer comes only from the daemon's own record, never from a file the worker can write.
func (sv *Server) ask(w http.ResponseWriter, r *http.Request) {
	if code, msg := sameOrigin(r); code != 0 {
		writeJSON(w, code, map[string]string{"error": msg})
		return
	}
	var req struct {
		ID       string   `json:"id"`
		Question string   `json:"question"`
		Options  []string `json:"options"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil || req.Question == "" || len(req.Options) > 6 {
		writeJSON(w, 400, map[string]string{"error": "body must be {\"id\":…,\"question\":…,\"options\":[…]}"})
		return
	}
	parts := strings.Split(strings.TrimPrefix(req.ID, "decide:"), "/")
	guardID := strings.HasPrefix(req.ID, "guard:") && cardID(strings.TrimPrefix(req.ID, "guard:"))
	chatID := strings.HasPrefix(req.ID, "ask:") && cardID(strings.TrimPrefix(req.ID, "ask:"))
	if !guardID && !chatID && (!strings.HasPrefix(req.ID, "decide:") || len(parts) != 3 || !cardID(parts[0]) || !cardID(parts[1]) || !cardID(parts[2])) {
		writeJSON(w, 400, map[string]string{"error": "id must be decide:<batch>/<item>/<key>, guard:<key> or ask:<key>"})
		return
	}
	opts := make([]string, 0, len(req.Options))
	for _, o := range req.Options {
		opts = append(opts, line1(o, 200))
	}
	if err := sv.Ask(req.ID, line1(req.Question, 500), opts); err != nil {
		writeJSON(w, 500, map[string]string{"error": "store failed"})
		return
	}
	writeJSON(w, 200, map[string]string{"id": req.ID})
}

func (sv *Server) decide(w http.ResponseWriter, r *http.Request) {
	if code, msg := sameOrigin(r); code != 0 {
		writeJSON(w, code, map[string]string{"error": msg})
		return
	}
	var req struct {
		ID     string `json:"id"`
		Answer string `json:"answer"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil || req.ID == "" || req.Answer == "" {
		writeJSON(w, 400, map[string]string{"error": "body must be {\"id\":…,\"answer\":…}"})
		return
	}
	o, err := sv.Answer(req.ID, req.Answer, r.Header.Get("X-Smithy-Who"))
	switch {
	case errors.Is(err, errNotFound):
		writeJSON(w, 404, map[string]string{"error": err.Error()})
	case errors.Is(err, errBadRequest):
		writeJSON(w, 400, map[string]string{"error": err.Error()})
	case err != nil:
		writeJSON(w, 500, map[string]string{"error": "store failed"})
	case o.Won:
		writeJSON(w, 200, o)
	default:
		writeJSON(w, 409, o)
	}
}

func (sv *Server) events(w http.ResponseWriter, r *http.Request) {
	from := r.Header.Get("Last-Event-ID")
	if from == "" {
		from = r.URL.Query().Get("since")
	}
	var last int64
	if from != "" {
		n, err := strconv.ParseInt(from, 10, 64)
		if err != nil || n < 0 {
			http.Error(w, "since must be a seq >= 0", 400)
			return
		}
		last = n
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}
	sv.clients.Add(1)
	defer func() { sv.touch(); sv.clients.Add(-1) }()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(200)
	fl.Flush()
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		sv.mu.Lock()
		wake := sv.wake
		sv.mu.Unlock()
		evs, err := sv.st.Since(last)
		if err != nil {
			return
		}
		for _, e := range evs {
			b, _ := json.Marshal(e)
			if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", e.Seq, sseSafe(e.Kind), b); err != nil {
				return
			}
			last = e.Seq
		}
		fl.Flush()
		select {
		case <-r.Context().Done():
			return
		case <-wake:
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

func sseSafe(s string) string { return strings.NewReplacer("\n", " ", "\r", " ").Replace(s) }
