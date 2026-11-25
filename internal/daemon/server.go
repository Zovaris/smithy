//go:build harness

package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

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
}

// Server serves the API over one store.
type Server struct {
	st      *store.Store
	token   string
	allow   []string
	log     func(string)
	started time.Time
	// ServeState, when set, fills State.Serve (the auto loop's heartbeat).
	ServeState func() any
	// Route, when set, sends a won answer back to its asker (RouteBack); an error leaves it for Resume.
	Route func(Decision, Outcome) error

	mu        sync.Mutex
	decisions map[string]*Decision
	routed    map[string]bool
	lastSeq   int64
	wake      chan struct{}
}

// New loads the decisions recorded in st. log may be nil.
func New(st *store.Store, token string, allow []string, log func(string)) (*Server, error) {
	if token == "" {
		return nil, errors.New("daemon: empty token")
	}
	sv := &Server{st: st, token: token, allow: allow, log: log, started: time.Now(),
		decisions: map[string]*Decision{}, routed: map[string]bool{}, wake: make(chan struct{})}
	evs, err := st.Since(0)
	if err != nil {
		return nil, err
	}
	for _, e := range evs {
		sv.apply(e)
		sv.lastSeq = e.Seq
	}
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
	if len(d.Options) > 0 && !slices.Contains(d.Options, answer) {
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

// Handler is the API, every route behind Authorize.
func (sv *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/state", sv.state)
	mux.HandleFunc("GET /v1/events", sv.events)
	mux.HandleFunc("POST /v1/decisions", sv.decide)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1" && !strings.HasPrefix(r.URL.Path, "/v1/") {
			if !ShellAllowed(r, sv.allow) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			web.Handler().ServeHTTP(w, r)
			return
		}
		who, ok := Authorize(r, sv.token, sv.allow)
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="smithy"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		r.Header.Set("X-Smithy-Who", who)
		mux.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func (sv *Server) state(w http.ResponseWriter, r *http.Request) {
	sv.mu.Lock()
	last := sv.lastSeq
	sv.mu.Unlock()
	st := State{PID: os.Getpid(), Started: sv.started, LastSeq: last, Pending: sv.Pending()}
	if sv.ServeState != nil {
		st.Serve = sv.ServeState()
	}
	writeJSON(w, 200, st)
}

func (sv *Server) decide(w http.ResponseWriter, r *http.Request) {
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
