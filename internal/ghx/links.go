package ghx

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// ErrNotFound is a linked issue or PR GitHub does not have (or that is the other kind).
var ErrNotFound = errors.New("not found")

// PRLink is what a session's links show of one PR. Checks: green, red, pending or "".
type PRLink struct {
	URL       string `json:"url"`
	Number    int    `json:"number"`
	Title     string `json:"title"`
	Body      string `json:"-"`
	State     string `json:"state"`
	Draft     bool   `json:"draft,omitempty"`
	Mergeable string `json:"mergeable,omitempty"`
	Review    string `json:"review,omitempty"`
	Checks    string `json:"checks,omitempty"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Head      string `json:"head,omitempty"`
}

// IssueLink is what a session's links show of one GitHub issue.
type IssueLink struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	State     string `json:"state"`
	Assignee  string `json:"assignee,omitempty"`
	Milestone string `json:"milestone,omitempty"`
}

const linkPRQuery = `query($owner:String!,$name:String!,$num:Int!){repository(owner:$owner,name:$name){pullRequest(number:$num){
  url number title body state isDraft mergeable reviewDecision additions deletions headRefName
  commits(last:1){nodes{commit{statusCheckRollup{state}}}}}}}`

const linkIssueQuery = `query($owner:String!,$name:String!,$num:Int!){repository(owner:$owner,name:$name){issueOrPullRequest(number:$num){
  __typename ... on Issue{number title url state assignees(first:1){nodes{login}} milestone{title}}}}}`

const branchPRQuery = `query($owner:String!,$name:String!,$head:String!){repository(owner:$owner,name:$name){
  pullRequests(headRefName:$head,first:1,orderBy:{field:UPDATED_AT,direction:DESC}){nodes{url}}}}`

// gql runs one GraphQL query through gh (gh api graphql) or the token API.
func gql(query string, strs map[string]string, ints map[string]int) ([]byte, error) {
	var b []byte
	err := via(func() error {
		args := []string{"api", "graphql", "-f", "query=" + query}
		for k, v := range strs {
			args = append(args, "-f", k+"="+v)
		}
		for k, v := range ints {
			args = append(args, "-F", k+"="+strconv.Itoa(v))
		}
		out, err := Run("gh", args...)
		b = []byte(out)
		return err
	}, func() error {
		vars := map[string]any{}
		for k, v := range strs {
			vars[k] = v
		}
		for k, v := range ints {
			vars[k] = v
		}
		var err error
		b, err = graphql(query, vars)
		return err
	})
	return b, err
}

func rollupChecks(s string) string {
	switch s {
	case "SUCCESS":
		return "green"
	case "FAILURE", "ERROR":
		return "red"
	case "PENDING", "EXPECTED":
		return "pending"
	}
	return ""
}

// LinkPR reads one PR by its URL.
func LinkPR(u string) (PRLink, error) {
	m := pullRe.FindStringSubmatch(u)
	if m == nil {
		return PRLink{}, fmt.Errorf("not a pull request URL: %q", u)
	}
	o, n, err := splitRepo(m[1])
	if err != nil {
		return PRLink{}, err
	}
	num, _ := strconv.Atoi(m[2])
	b, err := gql(linkPRQuery, map[string]string{"owner": o, "name": n}, map[string]int{"num": num})
	if err != nil {
		return PRLink{}, err
	}
	var r struct {
		Data struct {
			Repository struct {
				PullRequest *struct {
					URL, Title, Body, State, Mergeable, HeadRefName string
					Number, Additions, Deletions                    int
					IsDraft                                         bool
					ReviewDecision                                  *string
					Commits                                         struct {
						Nodes []struct {
							Commit struct{ StatusCheckRollup *struct{ State string } }
						}
					}
				}
			}
		}
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return PRLink{}, err
	}
	p := r.Data.Repository.PullRequest
	if p == nil {
		return PRLink{}, ErrNotFound
	}
	out := PRLink{URL: p.URL, Number: p.Number, Title: p.Title, Body: p.Body, State: p.State, Draft: p.IsDraft,
		Mergeable: p.Mergeable, Additions: p.Additions, Deletions: p.Deletions, Head: p.HeadRefName}
	if p.ReviewDecision != nil {
		out.Review = *p.ReviewDecision
	}
	for _, c := range p.Commits.Nodes {
		if c.Commit.StatusCheckRollup != nil {
			out.Checks = rollupChecks(c.Commit.StatusCheckRollup.State)
		}
	}
	return out, nil
}

// LinkIssue reads issue num of repo (owner/name); a PR number is ErrNotFound.
func LinkIssue(repo string, num int) (IssueLink, error) {
	o, n, err := splitRepo(repo)
	if err != nil {
		return IssueLink{}, err
	}
	b, err := gql(linkIssueQuery, map[string]string{"owner": o, "name": n}, map[string]int{"num": num})
	if err != nil {
		return IssueLink{}, err
	}
	var r struct {
		Data struct {
			Repository struct {
				IssueOrPullRequest *struct {
					Typename          string `json:"__typename"`
					Number            int
					Title, URL, State string
					Assignees         struct{ Nodes []struct{ Login string } }
					Milestone         *struct{ Title string }
				}
			}
		}
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return IssueLink{}, err
	}
	i := r.Data.Repository.IssueOrPullRequest
	if i == nil || i.Typename != "Issue" {
		return IssueLink{}, ErrNotFound
	}
	out := IssueLink{Number: i.Number, Title: i.Title, URL: i.URL, State: i.State}
	if len(i.Assignees.Nodes) > 0 {
		out.Assignee = i.Assignees.Nodes[0].Login
	}
	if i.Milestone != nil {
		out.Milestone = i.Milestone.Title
	}
	return out, nil
}

// BranchPR is the URL of the latest PR whose head is branch in repo, "" when there is none.
func BranchPR(repo, branch string) (string, error) {
	o, n, err := splitRepo(repo)
	if err != nil || branch == "" {
		return "", err
	}
	b, err := gql(branchPRQuery, map[string]string{"owner": o, "name": n, "head": branch}, nil)
	if err != nil {
		return "", err
	}
	var r struct {
		Data struct {
			Repository struct {
				PullRequests struct{ Nodes []struct{ URL string } }
			}
		}
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return "", err
	}
	if ns := r.Data.Repository.PullRequests.Nodes; len(ns) > 0 {
		return ns[0].URL, nil
	}
	return "", nil
}
