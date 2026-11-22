package ghx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ErrNoGitHub is returned by every operation when there is neither gh nor a token.
var ErrNoGitHub = errors.New("needs the GitHub CLI or GH_TOKEN (or GITHUB_TOKEN)")

var (
	lookPath     = exec.LookPath
	baseOverride string
	originURL    = func() string { s, _ := Run("git", "remote", "get-url", "origin"); return s }
	httpClient   = &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	hostRe   = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?(:[0-9]{1,5})?$`)
	pullRe   = regexp.MustCompile(`/([^/\s]+/[^/\s]+)/pull/(\d+)/?$`)
	nameRe   = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	runRe    = regexp.MustCompile(`/([^/\s]+/[^/\s]+)/actions/runs/(\d+)`)
	digitsRe = regexp.MustCompile(`^\d+$`)
)

func token() string {
	if t := strings.TrimSpace(os.Getenv("GH_TOKEN")); t != "" {
		return t
	}
	return strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
}

// TokenVar names the env var the token comes from, or "".
func TokenVar() string {
	for _, k := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if os.Getenv(k) != "" {
			return k
		}
	}
	return ""
}

// Mode is how smithy reaches GitHub: "gh" (the CLI), "token" (the API) or "none".
// A signed-out gh with a token set uses the API.
func Mode() string {
	if _, err := lookPath("gh"); err == nil {
		if token() == "" || exec.Command("gh", "auth", "status").Run() == nil {
			return "gh"
		}
	}
	if token() != "" {
		return "token"
	}
	return "none"
}

// Ready says whether GitHub operations can run: nil, or why not.
func Ready() error {
	switch Mode() {
	case "gh":
		if _, err := Run("gh", "auth", "status"); err != nil {
			return errors.New("gh is signed out: run `gh auth login` or set GH_TOKEN")
		}
		return nil
	case "token":
		_, _, _, err := endpoints()
		return err
	}
	return ErrNoGitHub
}

func host() string {
	if h := os.Getenv("GH_HOST"); h != "" {
		return h
	}
	return "github.com"
}

func endpoints() (rest, gql, web string, err error) {
	if baseOverride != "" {
		return baseOverride, baseOverride + "/graphql", "https://github.com", nil
	}
	h := host()
	if !hostRe.MatchString(h) {
		return "", "", "", fmt.Errorf("GH_HOST %q is not a host name", h)
	}
	if strings.EqualFold(h, "github.com") {
		return "https://api.github.com", "https://api.github.com/graphql", "https://github.com", nil
	}
	return "https://" + h + "/api/v3", "https://" + h + "/api/graphql", "https://" + h, nil
}

// CompareURL is the web page that opens a PR from head into base.
func CompareURL(repo, base, head string) string {
	base, head = refEscape(base), refEscape(head)
	if o, n, err := splitRepo(repo); err == nil {
		repo = o + "/" + n
	} else {
		repo = url.PathEscape(repo)
	}
	web := "https://" + host()
	if !hostRe.MatchString(host()) {
		web = "https://github.com"
	}
	return web + "/" + repo + "/compare/" + base + "..." + head + "?expand=1"
}

func repoFromRemote(remote string) string {
	re := regexp.MustCompile(`^(?:[a-z+]+://)?(?:[^@/]+@)?` + regexp.QuoteMeta(host()) + `(?::\d+)?[:/]([^/\s]+)/([^/\s]+?)(?:\.git)?/?$`)
	if m := re.FindStringSubmatch(strings.TrimSpace(remote)); m != nil && okName(m[1]) && okName(m[2]) {
		return m[1] + "/" + m[2]
	}
	return ""
}

// OriginIsGitHub reports whether the origin remote points at the GitHub host ("" origin: false).
func OriginIsGitHub() (bool, string) {
	u := strings.TrimSpace(originURL())
	return repoFromRemote(u) != "", u
}

func okName(s string) bool { return nameRe.MatchString(s) && s != "." && s != ".." }

func splitRepo(repo string) (string, string, error) {
	o, n, ok := strings.Cut(repo, "/")
	if !ok || !okName(o) || !okName(n) {
		return "", "", fmt.Errorf("not a GitHub repo: %q", repo)
	}
	return url.PathEscape(o), url.PathEscape(n), nil
}

func call(method, path string, in, out any) error {
	rest, _, _, err := endpoints()
	if err != nil {
		return err
	}
	b, err := send(method, rest+path, in)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(b, out)
}

func send(method, u string, in any) ([]byte, error) {
	tok := token()
	if tok == "" {
		return nil, ErrNoGitHub
	}
	var rd io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, u, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "smithy")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, errors.New(redact(fmt.Sprintf("github api: %s %s: %v", method, req.URL.Path, err), tok))
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		var e struct{ Message string }
		_ = json.Unmarshal(b, &e)
		return nil, errors.New(redact(fmt.Sprintf("github api: %s %s: %d %s", method, req.URL.Path, resp.StatusCode, e.Message), tok))
	}
	return b, nil
}

func refEscape(s string) string { return strings.ReplaceAll(url.PathEscape(s), "%2F", "/") }

func redact(s, tok string) string { return strings.ReplaceAll(s, tok, "***") }

func graphql(query string, vars map[string]any) ([]byte, error) {
	_, gql, _, err := endpoints()
	if err != nil {
		return nil, err
	}
	b, err := send("POST", gql, map[string]any{"query": query, "variables": vars})
	if err != nil {
		return nil, err
	}
	var e struct{ Errors []struct{ Message string } }
	if json.Unmarshal(b, &e) == nil && len(e.Errors) > 0 {
		return nil, errors.New(redact("github graphql: "+e.Errors[0].Message, token()))
	}
	return b, nil
}

func via(ghFn, apiFn func() error) error {
	switch Mode() {
	case "gh":
		return ghFn()
	case "token":
		return apiFn()
	}
	return ErrNoGitHub
}

func reshape(src, dst any) error {
	b, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}

// RepoName is owner/name: gh's repo, or the origin remote's (works without a token).
func RepoName() (string, error) {
	if Mode() == "gh" {
		return Run("gh", "repo", "view", "--json", "nameWithOwner", "-q", ".nameWithOwner")
	}
	if r := repoFromRemote(originURL()); r != "" {
		return r, nil
	}
	return "", fmt.Errorf("origin is not a %s remote", host())
}

func repoOf(ref string, re *regexp.Regexp) (string, error) {
	if m := re.FindStringSubmatch(ref); m != nil {
		return m[1], nil
	}
	return RepoName()
}

func prNumber(ref string) (string, int, error) {
	repo, err := repoOf(ref, pullRe)
	if err != nil {
		return "", 0, err
	}
	if _, _, err := splitRepo(repo); err != nil {
		return "", 0, err
	}
	n := 0
	switch {
	case pullRe.MatchString(ref):
		u, err := url.Parse(ref)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || !strings.EqualFold(u.Host, host()) || strings.Contains(u.Path, "..") {
			return "", 0, fmt.Errorf("not a %s pull request URL: %q", host(), ref)
		}
		n, _ = strconv.Atoi(pullRe.FindStringSubmatch(ref)[2])
	case digitsRe.MatchString(strings.TrimPrefix(ref, "#")):
		n, _ = strconv.Atoi(strings.TrimPrefix(ref, "#"))
	case strings.Contains(ref, "://"):
		return "", 0, fmt.Errorf("not a pull request URL: %q", ref)
	}
	if n > 0 {
		return repo, n, nil
	}
	if digitsRe.MatchString(strings.TrimPrefix(ref, "#")) {
		return "", 0, fmt.Errorf("not a pull request number: %q", ref)
	}
	var prs []struct{ Number int }
	if err := apiPRList(repo, ref, "all", &prs); err != nil {
		return "", 0, err
	}
	if len(prs) == 0 {
		return "", 0, fmt.Errorf("no pull requests found for branch %q", ref)
	}
	return repo, prs[0].Number, nil
}

const viewFields = "number,url,state,mergeable,reviewDecision,statusCheckRollup,headRefName,baseRefName,headRefOid,isDraft,headRepositoryOwner,comments,reviews"

const viewQuery = `query($owner:String!,$name:String!,$num:Int!){repository(owner:$owner,name:$name){pullRequest(number:$num){
  url number state mergeable reviewDecision isDraft headRefName baseRefName headRefOid
  headRepositoryOwner{login} comments{totalCount} reviews{totalCount}
  commits(last:1){nodes{commit{statusCheckRollup{contexts(first:100){nodes{
    __typename ... on CheckRun{name status conclusion startedAt completedAt} ... on StatusContext{context state}}}}}}}}}}`

// PRView is `gh pr view <ref> --json <all fields smithy reads>`; ref is a number, URL or branch.
func PRView(ref string, v any) error {
	if strings.HasPrefix(ref, "-") {
		return fmt.Errorf("not a pull request: %q", ref)
	}
	return via(func() error {
		return GH(v, "pr", "view", ref, "--json", viewFields)
	}, func() error {
		repo, num, err := prNumber(ref)
		if err != nil {
			return err
		}
		o, n, _ := strings.Cut(repo, "/")
		b, err := graphql(viewQuery, map[string]any{"owner": o, "name": n, "num": num})
		if err != nil {
			return err
		}
		var r struct {
			Data struct {
				Repository struct {
					PullRequest *struct {
						URL                 string          `json:"url"`
						Number              int             `json:"number"`
						State               string          `json:"state"`
						Mergeable           string          `json:"mergeable"`
						ReviewDecision      *string         `json:"reviewDecision"`
						IsDraft             bool            `json:"isDraft"`
						HeadRefName         string          `json:"headRefName"`
						BaseRefName         string          `json:"baseRefName"`
						HeadRefOid          string          `json:"headRefOid"`
						HeadRepositoryOwner json.RawMessage `json:"headRepositoryOwner"`
						Comments            struct{ TotalCount int }
						Reviews             struct{ TotalCount int }
						Commits             struct {
							Nodes []struct {
								Commit struct {
									StatusCheckRollup *struct {
										Contexts struct{ Nodes []json.RawMessage }
									}
								}
							}
						}
					} `json:"pullRequest"`
				}
			}
		}
		if err := json.Unmarshal(b, &r); err != nil {
			return err
		}
		p := r.Data.Repository.PullRequest
		if p == nil {
			return fmt.Errorf("no pull request %s#%d", repo, num)
		}
		rollup := []json.RawMessage{}
		for _, c := range p.Commits.Nodes {
			if c.Commit.StatusCheckRollup != nil {
				rollup = append(rollup, c.Commit.StatusCheckRollup.Contexts.Nodes...)
			}
		}
		return reshape(map[string]any{
			"url": p.URL, "number": p.Number, "state": p.State, "mergeable": p.Mergeable,
			"reviewDecision": p.ReviewDecision, "isDraft": p.IsDraft, "headRefName": p.HeadRefName,
			"baseRefName": p.BaseRefName, "headRefOid": p.HeadRefOid, "headRepositoryOwner": p.HeadRepositoryOwner,
			"comments": make([]struct{}, p.Comments.TotalCount), "reviews": make([]struct{}, p.Reviews.TotalCount),
			"statusCheckRollup": rollup,
		}, v)
	})
}

// PRList is `gh pr list [--head <branch>] --state <open|all>` with number, url, headRefName,
// author, labels and updatedAt.
func PRList(head, state string, v any) error {
	return via(func() error {
		args := []string{"pr", "list", "--state", state, "--limit", "100", "--json", "number,url,headRefName,author,labels,updatedAt"}
		if head != "" {
			args = append(args, "--head", head)
		}
		return GH(v, args...)
	}, func() error {
		repo, err := RepoName()
		if err != nil {
			return err
		}
		return apiPRList(repo, head, state, v)
	})
}

func apiPRList(repo, head, state string, v any) error {
	o, n, err := splitRepo(repo)
	if err != nil {
		return err
	}
	q := url.Values{"state": {state}, "per_page": {"100"}}
	if head != "" {
		owner, _, _ := strings.Cut(repo, "/")
		q.Set("head", owner+":"+head)
	}
	var raw []struct {
		Number    int    `json:"number"`
		HTMLURL   string `json:"html_url"`
		UpdatedAt string `json:"updated_at"`
		Head      struct{ Ref string }
		User      struct{ Login string }
		Labels    []struct{ Name string }
	}
	if err := call("GET", "/repos/"+o+"/"+n+"/pulls?"+q.Encode(), nil, &raw); err != nil {
		return err
	}
	out := []map[string]any{}
	for _, p := range raw {
		labels := []map[string]string{}
		for _, l := range p.Labels {
			labels = append(labels, map[string]string{"name": l.Name})
		}
		out = append(out, map[string]any{"number": p.Number, "url": p.HTMLURL, "headRefName": p.Head.Ref,
			"author": map[string]string{"login": p.User.Login}, "labels": labels, "updatedAt": p.UpdatedAt})
	}
	return reshape(out, v)
}

// PRCreate opens a PR from head into base and returns its URL.
func PRCreate(base, head, title, bodyFile string, draft bool) (string, error) {
	var u string
	err := via(func() error {
		args := []string{"pr", "create", "--base", base, "--head", head, "--title", title, "--body-file", bodyFile}
		if draft {
			args = append(args, "--draft")
		}
		out, err := Run("gh", args...)
		if lines := strings.Split(out, "\n"); err == nil {
			u = strings.TrimSpace(lines[len(lines)-1])
		}
		return err
	}, func() error {
		body, err := os.ReadFile(bodyFile)
		if err != nil {
			return err
		}
		repo, err := RepoName()
		if err != nil {
			return err
		}
		o, n, err := splitRepo(repo)
		if err != nil {
			return err
		}
		var r struct {
			HTMLURL string `json:"html_url"`
		}
		in := map[string]any{"base": base, "head": head, "title": title, "body": string(body), "draft": draft}
		if err := call("POST", "/repos/"+o+"/"+n+"/pulls", in, &r); err != nil {
			return err
		}
		u = r.HTMLURL
		return nil
	})
	return u, err
}

// PRMerge merges a PR with method merge, rebase or squash.
func PRMerge(ref, method string) error {
	if method != "merge" && method != "rebase" && method != "squash" {
		return fmt.Errorf("merge method must be merge, rebase or squash, not %q", method)
	}
	if strings.HasPrefix(ref, "-") {
		return fmt.Errorf("not a pull request: %q", ref)
	}
	return via(func() error {
		_, err := Run("gh", "pr", "merge", ref, "--"+method)
		return err
	}, func() error {
		repo, num, err := prNumber(ref)
		if err != nil {
			return err
		}
		o, n, err := splitRepo(repo)
		if err != nil {
			return err
		}
		return call("PUT", "/repos/"+o+"/"+n+"/pulls/"+strconv.Itoa(num)+"/merge", map[string]string{"merge_method": method}, nil)
	})
}

type apiRun struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	Status     string  `json:"status"`
	Conclusion *string `json:"conclusion"`
	HeadSha    string  `json:"head_sha"`
	CreatedAt  string  `json:"created_at"`
	HTMLURL    string  `json:"html_url"`
}

func (r apiRun) gh() map[string]any {
	c := ""
	if r.Conclusion != nil {
		c = *r.Conclusion
	}
	return map[string]any{"databaseId": r.ID, "name": r.Name, "status": r.Status, "conclusion": c, "headSha": r.HeadSha, "createdAt": r.CreatedAt, "url": r.HTMLURL}
}

// RunList is `gh run list --branch <b> --limit 30` with databaseId, status, conclusion, name,
// headSha, createdAt and url.
func RunList(branch string, v any) error {
	return via(func() error {
		return GH(v, "run", "list", "--branch", branch, "--limit", "30", "--json", "databaseId,status,conclusion,name,headSha,createdAt,url")
	}, func() error {
		repo, err := RepoName()
		if err != nil {
			return err
		}
		o, n, err := splitRepo(repo)
		if err != nil {
			return err
		}
		var r struct {
			WorkflowRuns []apiRun `json:"workflow_runs"`
		}
		q := url.Values{"branch": {branch}, "per_page": {"30"}}
		if err := call("GET", "/repos/"+o+"/"+n+"/actions/runs?"+q.Encode(), nil, &r); err != nil {
			return err
		}
		out := []map[string]any{}
		for _, run := range r.WorkflowRuns {
			out = append(out, run.gh())
		}
		return reshape(out, v)
	})
}

// RunView is `gh run view <id>` with status, conclusion, name, url and jobs; ref is an id or run URL.
func RunView(ref string, v any) error {
	id := ref
	if m := runRe.FindStringSubmatch(ref); m != nil {
		id = m[2]
	}
	if !digitsRe.MatchString(id) {
		return fmt.Errorf("not a run id or URL: %q", ref)
	}
	return via(func() error {
		args := []string{"run", "view", id, "--json", "status,conclusion,name,url,jobs"}
		if m := runRe.FindStringSubmatch(ref); m != nil {
			args = append(args, "-R", m[1])
		}
		return GH(v, args...)
	}, func() error {
		repo, err := repoOf(ref, runRe)
		if err != nil {
			return err
		}
		o, n, err := splitRepo(repo)
		if err != nil {
			return err
		}
		var run apiRun
		if err := call("GET", "/repos/"+o+"/"+n+"/actions/runs/"+id, nil, &run); err != nil {
			return err
		}
		var jobs struct {
			Jobs []struct {
				Name       string  `json:"name"`
				Conclusion *string `json:"conclusion"`
			}
		}
		if err := call("GET", "/repos/"+o+"/"+n+"/actions/runs/"+id+"/jobs?per_page=100", nil, &jobs); err != nil {
			return err
		}
		out := run.gh()
		js := []map[string]string{}
		for _, j := range jobs.Jobs {
			c := ""
			if j.Conclusion != nil {
				c = *j.Conclusion
			}
			js = append(js, map[string]string{"name": j.Name, "conclusion": c})
		}
		out["jobs"] = js
		return reshape(out, v)
	})
}

// IssueList is `gh issue list --state open --label <l>` with number, url, updatedAt.
func IssueList(label string, v any) error {
	return via(func() error {
		return GH(v, "issue", "list", "--state", "open", "--limit", "100", "--label", label, "--json", "number,url,updatedAt")
	}, func() error {
		repo, err := RepoName()
		if err != nil {
			return err
		}
		o, n, err := splitRepo(repo)
		if err != nil {
			return err
		}
		var raw []struct {
			Number      int             `json:"number"`
			HTMLURL     string          `json:"html_url"`
			UpdatedAt   string          `json:"updated_at"`
			PullRequest json.RawMessage `json:"pull_request"`
		}
		q := url.Values{"state": {"open"}, "labels": {label}, "per_page": {"100"}}
		if err := call("GET", "/repos/"+o+"/"+n+"/issues?"+q.Encode(), nil, &raw); err != nil {
			return err
		}
		out := []map[string]any{}
		for _, i := range raw {
			if len(i.PullRequest) > 0 {
				continue
			}
			out = append(out, map[string]any{"number": i.Number, "url": i.HTMLURL, "updatedAt": i.UpdatedAt})
		}
		return reshape(out, v)
	})
}

// CurrentUser is the signed-in login.
func CurrentUser() (string, error) {
	var login string
	err := via(func() error {
		var err error
		login, err = Run("gh", "api", "user", "-q", ".login")
		return err
	}, func() error {
		var u struct{ Login string }
		err := call("GET", "/user", nil, &u)
		login = u.Login
		return err
	})
	return login, err
}

// Compare is the raw compare API JSON for base...head (head may be owner:branch).
func Compare(repo, base, head string) ([]byte, error) {
	o, n, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}
	for _, r := range []string{base, head} {
		if r == "" || strings.Contains(r, "..") || strings.HasPrefix(r, "-") {
			return nil, fmt.Errorf("not a branch name: %q", r)
		}
	}
	path := "repos/" + o + "/" + n + "/compare/" + refEscape(base) + "..." + refEscape(head)
	var b []byte
	err = via(func() error {
		s, err := Run("gh", "api", path)
		b = []byte(s)
		return err
	}, func() error {
		rest, _, _, err := endpoints()
		if err != nil {
			return err
		}
		b, err = send("GET", rest+"/"+path, nil)
		return err
	})
	return b, err
}

const threadsQuery = `query($owner:String!,$name:String!,$num:Int!){repository(owner:$owner,name:$name){pullRequest(number:$num){
    reviewThreads(first:100){nodes{isResolved}}}}}`

// ReviewThreads is the raw GraphQL JSON of a PR's review threads (isResolved each).
func ReviewThreads(repo string, num int) ([]byte, error) {
	if _, _, err := splitRepo(repo); err != nil {
		return nil, err
	}
	if num <= 0 {
		return nil, fmt.Errorf("not a pull request number: %d", num)
	}
	o, n, _ := strings.Cut(repo, "/")
	var b []byte
	err := via(func() error {
		s, err := Run("gh", "api", "graphql", "-F", "owner="+o, "-F", "name="+n, "-F", "num="+strconv.Itoa(num), "-f", "query="+threadsQuery)
		b = []byte(s)
		return err
	}, func() error {
		var err error
		b, err = graphql(threadsQuery, map[string]any{"owner": o, "name": n, "num": num})
		return err
	})
	return b, err
}

// Alerts lists the repo's open Dependabot alerts: number, severity, package and url. It fails
// when the token or app may not read them.
func Alerts(v any) error {
	repo, err := RepoName()
	if err != nil {
		return err
	}
	o, n, err := splitRepo(repo)
	if err != nil {
		return err
	}
	path := "/repos/" + o + "/" + n + "/dependabot/alerts?state=open&per_page=100"
	var raw []struct {
		Number           int    `json:"number"`
		HTMLURL          string `json:"html_url"`
		SecurityAdvisory struct {
			Severity string `json:"severity"`
		} `json:"security_advisory"`
		Dependency struct {
			Package struct {
				Name string `json:"name"`
			} `json:"package"`
		} `json:"dependency"`
	}
	if err := via(func() error { return GH(&raw, "api", strings.TrimPrefix(path, "/")) },
		func() error { return call("GET", path, nil, &raw) }); err != nil {
		return err
	}
	out := []map[string]any{}
	for _, a := range raw {
		out = append(out, map[string]any{"number": a.Number, "url": a.HTMLURL,
			"severity": a.SecurityAdvisory.Severity, "package": a.Dependency.Package.Name})
	}
	return reshape(out, v)
}
