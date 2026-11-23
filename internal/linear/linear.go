// Package linear is the Linear issue source for smithy auto: a GraphQL client over net/http
// with the personal API key from auth.Get("linear") (env var linear.key_env, else auth.json),
// never printed.
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

	"github.com/Zovaris/smithy/internal/auth"
	"github.com/Zovaris/smithy/internal/auto"
	"github.com/Zovaris/smithy/internal/config"
)

// Endpoint is the GraphQL URL; tests point it at a fake server.
var Endpoint = "https://api.linear.app/graphql"

// Warn receives the low rate-limit and page-cap warnings.
var Warn io.Writer = os.Stderr

// warnOnce limits the rate-limit warning to one per process.
var warnOnce = &sync.Once{}

var now = time.Now

const (
	// lowRemaining is the X-RateLimit-Requests-Remaining level that triggers the warning.
	lowRemaining = 100
	// maxPages caps one poll at maxPages*50 issues.
	maxPages = 40
	// firstWindow bounds the very first poll (no last poll yet) to recently updated issues.
	firstWindow = 7 * 24 * time.Hour
	// overlap re-reads the minutes before the last poll; seen keys dedupe.
	overlap = 2 * time.Minute
)

// stateTypes are Linear's workflow state types.
var stateTypes = []string{"triage", "backlog", "unstarted", "started", "completed", "canceled"}

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
	Settings     auto.Settings
	Filters      Filters
	key          string
	client       *http.Client
	remaining    int
	hasRemaining bool
}

// Open reads the linear.* settings and the key. A missing team or key is an auto.ErrNoAccess
// error naming what to set, never the key.
func Open(s auto.Settings) (*Source, error) {
	f := Filters{
		Teams:       list(strings.ToUpper(config.Get("linear.team", ""))),
		Projects:    list(config.Get("linear.project", "")),
		Milestones:  list(config.Get("linear.milestone", "")),
		Initiatives: list(config.Get("linear.initiative", "")),
		States:      list(config.Get("linear.states", "")),
		Cycle:       strings.TrimSpace(config.Get("linear.cycle", "")),
	}
	if len(f.Teams) == 0 {
		return nil, fmt.Errorf("linear: set linear.team (team keys smithy may work in, e.g. ENG): %w", auto.ErrNoAccess)
	}
	if len(f.States) == 0 {
		f.States = []string{"unstarted", "backlog"}
	}
	for _, st := range f.States {
		ok := false
		for _, x := range stateTypes {
			ok = ok || x == st
		}
		if !ok {
			return nil, fmt.Errorf("linear.states: unknown state type %q (known: %s)", st, strings.Join(stateTypes, ", "))
		}
	}
	if f.Cycle != "" && f.Cycle != "current" {
		if _, err := strconv.Atoi(f.Cycle); err != nil {
			return nil, fmt.Errorf("linear.cycle must be current or a cycle number, not %q", f.Cycle)
		}
	}
	key, _, err := auth.Get("linear")
	if err != nil {
		return nil, fmt.Errorf("linear: %s: %w", auto.Clean(err.Error()), auto.ErrNoAccess)
	}
	if key == "" {
		return nil, fmt.Errorf("linear: no API key: run smithy auth set linear (key on stdin) or set %s: %w", auth.EnvVar("linear"), auto.ErrNoAccess)
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

// Filter is the IssueFilter for the settings and since, less a short overlap. With no since
// (the first poll) only issues updated in the last 7 days are read.
// Remaining is the rate-limit headroom the last request reported (auto.Rater).
func (s *Source) Remaining() (int, bool) { return s.remaining, s.hasRemaining }

func (s *Source) Filter(since time.Time) map[string]any {
	in := func(v []string) map[string]any { return map[string]any{"in": v} }
	f := map[string]any{"team": map[string]any{"key": in(s.Filters.Teams)}}
	f["state"] = map[string]any{"type": in(s.Filters.States)}
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
	if since.IsZero() {
		since = now().Add(-firstWindow)
	} else {
		since = since.Add(-overlap)
	}
	f["updatedAt"] = map[string]any{"gt": since.UTC().Format(time.RFC3339)}
	return f
}

type node struct {
	ID, Identifier, Title, Description, URL string
	Labels                                  struct{ Nodes []struct{ Name string } }
	Team                                    struct{ Key string }
}

// Poll lists the issues passing the filters updated after since, page by page (at most
// maxPages, stopping when the cursor does not advance). An issue from a team outside
// linear.team is dropped even if the API returns it.
func (s *Source) Poll(since time.Time) ([]auto.Issue, error) {
	var out []auto.Issue
	after := ""
	for page := 1; ; page++ {
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
		next := data.Issues.PageInfo.EndCursor
		if !data.Issues.PageInfo.HasNextPage || next == "" || next == after {
			return out, nil
		}
		if page >= maxPages {
			fmt.Fprintf(Warn, "linear: stopped after %d pages; the rest are read on the next poll\n", maxPages)
			return out, nil
		}
		after = next
	}
}

func (s *Source) allowed(team string) bool {
	team = strings.ToUpper(team)
	for _, t := range s.Filters.Teams {
		if t == team {
			return true
		}
	}
	return false
}

// Report moves issue id (identifier or UUID) to the team's started or completed workflow
// state with the lowest position, attaches the PR url, then comments; the comment goes last so
// a retry after a failed move or attachment does not post it twice. Issues outside linear.team
// are refused.
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
	if state == "started" || state == "completed" {
		var ws struct {
			WorkflowStates struct {
				Nodes []struct {
					ID, Name, Type string
					Position       float64
				}
			}
		}
		if err := s.do(`query($team: String!) { workflowStates(filter: { team: { key: { eq: $team } } }) { nodes { id name type position } } }`,
			map[string]any{"team": is.Issue.Team.Key}, &ws); err != nil {
			return err
		}
		sid, best := "", 0.0
		for _, w := range ws.WorkflowStates.Nodes {
			if w.Type == state && (sid == "" || w.Position < best) {
				sid, best = w.ID, w.Position
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
	return s.do(`mutation($input: CommentCreateInput!) { commentCreate(input: $input) { success } }`,
		map[string]any{"input": map[string]any{"issueId": uid, "body": auto.Comment(state, pr)}}, nil)
}

// do posts one GraphQL request and decodes data into out (nil: ignored). Errors never carry
// the key, and API text in them is cleaned of escape sequences.
func (s *Source) do(query string, vars map[string]any, out any) error {
	body, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	req, err := http.NewRequest("POST", Endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("linear: %s", auto.Clean(err.Error()))
	}
	req.Header.Set("Authorization", s.key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("linear: request failed: %s", auto.Clean(strings.ReplaceAll(err.Error(), s.key, "<key>")))
	}
	defer resp.Body.Close()
	r, rerr := strconv.Atoi(resp.Header.Get("X-RateLimit-Requests-Remaining"))
	if rerr == nil {
		s.remaining, s.hasRemaining = r, true
	}
	if rerr == nil && r < lowRemaining {
		warnOnce.Do(func() {
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
			m = append(m, auto.Clean(strings.ReplaceAll(e.Message, s.key, "<key>")))
		}
		return fmt.Errorf("linear: %s", strings.Join(m, "; "))
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("linear: HTTP %d", resp.StatusCode)
	}
	if out != nil {
		if err := json.Unmarshal(res.Data, out); err != nil {
			return fmt.Errorf("linear: bad response: %s", auto.Clean(err.Error()))
		}
	}
	return nil
}
