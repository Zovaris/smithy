package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
)

// GitHub events a schedule can start on, polled with gh in the schedule's dir.
const (
	EventDependabot = "dependabot_alert"
	EventPRRed      = "pr_red"
	EventIssue      = "issue_labeled"
)

// Schedule attaches one trigger to a workflow: a cron line, a webhook, a GitHub event or a file
// change. Inputs are passed to the workflow (a webhook body, the event and the changed paths
// add theirs). Dir is where GitHub events are read (the workflow's dir when empty).
type Schedule struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Workflow    string            `json:"workflow"`
	Cron        string            `json:"cron,omitempty"`
	Webhook     bool              `json:"webhook,omitempty"`
	GitHub      *GitHubTrigger    `json:"github,omitempty"`
	Watch       string            `json:"watch,omitempty"`
	Dir         string            `json:"dir,omitempty"`
	Inputs      map[string]string `json:"inputs,omitempty"`
}

// GitHubTrigger is a GitHub event: a new Dependabot alert, a PR whose checks turned red, or a
// new issue with Label.
type GitHubTrigger struct {
	Event string `json:"event"`
	Label string `json:"label,omitempty"`
}

// Trigger names the schedule's trigger in a few words.
func (s Schedule) Trigger() string {
	switch {
	case s.Cron != "":
		return s.Cron
	case s.Webhook:
		return "webhook"
	case s.GitHub != nil:
		t := "github " + s.GitHub.Event
		if s.GitHub.Label != "" {
			t += " " + s.GitHub.Label
		}
		return t
	}
	return "watch " + s.Watch
}

// ParseSchedule decodes and validates a schedule file; unknown fields are an error.
func ParseSchedule(b []byte) (Schedule, error) {
	var s Schedule
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if err := d.Decode(&s); err != nil {
		return s, fmt.Errorf("schedule: %w", err)
	}
	return s, s.Validate()
}

// Validate checks the names and that exactly one trigger is set.
func (s Schedule) Validate() error {
	if !nameRe.MatchString(s.Name) {
		return fmt.Errorf("schedule: name %q: use a-z, 0-9 and -", s.Name)
	}
	if !nameRe.MatchString(s.Workflow) {
		return fmt.Errorf("schedule %s: workflow %q is not a workflow name", s.Name, s.Workflow)
	}
	n := 0
	for _, set := range []bool{s.Cron != "", s.Webhook, s.GitHub != nil, s.Watch != ""} {
		if set {
			n++
		}
	}
	if n != 1 {
		return fmt.Errorf("schedule %s: needs exactly one of cron, webhook, github, watch", s.Name)
	}
	if s.Cron != "" {
		if _, err := ParseCron(s.Cron); err != nil {
			return fmt.Errorf("schedule %s: %w", s.Name, err)
		}
	}
	if g := s.GitHub; g != nil {
		switch g.Event {
		case EventDependabot, EventPRRed:
		case EventIssue:
			if g.Label == "" {
				return fmt.Errorf("schedule %s: %s needs a label", s.Name, EventIssue)
			}
		default:
			return fmt.Errorf("schedule %s: github event is %s, %s or %s", s.Name, EventDependabot, EventPRRed, EventIssue)
		}
	}
	for k := range s.Inputs {
		if !varRe.MatchString(k) {
			return fmt.Errorf("schedule %s: input %q: use letters, digits and _", s.Name, k)
		}
	}
	return nil
}
