//go:build harness

package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	smithyauth "github.com/Zovaris/smithy/internal/auth"
	"github.com/Zovaris/smithy/internal/codemode"
	"github.com/Zovaris/smithy/internal/connect"
	"github.com/Zovaris/smithy/internal/store"
	"github.com/Zovaris/smithy/internal/workflow"
)

// Store event kinds of workflows: a notify step's message, and a finished run.
const (
	KindWorkflowNote = "workflow_note"
	KindWorkflowRun  = "workflow_run"
)

type workflowNote struct {
	Workflow string `json:"workflow"`
	Title    string `json:"title"`
	Text     string `json:"text"`
}

type workflowRun struct {
	Workflow string  `json:"workflow"`
	Schedule string  `json:"schedule,omitempty"`
	Status   string  `json:"status"`
	Trigger  string  `json:"trigger"`
	Error    string  `json:"error,omitempty"`
	CostUSD  float64 `json:"cost_usd,omitempty"`
}

// NewWorkflows is the daemon's workflow scheduler over st: shell steps behind the guard, agent
// steps through `smithy agent --profile auto`, ask steps as decisions, notify steps as store
// events (TUI, web and the chat connectors), GitHub events through gh. Files from before
// schedules are migrated first.
func (sv *Server) NewWorkflows(st workflow.Store) *workflow.Scheduler {
	if moved := st.Migrate(time.Now()); len(moved) > 0 && sv.log != nil {
		sv.log("workflows: moved schedules out of " + strings.Join(moved, ", "))
	}
	return &workflow.Scheduler{
		Store:  st,
		GitHub: workflow.GitHubEvents,
		Env: workflow.Env{Shell: workflow.Shell, Agent: workflow.Agent, Guard: workflow.Guard, Ask: sv.workflowAsk, Code: workflowCode,
			Secret: workflowSecret,
			Session: func(_ context.Context, dir, agent, prompt string) (string, error) {
				s, err := sv.StartSession(agent, dir, prompt, "workflow")
				return s.ID, err
			},
			Notify: func(wf, title, text string) error {
				_, err := sv.Append(KindWorkflowNote, "", workflowNote{Workflow: wf, Title: title, Text: text})
				return err
			}},
		OnRun: func(r workflow.Record) {
			_, _ = sv.Append(KindWorkflowRun, "", workflowRun{Workflow: r.Workflow, Schedule: r.Schedule, Status: r.Status, Trigger: r.Trigger, Error: r.Error, CostUSD: r.CostUSD})
		},
	}
}

func workflowNoteOf(e store.Event) (connect.Note, bool) {
	var n workflowNote
	if json.Unmarshal(e.Data, &n) != nil || n.Workflow == "" {
		return connect.Note{}, false
	}
	return connect.Note{Key: "workflow|" + e.Time.UTC().Format(time.RFC3339Nano) + "|" + n.Workflow, Kind: "workflow",
		Title: line1(n.Title, 120), Text: lines(n.Text, 3500)}, true
}

func (sv *Server) workflowsOn(w http.ResponseWriter) bool {
	if sv.Workflows == nil {
		writeJSON(w, 503, map[string]string{"error": "workflows are off in this daemon"})
		return false
	}
	return true
}

func (sv *Server) listWorkflows(w http.ResponseWriter, r *http.Request) {
	if !sv.workflowsOn(w) {
		return
	}
	l, _ := sv.Workflows.Store.List()
	out := WorkflowList{Workflows: l, Running: []string{}}
	if out.Workflows == nil {
		out.Workflows = []workflow.Info{}
	}
	for n := range sv.Workflows.Running() {
		out.Running = append(out.Running, n)
	}
	sort.Strings(out.Running)
	writeJSON(w, 200, out)
}

func (sv *Server) workflowInfo(name string) (workflow.Info, bool) {
	l, _ := sv.Workflows.Store.List()
	for _, in := range l {
		if in.Name == name {
			return in, true
		}
	}
	return workflow.Info{}, false
}

func (sv *Server) getWorkflow(w http.ResponseWriter, r *http.Request) {
	if !sv.workflowsOn(w) {
		return
	}
	name := r.PathValue("name")
	in, ok := sv.workflowInfo(name)
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "no workflow named " + name})
		return
	}
	_, src, _ := sv.Workflows.Store.Get(name)
	h, _ := sv.Workflows.Store.History(name, 20)
	if h == nil {
		h = []workflow.Record{}
	}
	d := WorkflowDetail{Info: in, Source: string(src), History: h, Running: sv.Workflows.Running()[name]}
	if l, ok := sv.Workflows.Live(name); ok {
		d.Live = &l
	}
	writeJSON(w, 200, d)
}

func (sv *Server) workflowWrite(w http.ResponseWriter, r *http.Request) (string, bool) {
	if !writeGuard(w, r) || !sv.workflowsOn(w) {
		return "", false
	}
	name := r.PathValue("name")
	if _, ok := sv.workflowInfo(name); !ok {
		writeJSON(w, 404, map[string]string{"error": "no workflow named " + name})
		return "", false
	}
	return name, true
}

func (sv *Server) lifetime() context.Context {
	if sv.Ctx != nil {
		return sv.Ctx
	}
	return context.Background()
}

// runWorkflow is POST /v1/workflows/{name}/run with {"inputs": {...}}: start one run now.
func (sv *Server) runWorkflow(w http.ResponseWriter, r *http.Request) {
	name, ok := sv.workflowWrite(w, r)
	if !ok {
		return
	}
	if sv.Workflows.Running()[name] {
		writeJSON(w, 409, map[string]string{"error": "workflow " + name + " is already running"})
		return
	}
	trigger := "web"
	if v := r.URL.Query().Get("via"); v == "tui" || v == "cli" {
		trigger = v
	}
	var req struct {
		Inputs map[string]string `json:"inputs"`
	}
	if b, err := readAllCapped(w, r, 16<<10); err != nil || len(b) > 0 && json.Unmarshal(b, &req) != nil {
		writeJSON(w, 400, map[string]string{"error": `body must be {"inputs": {name: value}}`})
		return
	}
	go func() {
		_, _ = sv.Workflows.Start(sv.lifetime(), name, workflow.Invocation{Trigger: trigger, Inputs: req.Inputs})
	}()
	writeJSON(w, 202, map[string]string{"status": "started"})
}

// saveWorkflow is PUT /v1/workflows/{name}: the edited file, validated; the name can't change.
// A change turns off every schedule that pins the workflow.
func (sv *Server) saveWorkflow(w http.ResponseWriter, r *http.Request) {
	name, ok := sv.workflowWrite(w, r)
	if !ok {
		return
	}
	b, err := readAllCapped(w, r, 64<<10)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "body must be a workflow file up to 64 KiB"})
		return
	}
	wf, err := workflow.Parse(b)
	if err == nil && wf.Name != name {
		err = errRename
	}
	if err == nil {
		err = sv.Workflows.Store.Save(wf)
	}
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	in, _ := sv.workflowInfo(name)
	writeJSON(w, 200, in)
}

var errRename = errors.New("the name in the file must stay the workflow's name")

func readAllCapped(w http.ResponseWriter, r *http.Request, n int64) ([]byte, error) {
	return io.ReadAll(http.MaxBytesReader(w, r.Body, n))
}

// validateWorkflow is POST /v1/workflows/validate: the editor's check, nothing saved.
func (sv *Server) validateWorkflow(w http.ResponseWriter, r *http.Request) {
	b, err := readAllCapped(w, r, 64<<10)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "body must be a workflow file up to 64 KiB"})
		return
	}
	if _, err := workflow.Parse(b); err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (sv *Server) schedInfo(name string) (workflow.SchedInfo, bool) {
	l, _ := sv.Workflows.Store.Schedules()
	for _, in := range l {
		if in.Name == name {
			return in, true
		}
	}
	return workflow.SchedInfo{}, false
}

func (sv *Server) listSchedules(w http.ResponseWriter, r *http.Request) {
	if !sv.workflowsOn(w) {
		return
	}
	l, _ := sv.Workflows.Store.Schedules()
	if l == nil {
		l = []workflow.SchedInfo{}
	}
	writeJSON(w, 200, ScheduleList{Schedules: l})
}

func (sv *Server) getSchedule(w http.ResponseWriter, r *http.Request) {
	if !sv.workflowsOn(w) {
		return
	}
	name := r.PathValue("name")
	in, ok := sv.schedInfo(name)
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "no schedule named " + name})
		return
	}
	_, src, _ := sv.Workflows.Store.GetSchedule(name)
	h, _ := sv.Workflows.Store.ScheduleHistory(name, 20)
	if h == nil {
		h = []workflow.Record{}
	}
	d := ScheduleDetail{Info: in, Source: string(src), History: h}
	if in.Webhook && in.Enabled && r.Header.Get("X-Smithy-Who") == "local" {
		d.Hook = sv.Workflows.Store.HookToken(name)
	}
	writeJSON(w, 200, d)
}

// addSchedule is POST /v1/schedules: a new schedule file, saved disabled.
func (sv *Server) addSchedule(w http.ResponseWriter, r *http.Request) {
	if !writeGuard(w, r) || !sv.workflowsOn(w) {
		return
	}
	b, err := readAllCapped(w, r, 16<<10)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "body must be a schedule up to 16 KiB"})
		return
	}
	sc, err := workflow.ParseSchedule(b)
	if err == nil {
		if _, exists := sv.schedInfo(sc.Name); exists {
			err = fmt.Errorf("schedule %s already exists", sc.Name)
		} else if _, _, werr := sv.Workflows.Store.Get(sc.Workflow); werr != nil {
			err = werr
		}
	}
	if err == nil {
		err = sv.Workflows.Store.SaveSchedule(sc)
	}
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	in, _ := sv.schedInfo(sc.Name)
	writeJSON(w, 200, in)
}

func (sv *Server) scheduleWrite(w http.ResponseWriter, r *http.Request) (string, bool) {
	if !writeGuard(w, r) || !sv.workflowsOn(w) {
		return "", false
	}
	name := r.PathValue("name")
	if _, ok := sv.schedInfo(name); !ok {
		writeJSON(w, 404, map[string]string{"error": "no schedule named " + name})
		return "", false
	}
	return name, true
}

// setSchedule is POST /v1/schedules/{name}/enable or /disable.
func (sv *Server) setSchedule(w http.ResponseWriter, r *http.Request) {
	op := r.PathValue("op")
	if op != "enable" && op != "disable" {
		http.NotFound(w, r)
		return
	}
	name, ok := sv.scheduleWrite(w, r)
	if !ok {
		return
	}
	var err error
	if op == "enable" {
		err = sv.Workflows.Store.EnableSchedule(name, time.Now())
	} else {
		err = sv.Workflows.Store.DisableSchedule(name)
	}
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	in, _ := sv.schedInfo(name)
	writeJSON(w, 200, in)
}

func (sv *Server) removeSchedule(w http.ResponseWriter, r *http.Request) {
	name, ok := sv.scheduleWrite(w, r)
	if !ok {
		return
	}
	if err := sv.Workflows.Store.RemoveSchedule(name); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"removed": name})
}

// WorkflowsScheduled reports whether a workflow runs or a schedule is enabled: the idle exit must
// wait, or the schedule would never fire.
func (sv *Server) WorkflowsScheduled() bool {
	if sv.Workflows == nil {
		return false
	}
	return len(sv.Workflows.Running()) > 0 || len(sv.Workflows.Store.Enabled()) > 0
}

// HookWait is how long a webhook call waits for its run before answering 202.
var HookWait = 2 * time.Minute

// scheduleHook is POST /v1/hooks/schedules/{name}, outside the daemon token, from this machine
// only: the schedule's own token (X-Smithy-Hook or a Bearer header) opens it, only when it is a
// webhook schedule enabled with nothing it pins changed. The JSON body's fields the workflow
// declares become inputs; the answer holds the run's status and outputs, or 202 past HookWait.
func (sv *Server) scheduleHook(w http.ResponseWriter, r *http.Request, name string) {
	notFound := func() { writeJSON(w, 404, map[string]string{"error": "not found"}) }
	if sv.Workflows == nil {
		notFound()
		return
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err != nil || !net.ParseIP(host).IsLoopback() {
		notFound()
		return
	}
	tok := r.Header.Get("X-Smithy-Hook")
	if tok == "" {
		tok = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	sc, ok := sv.Workflows.Store.CheckHook(name, tok)
	if !ok {
		notFound()
		return
	}
	b, err := readAllCapped(w, r, 64<<10)
	if err != nil {
		writeJSON(w, 413, map[string]string{"error": "body up to 64 KiB"})
		return
	}
	body := map[string]string{}
	if len(bytes.TrimSpace(b)) > 0 {
		var obj map[string]any
		if json.Unmarshal(b, &obj) != nil {
			writeJSON(w, 400, map[string]string{"error": "body must be a JSON object"})
			return
		}
		for k, v := range obj {
			if s, isStr := v.(string); isStr {
				body[k] = s
			} else {
				vb, _ := json.Marshal(v)
				body[k] = string(vb)
			}
		}
	}
	done := make(chan workflow.Record, 1)
	errc := make(chan error, 1)
	go func() {
		rec, err := sv.Workflows.Webhook(sv.lifetime(), sc, body)
		if err != nil {
			errc <- err
			return
		}
		done <- rec
	}()
	select {
	case rec := <-done:
		writeJSON(w, 200, map[string]any{"status": rec.Status, "outputs": rec.Outputs, "error": rec.Error})
	case err := <-errc:
		writeJSON(w, 409, map[string]string{"error": err.Error()})
	case <-time.After(HookWait):
		writeJSON(w, 202, map[string]string{"status": "running"})
	}
}

// workflowAsk is an ask step: the question goes out like any decision (TUI, web, chat
// connectors) and the step waits for its answer.
func (sv *Server) workflowAsk(ctx context.Context, id, question string, options []string) (string, error) {
	q := "Workflow " + strings.SplitN(strings.TrimPrefix(id, "workflow:"), ":", 2)[0] + ": " + question
	if err := sv.askDecision(Decision{ID: id, Question: q, Options: options, Other: len(options) == 0}); err != nil {
		return "", err
	}
	for {
		sv.mu.Lock()
		d := sv.decisions[id]
		var o *Outcome
		if d != nil {
			o = d.Outcome
		}
		sv.mu.Unlock()
		if o != nil {
			return o.Answer, nil
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("no answer before the step's timeout")
		case <-time.After(300 * time.Millisecond):
		}
	}
}

// workflowSecret is {{secret.name}}: a key stored with smithy auth set ("" when none).
func workflowSecret(name string) string {
	k, _, err := smithyauth.Get(name)
	if err != nil {
		return ""
	}
	return k
}

// workflowCode runs a workflow code step in code mode's sandbox (its default limits, no tools).
func workflowCode(ctx context.Context, code, input string) (string, error) {
	return codemode.Eval(ctx, code, input, codemode.Limits{})
}
