//go:build harness

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/Zovaris/smithy/internal/connect"
	"github.com/Zovaris/smithy/internal/store"
	"github.com/Zovaris/smithy/internal/workflow"
)

// Store event kinds of scheduled workflows: a notify step's message, and a finished run.
const (
	KindWorkflowNote = "workflow_note"
	KindWorkflowRun  = "workflow_run"
)

// WorkflowList is GET /v1/workflows.
type WorkflowList struct {
	Workflows []workflow.Info `json:"workflows"`
	Running   []string        `json:"running"`
}

// WorkflowDetail is GET /v1/workflows/{name}: the parsed file, its source and newest runs.
type WorkflowDetail struct {
	Info    workflow.Info     `json:"info"`
	Source  string            `json:"source"`
	History []workflow.Record `json:"history"`
	Running bool              `json:"running"`
}

type workflowNote struct {
	Workflow string `json:"workflow"`
	Title    string `json:"title"`
	Text     string `json:"text"`
}

type workflowRun struct {
	Workflow string  `json:"workflow"`
	Status   string  `json:"status"`
	Trigger  string  `json:"trigger"`
	Error    string  `json:"error,omitempty"`
	CostUSD  float64 `json:"cost_usd,omitempty"`
}

// NewWorkflows is the daemon's workflow scheduler over st: shell steps behind the guard, agent
// steps through `smithy agent --profile auto`, notify steps as store events (TUI, web and the
// chat connectors).
func (sv *Server) NewWorkflows(st workflow.Store) *workflow.Scheduler {
	return &workflow.Scheduler{
		Store: st,
		Env: workflow.Env{Shell: workflow.Shell, Agent: workflow.Agent, Guard: workflow.Guard,
			Notify: func(wf, title, text string) error {
				_, err := sv.Append(KindWorkflowNote, "", workflowNote{Workflow: wf, Title: title, Text: text})
				return err
			}},
		OnRun: func(r workflow.Record) {
			_, _ = sv.Append(KindWorkflowRun, "", workflowRun{Workflow: r.Workflow, Status: r.Status, Trigger: r.Trigger, Error: r.Error, CostUSD: r.CostUSD})
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
	l, err := sv.Workflows.Store.List()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
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
	writeJSON(w, 200, WorkflowDetail{Info: in, Source: string(src), History: h, Running: sv.Workflows.Running()[name]})
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

// runWorkflow is POST /v1/workflows/{name}/run: start one run now, enabled or not.
func (sv *Server) runWorkflow(w http.ResponseWriter, r *http.Request) {
	name, ok := sv.workflowWrite(w, r)
	if !ok {
		return
	}
	if sv.Workflows.Running()[name] {
		writeJSON(w, 409, map[string]string{"error": "workflow " + name + " is already running"})
		return
	}
	ctx := sv.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	trigger := "web"
	if r.URL.Query().Get("via") == "tui" || r.URL.Query().Get("via") == "cli" {
		trigger = r.URL.Query().Get("via")
	}
	go func() { _, _ = sv.Workflows.RunNow(ctx, name, trigger) }()
	writeJSON(w, 202, map[string]string{"status": "started"})
}

func (sv *Server) enableWorkflow(w http.ResponseWriter, r *http.Request) {
	if op := r.PathValue("op"); op != "enable" && op != "disable" {
		http.NotFound(w, r)
		return
	}
	name, ok := sv.workflowWrite(w, r)
	if !ok {
		return
	}
	var err error
	if r.PathValue("op") == "enable" {
		err = sv.Workflows.Store.Enable(name, time.Now())
	} else {
		err = sv.Workflows.Store.Disable(name)
	}
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	in, _ := sv.workflowInfo(name)
	writeJSON(w, 200, in)
}

// saveWorkflow is PUT /v1/workflows/{name}: the edited file, validated; the name can't change.
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

// WorkflowsScheduled reports whether a workflow runs or is enabled: the idle exit must wait, or
// its schedule would never fire.
func (sv *Server) WorkflowsScheduled() bool {
	if sv.Workflows == nil {
		return false
	}
	if len(sv.Workflows.Running()) > 0 {
		return true
	}
	l, _ := sv.Workflows.Store.List()
	for _, in := range l {
		if in.Enabled {
			return true
		}
	}
	return false
}
