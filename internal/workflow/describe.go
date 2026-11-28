package workflow

import (
	"fmt"
	"strings"
)

// String is a condition in one line, as list views show it.
func (c *Cond) String() string {
	if c == nil {
		return ""
	}
	subj := c.Value
	if subj == "" {
		subj = c.Step
	}
	switch {
	case c.Exit != nil:
		return fmt.Sprintf("%s exit == %d", c.Step, *c.Exit)
	case c.Match != "":
		return subj + " ~ /" + c.Match + "/"
	case c.Equals != "":
		return subj + " == " + c.Equals
	}
	op := c.Op
	if op == "" {
		op = "eq"
	}
	return strings.TrimSpace(fmt.Sprintf("%s.%s %s %s", subj, c.Path, op, c.To))
}

// Summary is a step in one line: what it runs, asks or tests.
func (s Step) Summary() string {
	var out string
	switch s.Kind {
	case KindRun:
		out = s.Run
	case KindAgent:
		out = s.Prompt
		if s.Answer != "" {
			out = "(" + s.Answer + ") " + out
		}
	case KindIf:
		out = fmt.Sprintf("%s → %s, else %s", s.If, orNext(s.Then), orNext(s.Else))
	case KindSwitch:
		var cs []string
		for _, c := range s.Cases {
			cs = append(cs, c.Equals+c.Match+" → "+c.Goto)
		}
		out = s.Value + ": " + strings.Join(cs, "; ") + "; else " + orNext(s.Default)
	case KindSet:
		out = strings.Join(sortedKeys(s.Vars), ", ")
	case KindJSON:
		out = s.Path + " of " + s.From
	case KindForeach:
		out = fmt.Sprintf("%s: %d steps", s.Items, len(s.Do))
		if s.Parallel > 1 {
			out += fmt.Sprintf(", %d at a time", s.Parallel)
		}
	case KindAsk:
		out = s.Question
	case KindNotify:
		out = s.Title + ": " + s.Message
	case KindCall:
		out = s.Workflow
	}
	if s.When != nil {
		out += " (when " + s.When.String() + ")"
	}
	return clipText(out, 80)
}

func orNext(s string) string {
	if s == "" {
		return "next"
	}
	return s
}

// Line is a step result in one line: its error, branch, answer or output.
func (r StepResult) Line() string {
	switch {
	case r.Status == StatusSkipped:
		return "skipped"
	case r.Error != "":
		return "error: " + clipText(r.Error, 100)
	case r.Branch != "":
		return "→ " + r.Branch
	case r.Answer != "":
		return clipText(r.Answer+" "+r.Why, 100)
	case r.Kind == KindRun:
		return fmt.Sprintf("exit %d · %s", r.Exit, clipText(r.Output, 90))
	}
	return clipText(r.Output, 100)
}
