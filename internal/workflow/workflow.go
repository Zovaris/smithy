// Package workflow runs scheduled workflows: a cron schedule plus ordered steps (run a shell
// command, ask an agent, branch, notify). Workflows are JSON files under ~/.smithy/workflows and
// run only once the user enabled them; the enable pins the file's hash, so a file changed
// outside smithy stops running until it is enabled again. Each run is kept with every step's
// output.
package workflow

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Step kinds.
const (
	KindRun    = "run"
	KindAgent  = "agent"
	KindIf     = "if"
	KindNotify = "notify"
)

// Stop as a branch target ends the run.
const Stop = "stop"

// Caps when a workflow sets none.
const (
	DefaultMaxMinutes = 30
	DefaultMaxAgents  = 3
	DefaultMaxCostUSD = 1.0
	MaxOutput         = 64 << 10
)

// Workflow is one workflow file.
type Workflow struct {
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	Schedule    string  `json:"schedule"`
	Dir         string  `json:"dir,omitempty"`
	MaxMinutes  int     `json:"max_minutes,omitempty"`
	MaxAgents   int     `json:"max_agents,omitempty"`
	MaxCostUSD  float64 `json:"max_cost_usd,omitempty"`
	Steps       []Step  `json:"steps"`
}

// Step is one step. run: Run is a shell command (no templates: earlier outputs never reach a
// shell). agent: Prompt (templates allowed), Answer "" (free text), "yesno" or "json". if: Step
// names an earlier step and one test (Exit, Match regex on its output, Equals its answer); Then
// and Else name the step to go to or "stop" ("" is the next step). notify: Title and Message
// (templates allowed). Templates: {{<id>.output}}, {{<id>.answer}}, {{<id>.why}}, {{<id>.exit}}.
type Step struct {
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	Run            string `json:"run,omitempty"`
	Prompt         string `json:"prompt,omitempty"`
	Answer         string `json:"answer,omitempty"`
	TimeoutMinutes int    `json:"timeout_minutes,omitempty"`
	Step           string `json:"step,omitempty"`
	Exit           *int   `json:"exit,omitempty"`
	Match          string `json:"match,omitempty"`
	Equals         string `json:"equals,omitempty"`
	Then           string `json:"then,omitempty"`
	Else           string `json:"else,omitempty"`
	Title          string `json:"title,omitempty"`
	Message        string `json:"message,omitempty"`
}

var (
	nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,47}$`)
	idRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)
	tmplRe = regexp.MustCompile(`\{\{\s*([a-z0-9][a-z0-9_-]*)\.(output|answer|why|exit)\s*\}\}`)
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

// Validate checks the name, schedule, caps and every step.
func (w Workflow) Validate() error {
	if !nameRe.MatchString(w.Name) {
		return fmt.Errorf("workflow: name %q: use a-z, 0-9 and -", w.Name)
	}
	if _, err := ParseCron(w.Schedule); err != nil {
		return fmt.Errorf("workflow %s: %w", w.Name, err)
	}
	if w.MaxMinutes < 0 || w.MaxAgents < 0 || w.MaxCostUSD < 0 {
		return fmt.Errorf("workflow %s: caps can't be negative", w.Name)
	}
	if len(w.Steps) == 0 {
		return fmt.Errorf("workflow %s: no steps", w.Name)
	}
	pos := map[string]int{}
	for i, s := range w.Steps {
		if !idRe.MatchString(s.ID) || s.ID == Stop {
			return fmt.Errorf("workflow %s: step %d: id %q: use a-z, 0-9, _ and -", w.Name, i+1, s.ID)
		}
		if _, dup := pos[s.ID]; dup {
			return fmt.Errorf("workflow %s: step id %q used twice", w.Name, s.ID)
		}
		pos[s.ID] = i
	}
	for i, s := range w.Steps {
		if err := s.validate(pos, i); err != nil {
			return fmt.Errorf("workflow %s: step %s: %w", w.Name, s.ID, err)
		}
	}
	return nil
}

func (s Step) validate(pos map[string]int, i int) error {
	earlier := func(text string) error {
		for _, m := range tmplRe.FindAllStringSubmatch(text, -1) {
			if p, ok := pos[m[1]]; !ok || p >= i {
				return fmt.Errorf("template %s names no earlier step", m[0])
			}
		}
		return nil
	}
	target := func(t string) error {
		if t == "" || t == Stop {
			return nil
		}
		if p, ok := pos[t]; !ok || p <= i {
			return fmt.Errorf("goes to %q, which is not a later step", t)
		}
		return nil
	}
	switch s.Kind {
	case KindRun:
		if strings.TrimSpace(s.Run) == "" {
			return fmt.Errorf("run needs a command")
		}
		if strings.Contains(s.Run, "{{") {
			return fmt.Errorf("run commands take no templates (earlier outputs never reach a shell)")
		}
	case KindAgent:
		if strings.TrimSpace(s.Prompt) == "" {
			return fmt.Errorf("agent needs a prompt")
		}
		if s.Answer != "" && s.Answer != "yesno" && s.Answer != "json" {
			return fmt.Errorf("answer is \"\", yesno or json, not %q", s.Answer)
		}
		return earlier(s.Prompt)
	case KindIf:
		if p, ok := pos[s.Step]; !ok || p >= i {
			return fmt.Errorf("if needs step: an earlier step id")
		}
		tests := 0
		if s.Exit != nil {
			tests++
		}
		if s.Match != "" {
			tests++
			if _, err := regexp.Compile(s.Match); err != nil {
				return fmt.Errorf("match: %v", err)
			}
		}
		if s.Equals != "" {
			tests++
		}
		if tests != 1 {
			return fmt.Errorf("if needs exactly one of exit, match, equals")
		}
		if err := target(s.Then); err != nil {
			return err
		}
		return target(s.Else)
	case KindNotify:
		if strings.TrimSpace(s.Message) == "" {
			return fmt.Errorf("notify needs a message")
		}
		if err := earlier(s.Title); err != nil {
			return err
		}
		return earlier(s.Message)
	default:
		return fmt.Errorf("kind is run, agent, if or notify, not %q", s.Kind)
	}
	if s.TimeoutMinutes < 0 {
		return fmt.Errorf("timeout_minutes can't be negative")
	}
	return nil
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
