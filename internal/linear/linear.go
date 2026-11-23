// Package linear is the Linear issue source for smithy auto: a GraphQL client over net/http
// that reads a personal API key from the env var named by linear.key_env and never prints it.
package linear

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Zovaris/smithy/internal/auto"
	"github.com/Zovaris/smithy/internal/config"
)

// Endpoint is the GraphQL URL; tests point it at a fake server.
var Endpoint = "https://api.linear.app/graphql"

// Warn receives the one low rate-limit warning.
var Warn io.Writer = os.Stderr

// lowRemaining is the X-RateLimit-Requests-Remaining level that triggers the warning.
const lowRemaining = 100

func init() {
	auto.Sources["linear"] = func(s auto.Settings) (auto.Source, error) { return Open(s) }
}

// Filters are the linear.* settings.
type Filters struct {
	Teams, Projects, Milestones, Initiatives, States []string
	Cycle                                            string
}

// Source is the Linear source.
type Source struct {
	Settings auto.Settings
	Filters  Filters
	key      string
	client   *http.Client
	warned   sync.Once
}

// Open reads the linear.* settings and the key. A missing team or key is an auto.ErrNoAccess
// error naming the setting or env var, never the key.
func Open(s auto.Settings) (*Source, error) {
	f := Filters{
		Teams:       list(config.Get("linear.team", "")),
		Projects:    list(config.Get("linear.project", "")),
		Milestones:  list(config.Get("linear.milestone", "")),
		Initiatives: list(config.Get("linear.initiative", "")),
		States:      list(config.Get("linear.states", "unstarted,backlog")),
		Cycle:       strings.TrimSpace(config.Get("linear.cycle", "")),
	}
	if len(f.Teams) == 0 {
		return nil, fmt.Errorf("linear: set linear.team (team keys smithy may work in, e.g. ENG): %w", auto.ErrNoAccess)
	}
	if f.Cycle != "" && f.Cycle != "current" {
		if _, err := strconv.Atoi(f.Cycle); err != nil {
			return nil, fmt.Errorf("linear.cycle must be current or a cycle number, not %q", f.Cycle)
		}
	}
	env := config.Get("linear.key_env", "LINEAR_API_KEY")
	key := strings.TrimSpace(os.Getenv(env))
	if key == "" {
		return nil, fmt.Errorf("linear: env var %s is empty (Linear personal API key): %w", env, auto.ErrNoAccess)
	}
	return &Source{Settings: s, Filters: f, key: key, client: &http.Client{Timeout: 30 * time.Second}}, nil
}

func list(s string) []string {
	var out []string
	for _, x := range strings.Split(s, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

// Name is "linear".
func (*Source) Name() string { return "linear" }

const issuesQuery = `query($first: Int!, $after: String, $filter: IssueFilter) {
  issues(first: $first, after: $after, filter: $filter, orderBy: updatedAt) {
    nodes { id identifier title description url state { name type } labels { nodes { name } }
      project { name } projectMilestone { name } team { key } updatedAt }
    pageInfo { hasNextPage endCursor }
  }
}`

// Filter is the IssueFilter for the settings and since (zero: no updatedAt bound).
func (s *Source) Filter(since time.Time) map[string]any {
	in := func(v []string) map[string]any { return map[string]any{"in": v} }
	f := map[string]any{"team": map[string]any{"key": in(s.Filters.Teams)}}
	if len(s.Filters.States) > 0 {
		f["state"] = map[string]any{"type": in(s.Filters.States)}
	}
	project := map[string]any{}
	if len(s.Filters.Projects) > 0 {
		project["name"] = in(s.Filters.Projects)
	}
	if len(s.Filters.Initiatives) > 0 {
		project["initiatives"] = map[string]any{"some": map[string]any{"name": in(s.Filters.Initiatives)}}
	}
	if len(project) > 0 {
		f["project"] = project
	}
	if len(s.Filters.Milestones) > 0 {
		f["projectMilestone"] = map[string]any{"name": in(s.Filters.Milestones)}
	}
	switch c := s.Filters.Cycle; {
	case c == "current":
		f["cycle"] = map[string]any{"isActive": map[string]any{"eq": true}}
	case c != "":
		n, _ := strconv.Atoi(c)
		f["cycle"] = map[string]any{"number": map[string]any{"eq": n}}
	}
	if !since.IsZero() {
		f["updatedAt"] = map[string]any{"gt": since.UTC().Format(time.RFC3339)}
	}
	return f
}

type node struct {
	ID, Identifier, Title, Description, URL string
	Labels                                  struct{ Nodes []struct{ Name string } }
	Team                                    struct{ Key string }
}

// Poll lists the issues passing the filters updated after since, page by page. An issue from a
// team outside linear.team is dropped even if the API returns it.
func (s *Source) Poll(since time.Time) ([]auto.Issue, error) {
	var out []auto.Issue
	after := ""
	for {
		vars := map[string]any{"first": 50, "filter": s.Filter(since)}
		if after != "" {
			vars["after"] = after
		}
		var data struct {
			Issues struct {
				Nodes    []node
				PageInfo struct {
					HasNextPage bool
					EndCursor   string
				}
			}
		}
		if err := s.do(issuesQuery, vars, &data); err != nil {
			return nil, err
		}
		for _, n := range data.Issues.Nodes {
			if !s.allowed(n.Team.Key) {
				continue
			}
			i := auto.Issue{Source: "linear", Key: n.ID, ID: n.Identifier, Title: n.Title, URL: n.URL, Body: n.Description}
			for _, l := range n.Labels.Nodes {
				i.Labels = append(i.Labels, l.Name)
			}
			out = append(out, i)
		}
		if !data.Issues.PageInfo.HasNextPage || data.Issues.PageInfo.EndCursor == "" {
			return out, nil
		}
		after = data.Issues.PageInfo.EndCursor
	}
}

func (s *Source) allowed(team string) bool {
	for _, t := range s.Filters.Teams {
		if strings.EqualFold(t, team) {
			return true
		}
	}
	return false
}

// Report comments on issue id (identifier or UUID), moves it to the team's first started or
// completed workflow state, and attaches the PR url. Issues outside linear.team are refused.
func (s *Source) Report(id, state, pr string) error {
	var is struct {
		Issue struct {
			ID   string
			Team struct{ Key string }
		}
	}
	if err := s.do(`query($id: String!) { issue(id: $id) { id team { key } } }`, map[string]any{"id": id}, &is); err != nil {
		return err
	}
	if is.Issue.ID == "" {
		return fmt.Errorf("linear: issue %s not found", id)
	}
	if !s.allowed(is.Issue.Team.Key) {
		return fmt.Errorf("linear: issue %s is in team %s, not in linear.team", id, is.Issue.Team.Key)
	}
	uid := is.Issue.ID
	if err := s.do(`mutation($input: CommentCreateInput!) { commentCreate(input: $input) { success } }`,
		map[string]any{"input": map[string]any{"issueId": uid, "body": auto.Comment(state, pr)}}, nil); err != nil {
		return err
	}
	if state == "started" || state == "completed" {
		var ws struct {
			WorkflowStates struct {
				Nodes []struct{ ID, Name, Type string }
			}
		}
		if err := s.do(`query($team: String!) { workflowStates(filter: { team: { key: { eq: $team } } }) { nodes { id name type } } }`,
			map[string]any{"team": is.Issue.Team.Key}, &ws); err != nil {
			return err
		}
		sid := ""
		for _, w := range ws.WorkflowStates.Nodes {
			if w.Type == state {
				sid = w.ID
				break
			}
		}
		if sid == "" {
			return fmt.Errorf("linear: team %s has no %s workflow state", is.Issue.Team.Key, state)
		}
		if err := s.do(`mutation($id: String!, $input: IssueUpdateInput!) { issueUpdate(id: $id, input: $input) { success } }`,
			map[string]any{"id": uid, "input": map[string]any{"stateId": sid}}, nil); err != nil {
			return err
		}
	}
	if pr != "" {
		if err := s.do(`mutation($issueId: String!, $url: String!, $title: String) { attachmentLinkURL(issueId: $issueId, url: $url, title: $title) { success } }`,
			map[string]any{"issueId": uid, "url": pr, "title": "Pull request"}, nil); err != nil {
			return err
		}
	}
	return nil
}

// do posts one GraphQL request and decodes data into out (nil: ignored). Errors never carry
// the key.
func (s *Source) do(query string, vars map[string]any, out any) error {
	body, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	req, err := http.NewRequest("POST", Endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("linear: %v", err)
	}
	req.Header.Set("Authorization", s.key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("linear: request failed: %s", strings.ReplaceAll(err.Error(), s.key, "<key>"))
	}
	defer resp.Body.Close()
	if r, err := strconv.Atoi(resp.Header.Get("X-RateLimit-Requests-Remaining")); err == nil && r < lowRemaining {
		s.warned.Do(func() {
			fmt.Fprintf(Warn, "linear: rate limit low: %d requests left in this window\n", r)
		})
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	var res struct {
		Data   json.RawMessage
		Errors []struct{ Message string }
	}
	_ = json.Unmarshal(raw, &res)
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("linear: key rejected (HTTP %d): %w", resp.StatusCode, auto.ErrNoAccess)
	}
	if len(res.Errors) > 0 {
		var m []string
		for _, e := range res.Errors {
			m = append(m, strings.ReplaceAll(e.Message, s.key, "<key>"))
		}
		return fmt.Errorf("linear: %s", strings.Join(m, "; "))
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("linear: HTTP %d", resp.StatusCode)
	}
	if out != nil {
		if err := json.Unmarshal(res.Data, out); err != nil {
			return fmt.Errorf("linear: bad response: %v", err)
		}
	}
	return nil
}
