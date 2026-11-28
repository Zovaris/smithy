package linear

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/auth"
	"github.com/Zovaris/smithy/internal/auto"
)

// ErrNotFound is an issue id Linear does not know.
var ErrNotFound = errors.New("linear: issue not found")

// IDRe is a Linear issue identifier: a team key, a dash and a number (TK-123).
var IDRe = regexp.MustCompile(`^[A-Z][A-Z0-9]{0,9}-[0-9]{1,7}$`)

// Issue is what a session's links show of one Linear issue.
type Issue struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	Status    string `json:"status"`
	StateType string `json:"state_type,omitempty"`
	Assignee  string `json:"assignee,omitempty"`
	Milestone string `json:"milestone,omitempty"`
}

const lookupQuery = `query($id:String!){issue(id:$id){identifier title url state{name type} assignee{name} projectMilestone{name}}}`

// Lookup reads one issue by its identifier with the stored Linear key (never an MCP connector).
// It needs no linear.* settings: a link names the issue itself.
func Lookup(id string) (Issue, error) {
	if !IDRe.MatchString(id) {
		return Issue{}, fmt.Errorf("linear: %q is not an issue id", id)
	}
	key, _, err := auth.Get("linear")
	if err != nil {
		return Issue{}, fmt.Errorf("linear: %s: %w", auto.Clean(err.Error()), auto.ErrNoAccess)
	}
	if key == "" {
		return Issue{}, fmt.Errorf("linear: no API key: run smithy auth set linear: %w", auto.ErrNoAccess)
	}
	s := &Source{key: key, client: &http.Client{Timeout: 20 * time.Second}}
	var r struct {
		Issue *struct {
			Identifier, Title, URL string
			State                  struct{ Name, Type string }
			Assignee               *struct{ Name string }
			ProjectMilestone       *struct{ Name string }
		}
	}
	if err := s.do(lookupQuery, map[string]any{"id": id}, &r); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "entity not found") {
			return Issue{}, ErrNotFound
		}
		return Issue{}, err
	}
	if r.Issue == nil {
		return Issue{}, ErrNotFound
	}
	out := Issue{ID: r.Issue.Identifier, Title: r.Issue.Title, URL: r.Issue.URL, Status: r.Issue.State.Name, StateType: r.Issue.State.Type}
	if r.Issue.Assignee != nil {
		out.Assignee = r.Issue.Assignee.Name
	}
	if r.Issue.ProjectMilestone != nil {
		out.Milestone = r.Issue.ProjectMilestone.Name
	}
	return out, nil
}
