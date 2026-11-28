// Package workflow runs workflows: triggers (a cron schedule, by hand, a webhook) and steps that
// compose (run a command, ask an agent, branch, set variables, pick JSON fields, loop, ask a
// person, notify, call another workflow). Workflows are JSON files under ~/.smithy/workflows and
// run unattended only once the user enabled them; the enable pins the file's hash, so a file
// changed outside smithy stops running until it is enabled again. Each run is kept with every
// step's input, output, duration and status.
package workflow

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Step kinds.
const (
	KindRun     = "run"
	KindAgent   = "agent"
	KindIf      = "if"
	KindSwitch  = "switch"
	KindSet     = "set"
	KindJSON    = "json"
	KindForeach = "foreach"
	KindAsk     = "ask"
	KindNotify  = "notify"
	KindCall    = "call"
	KindJudge   = "judge"
	KindLoop    = "loop"
	KindSession = "session"
	KindSmelt   = "smelt"
	KindWarden  = "warden"
	KindHTTP    = "http"
	KindPar     = "parallel"
	KindWait    = "wait"
	KindGitHub  = "github"
	KindFile    = "file"
)

var kinds = []string{KindRun, KindAgent, KindIf, KindSwitch, KindSet, KindJSON, KindForeach, KindAsk, KindNotify, KindCall,
	KindJudge, KindLoop, KindSession, KindSmelt, KindWarden, KindHTTP, KindPar, KindWait, KindGitHub, KindFile}

// GitHub actions; the write ones need allow_write, pr and merge an ask step before them.
var (
	ghRead  = map[string]bool{"alerts": true, "prs": true, "issues": true}
	ghWrite = map[string]bool{"comment": true, "label": true, "issue": true, "pr": true, "merge": true}
)

// MaxLoop caps a loop's tries.
const MaxLoop = 10

// Stop as a branch or on_error target ends the run; Continue as on_error goes on to the next step.
const (
	Stop     = "stop"
	Continue = "continue"
)

// Caps when a workflow sets none.
const (
	DefaultMaxMinutes = 30
	DefaultMaxAgents  = 3
	DefaultMaxCostUSD = 1.0
	DefaultTimeout    = 10 * time.Minute
	MaxOutput         = 64 << 10
	MaxCallDepth      = 3
	MaxRetries        = 5
	MaxItems          = 200
	MaxParallel       = 8
)

// Workflow is one workflow file: a flow you run by hand (a schedule attaches triggers to it).
// Inputs are names with default values (the form Run shows), Vars the starting variables,
// Outputs templates filled when the run ends.
type Workflow struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Dir         string            `json:"dir,omitempty"`
	Inputs      map[string]string `json:"inputs,omitempty"`
	Vars        map[string]string `json:"vars,omitempty"`
	Outputs     map[string]string `json:"outputs,omitempty"`
	MaxMinutes  int               `json:"max_minutes,omitempty"`
	MaxAgents   int               `json:"max_agents,omitempty"`
	MaxCostUSD  float64           `json:"max_cost_usd,omitempty"`
	AllowWrite  bool              `json:"allow_write,omitempty"`
	Files       []string          `json:"files,omitempty"`
	Steps       []Step            `json:"steps"`
}

// Step is one step. Every kind takes when (skip it unless the condition holds), timeout (a
// duration like 30s), retry and on_error (stop, continue or a later step's id).
type Step struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	When    *Cond  `json:"when,omitempty"`
	Timeout string `json:"timeout,omitempty"`
	Retry   *Retry `json:"retry,omitempty"`
	OnError string `json:"on_error,omitempty"`

	Run string `json:"run,omitempty"`

	Prompt  string            `json:"prompt,omitempty"`
	Answer  string            `json:"answer,omitempty"`
	Options []string          `json:"options,omitempty"`
	Schema  map[string]string `json:"schema,omitempty"`
	Model   string            `json:"model,omitempty"`
	Effort  string            `json:"effort,omitempty"`

	If   *Cond  `json:"if,omitempty"`
	Then string `json:"then,omitempty"`
	Else string `json:"else,omitempty"`

	Value   string `json:"value,omitempty"`
	Cases   []Case `json:"cases,omitempty"`
	Default string `json:"default,omitempty"`

	Vars map[string]string `json:"vars,omitempty"`

	From string `json:"from,omitempty"`
	Path string `json:"path,omitempty"`

	Items    string `json:"items,omitempty"`
	Lines    bool   `json:"lines,omitempty"`
	Parallel int    `json:"parallel,omitempty"`
	Limit    int    `json:"limit,omitempty"`
	Do       []Step `json:"do,omitempty"`

	Question string `json:"question,omitempty"`

	Title   string `json:"title,omitempty"`
	Message string `json:"message,omitempty"`

	Workflow string            `json:"workflow,omitempty"`
	With     map[string]string `json:"with,omitempty"`

	Write    bool     `json:"write,omitempty"`
	Subject  string   `json:"subject,omitempty"`
	Criteria []string `json:"criteria,omitempty"`
	Pass     float64  `json:"pass,omitempty"`
	Until    *Cond    `json:"until,omitempty"`
	Max      int      `json:"max,omitempty"`
	Task     string   `json:"task,omitempty"`
	Speed    string   `json:"speed,omitempty"`
	Agent    string   `json:"agent,omitempty"`

	Method   string            `json:"method,omitempty"`
	URL      string            `json:"url,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"`
	Body     string            `json:"body,omitempty"`
	Branches [][]Step          `json:"branches,omitempty"`
	For      string            `json:"for,omitempty"`
	Check    string            `json:"check,omitempty"`
	Every    string            `json:"every,omitempty"`
	Action   string            `json:"action,omitempty"`
	PR       string            `json:"pr,omitempty"`
	Label    string            `json:"label,omitempty"`
	Op       string            `json:"op,omitempty"`
	Content  string            `json:"content,omitempty"`
}

// children are a step's nested blocks: a foreach's or loop's do, a parallel's branches.
func (s Step) children() [][]Step {
	out := [][]Step{}
	if len(s.Do) > 0 {
		out = append(out, s.Do)
	}
	return append(out, s.Branches...)
}

// Retry runs a failed step again up to Count times, waiting Backoff (doubling) between tries.
type Retry struct {
	Count   int    `json:"count"`
	Backoff string `json:"backoff,omitempty"`
}

// Case is one switch arm: Equals (case-insensitive) or Match (regex) sends it to Goto.
type Case struct {
	Equals string `json:"equals,omitempty"`
	Match  string `json:"match,omitempty"`
	Goto   string `json:"goto"`
}

// Cond tests one value. The subject is Value (a template) or, when empty, step Step's answer
// (its output when it has none). Exactly one test: Exit (step Step's exit code), Match (regex),
// Equals (case-insensitive), or Path (a JSON field of the subject) compared by Op (eq, ne, gt,
// ge, lt, le, contains, exists) with To.
type Cond struct {
	Value  string `json:"value,omitempty"`
	Step   string `json:"step,omitempty"`
	Exit   *int   `json:"exit,omitempty"`
	Match  string `json:"match,omitempty"`
	Equals string `json:"equals,omitempty"`
	Path   string `json:"path,omitempty"`
	Op     string `json:"op,omitempty"`
	To     string `json:"to,omitempty"`
}

var (
	nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,47}$`)
	idRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)
	varRe  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,31}$`)
	ops    = map[string]bool{"": true, "eq": true, "ne": true, "gt": true, "ge": true, "lt": true, "le": true, "contains": true, "exists": true}
)

// Parse decodes and validates a workflow file; unknown fields are an error.
func Parse(b []byte) (Workflow, error) {
	var w Workflow
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if err := d.Decode(&w); err != nil {
		if strings.Contains(err.Error(), `"schedule"`) || strings.Contains(err.Error(), `"webhook"`) {
			return w, fmt.Errorf("workflow: schedule and webhook moved to schedules (smithy schedule add <name> --workflow <workflow> --cron …)")
		}
		return w, fmt.Errorf("workflow: %w", err)
	}
	return w, w.Validate()
}

// Validate checks the name, triggers, caps, variables and every step.
func (w Workflow) Validate() error {
	if !nameRe.MatchString(w.Name) {
		return fmt.Errorf("workflow: name %q: use a-z, 0-9 and -", w.Name)
	}
	if w.MaxMinutes < 0 || w.MaxAgents < 0 || w.MaxCostUSD < 0 {
		return fmt.Errorf("workflow %s: caps can't be negative", w.Name)
	}
	for _, m := range []map[string]string{w.Inputs, w.Vars, w.Outputs} {
		for k := range m {
			if !varRe.MatchString(k) {
				return fmt.Errorf("workflow %s: name %q: use letters, digits and _", w.Name, k)
			}
		}
	}
	if len(w.Steps) == 0 {
		return fmt.Errorf("workflow %s: no steps", w.Name)
	}
	ids := map[string]bool{}
	if err := collect(w.Steps, ids); err != nil {
		return fmt.Errorf("workflow %s: %w", w.Name, err)
	}
	v := validator{ids: ids, write: w.AllowWrite, files: len(w.Files) > 0}
	for _, t := range w.Outputs {
		if err := v.tmpl(t); err != nil {
			return fmt.Errorf("workflow %s: outputs: %w", w.Name, err)
		}
	}
	for _, t := range w.Vars {
		if err := v.tmpl(t); err != nil {
			return fmt.Errorf("workflow %s: vars: %w", w.Name, err)
		}
	}
	if err := v.block(w.Steps); err != nil {
		return fmt.Errorf("workflow %s: %w", w.Name, err)
	}
	return nil
}

func collect(steps []Step, ids map[string]bool) error {
	for i, s := range steps {
		if !idRe.MatchString(s.ID) || s.ID == Stop || s.ID == Continue {
			return fmt.Errorf("step %d: id %q: use a-z, 0-9, _ and -", i+1, s.ID)
		}
		if ids[s.ID] {
			return fmt.Errorf("step id %q used twice", s.ID)
		}
		ids[s.ID] = true
		for _, c := range s.children() {
			if err := collect(c, ids); err != nil {
				return err
			}
		}
	}
	return nil
}

type validator struct {
	ids          map[string]bool
	write, files bool
	secretOK     bool
	asks         map[string]bool
}

func (v validator) tmpl(text string) error {
	if strings.Count(text, "{{") != len(tmplRe.FindAllString(text, -1)) {
		return fmt.Errorf("unknown template in %q: use {{steps.<id>.output|answer|why|exit|status}}, {{vars.x}}, {{inputs.x}}, {{env.X}}, {{item}} or {{index}}", clipText(text, 60))
	}
	for _, m := range tmplRe.FindAllStringSubmatch(text, -1) {
		if m[1] != "" && !v.ids[m[1]] {
			return fmt.Errorf("template %s names no step", m[0])
		}
		if m[7] != "" && !v.secretOK {
			return fmt.Errorf("%s works only in an http step's headers and body", m[0])
		}
	}
	return nil
}

func (v validator) cond(c *Cond) error {
	if c == nil {
		return nil
	}
	if c.Step != "" && !v.ids[c.Step] {
		return fmt.Errorf("condition names no step %q", c.Step)
	}
	if c.Value == "" && c.Step == "" {
		return fmt.Errorf("condition needs value or step")
	}
	if c.Exit != nil && c.Step == "" {
		return fmt.Errorf("an exit test needs step")
	}
	n := 0
	for _, set := range []bool{c.Exit != nil, c.Match != "", c.Equals != "", c.Path != ""} {
		if set {
			n++
		}
	}
	if n != 1 {
		return fmt.Errorf("condition needs exactly one of exit, match, equals, path")
	}
	if c.Match != "" {
		if _, err := regexp.Compile(c.Match); err != nil {
			return fmt.Errorf("match: %v", err)
		}
	}
	if !ops[c.Op] {
		return fmt.Errorf("op is eq, ne, gt, ge, lt, le, contains or exists, not %q", c.Op)
	}
	return v.tmpl(c.Value + c.To)
}

func (v validator) block(steps []Step) error {
	pos := map[string]int{}
	asks := map[string]bool{}
	for k := range v.asks {
		asks[k] = true
	}
	v.asks = asks
	for i, s := range steps {
		pos[s.ID] = i
	}
	for i, s := range steps {
		target := func(what, t string, extra ...string) error {
			for _, e := range extra {
				if t == e {
					return nil
				}
			}
			if p, ok := pos[t]; !ok || p <= i {
				return fmt.Errorf("%s goes to %q, which is not a later step in the same block", what, t)
			}
			return nil
		}
		err := v.step(s, target)
		if s.Kind == KindAsk {
			v.asks[s.ID] = true
		}
		if err == nil && s.OnError != "" {
			err = target("on_error", s.OnError, Stop, Continue)
		}
		if err != nil {
			return fmt.Errorf("step %s: %w", s.ID, err)
		}
	}
	return nil
}

func (v validator) step(s Step, target func(string, string, ...string) error) error {
	if s.Timeout != "" {
		if d, err := time.ParseDuration(s.Timeout); err != nil || d <= 0 {
			return fmt.Errorf("timeout %q: use a duration like 30s or 5m", s.Timeout)
		}
	}
	if r := s.Retry; r != nil {
		if r.Count < 0 || r.Count > MaxRetries {
			return fmt.Errorf("retry count is 0-%d", MaxRetries)
		}
		if r.Backoff != "" {
			if _, err := time.ParseDuration(r.Backoff); err != nil {
				return fmt.Errorf("retry backoff %q: use a duration", r.Backoff)
			}
		}
	}
	if err := v.cond(s.When); err != nil {
		return fmt.Errorf("when: %w", err)
	}
	switch s.Kind {
	case KindRun:
		if strings.TrimSpace(s.Run) == "" {
			return fmt.Errorf("run needs a command")
		}
		if strings.Contains(s.Run, "{{") {
			return fmt.Errorf("run commands take no templates: read $SMITHY_VAR_<name>, $SMITHY_INPUT_<name> or $SMITHY_ITEM instead")
		}
	case KindAgent:
		if strings.TrimSpace(s.Prompt) == "" {
			return fmt.Errorf("agent needs a prompt")
		}
		switch s.Answer {
		case "", "yesno", "json":
		case "enum":
			if len(s.Options) < 2 {
				return fmt.Errorf("an enum answer needs two or more options")
			}
		default:
			return fmt.Errorf("answer is \"\", yesno, enum or json, not %q", s.Answer)
		}
		for k, t := range s.Schema {
			if !map[string]bool{"string": true, "number": true, "bool": true, "array": true, "object": true}[t] {
				return fmt.Errorf("schema %s: type is string, number, bool, array or object", k)
			}
		}
		if len(s.Schema) > 0 && s.Answer != "json" {
			return fmt.Errorf("schema needs answer json")
		}
		if s.Write && !v.write {
			return fmt.Errorf("a writing agent needs \"allow_write\": true on the workflow")
		}
		if s.Items != "" {
			if s.Parallel < 0 || s.Parallel > MaxParallel || s.Limit < 0 || s.Limit > MaxItems {
				return fmt.Errorf("parallel is 0-%d and limit 0-%d", MaxParallel, MaxItems)
			}
			if err := v.tmpl(s.Items); err != nil {
				return err
			}
		}
		return v.tmpl(s.Prompt)
	case KindIf:
		if s.If == nil {
			return fmt.Errorf("if needs an if condition")
		}
		if err := v.cond(s.If); err != nil {
			return err
		}
		if err := target("then", s.Then, "", Stop); err != nil {
			return err
		}
		return target("else", s.Else, "", Stop)
	case KindSwitch:
		if len(s.Cases) == 0 || s.Value == "" {
			return fmt.Errorf("switch needs value and cases")
		}
		for _, c := range s.Cases {
			if (c.Equals == "") == (c.Match == "") {
				return fmt.Errorf("each case needs exactly one of equals, match")
			}
			if c.Match != "" {
				if _, err := regexp.Compile(c.Match); err != nil {
					return fmt.Errorf("case match: %v", err)
				}
			}
			if err := target("case", c.Goto, Stop); err != nil {
				return err
			}
		}
		if err := target("default", s.Default, "", Stop); err != nil {
			return err
		}
		return v.tmpl(s.Value)
	case KindSet:
		if len(s.Vars) == 0 {
			return fmt.Errorf("set needs vars")
		}
		for k, t := range s.Vars {
			if !varRe.MatchString(k) {
				return fmt.Errorf("var %q: use letters, digits and _", k)
			}
			if err := v.tmpl(t); err != nil {
				return err
			}
		}
	case KindJSON:
		if s.From == "" {
			return fmt.Errorf("json needs from")
		}
		if _, err := splitPath(s.Path); err != nil {
			return err
		}
		return v.tmpl(s.From)
	case KindForeach:
		if s.Items == "" || len(s.Do) == 0 {
			return fmt.Errorf("foreach needs items and do")
		}
		if s.Parallel < 0 || s.Parallel > MaxParallel || s.Limit < 0 || s.Limit > MaxItems {
			return fmt.Errorf("parallel is 0-%d and limit 0-%d", MaxParallel, MaxItems)
		}
		if err := v.tmpl(s.Items); err != nil {
			return err
		}
		return v.block(s.Do)
	case KindAsk:
		if strings.TrimSpace(s.Question) == "" {
			return fmt.Errorf("ask needs a question")
		}
		return v.tmpl(s.Question)
	case KindNotify:
		if strings.TrimSpace(s.Message) == "" {
			return fmt.Errorf("notify needs a message")
		}
		return v.tmpl(s.Title + s.Message)
	case KindCall:
		if !nameRe.MatchString(s.Workflow) {
			return fmt.Errorf("call needs workflow: a workflow name")
		}
		for k, t := range s.With {
			if !varRe.MatchString(k) {
				return fmt.Errorf("with %q: use letters, digits and _", k)
			}
			if err := v.tmpl(t); err != nil {
				return err
			}
		}
	case KindJudge:
		if s.Subject == "" || len(s.Criteria) == 0 {
			return fmt.Errorf("judge needs subject and criteria")
		}
		if s.Pass < 0 || s.Pass > 1 {
			return fmt.Errorf("pass is a score from 0 to 1")
		}
		return v.tmpl(s.Subject + strings.Join(s.Criteria, " "))
	case KindLoop:
		if len(s.Do) == 0 || s.Until == nil {
			return fmt.Errorf("loop needs do and until")
		}
		if s.Max < 0 || s.Max > MaxLoop {
			return fmt.Errorf("max is 1-%d", MaxLoop)
		}
		if err := v.block(s.Do); err != nil {
			return err
		}
		return v.cond(s.Until)
	case KindSession, KindSmelt, KindWarden:
		text := s.Prompt
		if s.Kind != KindSession {
			text = s.Task
		}
		if strings.TrimSpace(text) == "" {
			return fmt.Errorf("%s needs %s", s.Kind, map[string]string{KindSession: "a prompt", KindSmelt: "a task", KindWarden: "a task"}[s.Kind])
		}
		if !v.write {
			return fmt.Errorf("%s changes code: the workflow needs \"allow_write\": true", s.Kind)
		}
		if s.Kind != KindWarden && !v.askedBefore(s) && (strings.Contains(text, "{{inputs.") || strings.Contains(text, "{{steps.")) {
			return fmt.Errorf("a %s prompt built from inputs or step outputs can be steered: put an ask step before it and gate it with when: {step: <ask>, equals: yes}", s.Kind)
		}
		if s.Kind == KindWarden && !v.askedBefore(s) {
			return fmt.Errorf("warden starts sessions that cost money: put an ask step before it and gate it with when: {step: <ask>, equals: yes}")
		}
		if s.Speed != "" && s.Speed != "fast" && s.Speed != "normal" && s.Speed != "full" {
			return fmt.Errorf("speed is fast, normal or full")
		}
		return v.tmpl(text)
	case KindHTTP:
		m := strings.ToUpper(s.Method)
		if m == "" {
			m = "GET"
		}
		if !map[string]bool{"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true}[m] {
			return fmt.Errorf("method is GET, POST, PUT, PATCH or DELETE")
		}
		if m != "GET" && !v.write {
			return fmt.Errorf("%s changes things elsewhere: the workflow needs \"allow_write\": true", m)
		}
		if !strings.HasPrefix(s.URL, "https://") && !strings.HasPrefix(s.URL, "http://") {
			return fmt.Errorf("url must start with https:// or http://")
		}
		if strings.HasPrefix(s.URL, "http://") && strings.Contains(fmt.Sprint(s.Headers)+s.Body, "{{secret.") {
			return fmt.Errorf("a secret goes only over https")
		}
		if err := v.tmpl(s.URL); err != nil {
			return err
		}
		sv := v
		sv.secretOK = true
		for k, h := range s.Headers {
			if k == "" || strings.ContainsAny(k, " :\r\n") {
				return fmt.Errorf("header name %q", k)
			}
			if err := sv.tmpl(h); err != nil {
				return err
			}
		}
		return sv.tmpl(s.Body)
	case KindPar:
		if len(s.Branches) < 2 {
			return fmt.Errorf("parallel needs two or more branches")
		}
		for _, b := range s.Branches {
			if len(b) == 0 {
				return fmt.Errorf("a parallel branch is empty")
			}
			if err := v.block(b); err != nil {
				return err
			}
		}
	case KindWait:
		if (s.For == "") == (s.Check == "") {
			return fmt.Errorf("wait needs exactly one of for (a duration) or check (a command polled until it exits 0)")
		}
		for _, d := range []string{s.For, s.Every} {
			if d != "" {
				if x, err := time.ParseDuration(d); err != nil || x <= 0 {
					return fmt.Errorf("duration %q: use one like 30s", d)
				}
			}
		}
		if strings.Contains(s.Check, "{{") {
			return fmt.Errorf("check commands take no templates")
		}
	case KindGitHub:
		switch {
		case ghRead[s.Action]:
		case ghWrite[s.Action]:
			if !v.write {
				return fmt.Errorf("github %s changes the repo: the workflow needs \"allow_write\": true", s.Action)
			}
			if (s.Action == "merge" || s.Action == "pr") && !v.askedBefore(s) {
				return fmt.Errorf("github %s needs an ask step before it and when: {step: <ask>, equals: yes}", s.Action)
			}
		default:
			return fmt.Errorf("github action is alerts, prs, issues, comment, label, issue, pr or merge")
		}
		return v.tmpl(s.PR + s.Body + s.Title + s.Label)
	case KindFile:
		if !v.files {
			return fmt.Errorf("file needs the folders it may use in the workflow's \"files\"")
		}
		switch s.Op {
		case "read":
		case "write", "append":
			if !v.write {
				return fmt.Errorf("file %s needs \"allow_write\": true", s.Op)
			}
		default:
			return fmt.Errorf("file op is read, write or append")
		}
		if s.Path == "" {
			return fmt.Errorf("file needs a path")
		}
		return v.tmpl(s.Path + s.Content)
	default:
		return fmt.Errorf("kind is one of %s, not %q", strings.Join(kinds, ", "), s.Kind)
	}
	return nil
}

// askedBefore reports whether s runs only on a yes from an earlier ask step in its block.
func (v validator) askedBefore(s Step) bool {
	c := s.When
	return c != nil && v.asks[c.Step] && c.Value == "" && strings.EqualFold(c.Equals, "yes")
}

func (s Step) timeout() time.Duration {
	if d, err := time.ParseDuration(s.Timeout); err == nil && d > 0 {
		return d
	}
	return DefaultTimeout
}

func (w Workflow) maxMinutes() int {
	if w.MaxMinutes > 0 {
		return w.MaxMinutes
	}
	return DefaultMaxMinutes
}

func (w Workflow) maxAgents() int {
	if w.MaxAgents > 0 {
		return w.MaxAgents
	}
	return DefaultMaxAgents
}

func (w Workflow) maxCost() float64 {
	if w.MaxCostUSD > 0 {
		return w.MaxCostUSD
	}
	return DefaultMaxCostUSD
}

func clipText(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
