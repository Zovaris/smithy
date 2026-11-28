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
)

var kinds = []string{KindRun, KindAgent, KindIf, KindSwitch, KindSet, KindJSON, KindForeach, KindAsk, KindNotify, KindCall}

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

// Workflow is one workflow file. Schedule is a cron line ("" runs only by hand or webhook);
// Webhook lets a token-checked POST start it. Inputs are names with default values, Vars the
// starting variables, Outputs templates filled when the run ends.
type Workflow struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Schedule    string            `json:"schedule,omitempty"`
	Webhook     bool              `json:"webhook,omitempty"`
	Dir         string            `json:"dir,omitempty"`
	Inputs      map[string]string `json:"inputs,omitempty"`
	Vars        map[string]string `json:"vars,omitempty"`
	Outputs     map[string]string `json:"outputs,omitempty"`
	MaxMinutes  int               `json:"max_minutes,omitempty"`
	MaxAgents   int               `json:"max_agents,omitempty"`
	MaxCostUSD  float64           `json:"max_cost_usd,omitempty"`
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
		return w, fmt.Errorf("workflow: %w", err)
	}
	return w, w.Validate()
}

// Validate checks the name, triggers, caps, variables and every step.
func (w Workflow) Validate() error {
	if !nameRe.MatchString(w.Name) {
		return fmt.Errorf("workflow: name %q: use a-z, 0-9 and -", w.Name)
	}
	if w.Schedule != "" {
		if _, err := ParseCron(w.Schedule); err != nil {
			return fmt.Errorf("workflow %s: %w", w.Name, err)
		}
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
	v := validator{ids: ids}
	for _, t := range w.Outputs {
		if err := v.tmpl(t); err != nil {
			return fmt.Errorf("workflow %s: outputs: %w", w.Name, err)
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
		if err := collect(s.Do, ids); err != nil {
			return err
		}
	}
	return nil
}

type validator struct{ ids map[string]bool }

func (v validator) tmpl(text string) error {
	if strings.Count(text, "{{") != len(tmplRe.FindAllString(text, -1)) {
		return fmt.Errorf("unknown template in %q: use {{steps.<id>.output|answer|why|exit|status}}, {{vars.x}}, {{inputs.x}}, {{env.X}}, {{item}} or {{index}}", clipText(text, 60))
	}
	for _, m := range tmplRe.FindAllStringSubmatch(text, -1) {
		if m[1] != "" && !v.ids[m[1]] {
			return fmt.Errorf("template %s names no step", m[0])
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
	default:
		return fmt.Errorf("kind is one of %s, not %q", strings.Join(kinds, ", "), s.Kind)
	}
	return nil
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
