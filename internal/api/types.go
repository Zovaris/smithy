//go:build harness

package api

import (
	"time"

	"github.com/Zovaris/smithy/internal/agents"
	"github.com/Zovaris/smithy/internal/agents/skills"
	"github.com/Zovaris/smithy/internal/ghx"
	"github.com/Zovaris/smithy/internal/usage"
	"github.com/Zovaris/smithy/internal/workflow"
)

// AgentChoice is the agent (and model, for a CLI) new sessions start with.
type AgentChoice struct {
	Agent string `json:"agent"`
	Model string `json:"model,omitempty"`
}

// BusStartReq is what a session asks for when it starts a child session.
type BusStartReq struct {
	Agent   string `json:"agent"`
	Model   string `json:"model"`
	Effort  string `json:"effort"`
	Dir     string `json:"dir"`
	Message string `json:"message"`
}

// ConnectTestResult is POST /v1/connect/test: one line per channel tried, or why none was.
type ConnectTestResult struct {
	Results []string `json:"results"`
	Error   string   `json:"error,omitempty"`
}

// Decision is a question waiting on (or answered by) someone.
type Decision struct {
	ID       string    `json:"id"`
	Question string    `json:"question"`
	Options  []string  `json:"options,omitempty"`
	Multi    bool      `json:"multi,omitempty"` // several options may be picked; the answer joins them with ", "
	Other    bool      `json:"other,omitempty"` // a free-text answer is taken besides the options
	Asked    time.Time `json:"asked"`
	Outcome  *Outcome  `json:"outcome,omitempty"`
}

// DirList is GET /v1/fs: the folders inside Dir, hidden ones left out.
type DirList struct {
	Dir    string   `json:"dir"`
	Parent string   `json:"parent,omitempty"`
	Dirs   []string `json:"dirs"`
}

// KeyRow is one stored API key, masked.
type KeyRow struct {
	Service string `json:"service"`
	Masked  string `json:"masked"`
}

// KitChoice is what goes into every driven CLI (kit.mcp, kit.skills).
type KitChoice struct {
	MCP    bool     `json:"mcp"`
	Skills []string `json:"skills"`
}

// LinkIssue is one issue a session links: a Linear issue (ID TK-123) or a GitHub issue (ID #12).
type LinkIssue struct {
	Kind      string `json:"kind"`
	ID        string `json:"id"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	Status    string `json:"status"`
	Assignee  string `json:"assignee,omitempty"`
	Milestone string `json:"milestone,omitempty"`
}

// Mail is one message between two sessions of this daemon; N orders it (the store sequence).
type Mail struct {
	N    int64     `json:"n"`
	From string    `json:"from"`
	To   string    `json:"to"`
	Text string    `json:"text"`
	Time time.Time `json:"time"`
}

// Outcome is the recorded answer. Won is true only in the response to the answer that won.
type Outcome struct {
	ID     string    `json:"id"`
	Answer string    `json:"answer"`
	By     string    `json:"by"`
	At     time.Time `json:"at"`
	Won    bool      `json:"won"`
}

// Project is one folder new sessions on this daemon can start in.
type Project struct {
	Dir  string `json:"dir"`
	Name string `json:"name"`
}

// ProjectList is GET /v1/projects: this daemon's projects in the order added, and the default.
type ProjectList struct {
	Projects []Project `json:"projects"`
	Default  string    `json:"default,omitempty"`
}

// PromptState says which file the system prompt's base text comes from: the user file at Path
// when Active, else the default built into smithy.
type PromptState struct {
	Path   string `json:"path"`
	Active bool   `json:"active"`
}

// ScheduleDetail is GET /v1/schedules/{name}: the schedule, its source, the runs it started and,
// for a webhook on this machine, its token.
type ScheduleDetail struct {
	Info    workflow.SchedInfo `json:"info"`
	Source  string             `json:"source"`
	History []workflow.Record  `json:"history"`
	Hook    string             `json:"hook,omitempty"`
}

// ScheduleList is GET /v1/schedules.
type ScheduleList struct {
	Schedules []workflow.SchedInfo `json:"schedules"`
}

// Session is one conversation with an agent in a folder.
type Session struct {
	ID      string       `json:"id"`
	Agent   string       `json:"agent"`
	Model   string       `json:"model,omitempty"`
	Effort  string       `json:"effort,omitempty"`
	Dir     string       `json:"dir"`
	Title   string       `json:"title"`
	Status  string       `json:"status"`
	Detail  string       `json:"detail,omitempty"`
	Created time.Time    `json:"created"`
	Updated time.Time    `json:"updated"`
	Usage   *usage.Usage `json:"usage,omitempty"`
	PRs     []string     `json:"prs,omitempty"`
	Links   []string     `json:"links,omitempty"`
	Parent  string       `json:"parent,omitempty"`
	// Goal is pinned by a person (/goal); the daemon continues the session toward it.
	Goal      string `json:"goal,omitempty"`
	GoalState string `json:"goal_state,omitempty"`
	GoalTurns int    `json:"goal_turns,omitempty"`
}

// SessionLinks is GET /v1/sessions/{id}/links: the PRs a session opened or works on (its
// branch's PR, a linked URL) and the issues named in its branch, PRs, prompts or /link refs.
type SessionLinks struct {
	Session string       `json:"session"`
	PRs     []ghx.PRLink `json:"prs"`
	Issues  []LinkIssue  `json:"issues"`
	Error   string       `json:"error,omitempty"`
	Checked time.Time    `json:"checked"`
}

// SessionList is GET /v1/sessions.
type SessionList struct {
	Sessions  []Session `json:"sessions"`
	LastAgent string    `json:"last_agent"`
	LastModel string    `json:"last_model,omitempty"`
}

// SettingRow is one known key with its effective value and where it comes from; secrets are masked.
type SettingRow struct {
	Key     string   `json:"key"`
	Value   string   `json:"value"`
	Src     string   `json:"src"`
	Default string   `json:"default"`
	Meaning string   `json:"meaning"`
	Secret  bool     `json:"secret,omitempty"`
	Kind    string   `json:"kind"`
	Options []string `json:"options,omitempty"`
}

// Settings is GET /v1/settings: every setting, the stored API keys and whether serve may launch.
type Settings struct {
	Settings []SettingRow `json:"settings"`
	Keys     []KeyRow     `json:"keys"`
	Serve    bool         `json:"serve"`
	Prompt   PromptState  `json:"prompt"`
}

// Setup is GET /v1/setup: what first run and the pickers show.
type Setup struct {
	Agents    []agents.Ready            `json:"agents"`
	Default   AgentChoice               `json:"default"`
	Chosen    bool                      `json:"chosen"` // false on first run: no agent picked or used yet
	Kit       KitChoice                 `json:"kit"`
	Skills    []string                  `json:"skills"` // installed skills the kit can link in
	Paths     map[string]string         `json:"paths"`  // skill name to its SKILL.md, to invoke it from chat
	Harnesses []skills.HarnessInventory `json:"harnesses"`
	Effort    agents.EffortRules        `json:"effort"` // the CLIs that take effort and the levels a session may set
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

// TaskBoard is GET /v1/tasks: a project's task lists, the files `smithy tasks` reads.
type TaskBoard struct {
	Project  string     `json:"project"`
	Statuses []string   `json:"statuses"`
	Lists    []TaskList `json:"lists"`
}

// TaskCard is one task file.
type TaskCard struct {
	File     string   `json:"file"`
	NN       string   `json:"nn"`
	Title    string   `json:"title"`
	Status   string   `json:"status"`
	PR       string   `json:"pr,omitempty"`
	Blockers []string `json:"blockers"`
}

// TaskList is one task dir and its tasks in NN order.
type TaskList struct {
	Dir   string     `json:"dir"`
	Name  string     `json:"name"`
	Tasks []TaskCard `json:"tasks"`
}

// TendList is GET /v1/tend.
type TendList struct {
	Mode string   `json:"mode"`
	PRs  []TendPR `json:"prs"`
}

// TendPR is what the daemon knows of one watched PR. Status: green, waiting, fixing (a tend
// session is working on it), needs-fix (watch mode: a fix is due), needs-you (tend's needs-you,
// or two tend sessions ended with the same action left), closed, error.
type TendPR struct {
	TendRef
	Action  string    `json:"action"`
	Checks  string    `json:"checks"`
	Status  string    `json:"status"`
	PID     int       `json:"pid,omitempty"`
	Log     string    `json:"log,omitempty"`
	Tries   int       `json:"tries,omitempty"`
	Total   int       `json:"total,omitempty"`
	Error   string    `json:"error,omitempty"`
	Checked time.Time `json:"checked"`
}

// TendRef is one PR to tend: a warden batch's (its card names the PR, its plan item the
// worktree) or a chat session's (a pr create it ran printed the PR; Dir is the session's folder).
type TendRef struct {
	URL     string `json:"url"`
	Batch   string `json:"batch"`
	Item    string `json:"item"`
	Session string `json:"session,omitempty"`
	Dir     string `json:"dir"`
}

// WorkflowDetail is GET /v1/workflows/{name}: the parsed file, its source, newest runs and the
// run in progress.
type WorkflowDetail struct {
	Info    workflow.Info     `json:"info"`
	Source  string            `json:"source"`
	History []workflow.Record `json:"history"`
	Running bool              `json:"running"`
	Live    *workflow.Record  `json:"live,omitempty"`
}

// WorkflowList is GET /v1/workflows.
type WorkflowList struct {
	Workflows []workflow.Info `json:"workflows"`
	Running   []string        `json:"running"`
}
