package ghx

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

const testToken = "ghp_secretTOKEN123"

func noGH(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_HOST", "")
}

func apiMode(t *testing.T, h http.HandlerFunc) {
	t.Helper()
	noGH(t)
	t.Setenv("GH_TOKEN", testToken)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+testToken {
			t.Errorf("auth header = %q", got)
		}
		if strings.Contains(r.URL.String(), testToken) {
			t.Errorf("token in URL %s", r.URL)
		}
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	oldBase, oldOrigin := baseOverride, originURL
	baseOverride = srv.URL
	originURL = func() string { return "git@github.com:o/r.git" }
	t.Cleanup(func() { baseOverride, originURL = oldBase, oldOrigin })
}

func body(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	b, _ := io.ReadAll(r.Body)
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("body %q: %v", b, err)
	}
	return m
}

func TestMode(t *testing.T) {
	noGH(t)
	if m := Mode(); m != "none" {
		t.Fatalf("no gh, no token: %q", m)
	}
	t.Setenv("GITHUB_TOKEN", "x")
	if m := Mode(); m != "token" {
		t.Fatalf("GITHUB_TOKEN: %q", m)
	}
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "x")
	if m := Mode(); m != "token" {
		t.Fatalf("GH_TOKEN: %q", m)
	}
	t.Setenv("GH_TOKEN", "")
	fakeGH(t, `exit 0`)
	if m := Mode(); m != "gh" {
		t.Fatalf("gh: %q", m)
	}
}

func TestModeSignedOutGHWithToken(t *testing.T) {
	noGH(t)
	fakeGH(t, `exit 1`)
	t.Setenv("GH_TOKEN", "x")
	if m := Mode(); m != "token" {
		t.Fatalf("signed-out gh with a token should use the API: %q", m)
	}
}

func TestNoneMode(t *testing.T) {
	noGH(t)
	old := originURL
	originURL = func() string { return "https://github.com/o/r.git" }
	t.Cleanup(func() { originURL = old })
	var v any
	errs := map[string]error{
		"view":    PRView("1", &v),
		"list":    PRList("b", "open", &v),
		"merge":   PRMerge("1", "merge"),
		"runs":    RunList("b", &v),
		"run":     RunView("5", &v),
		"issues":  IssueList("agent", &v),
		"ready":   Ready(),
		"threads": func() error { _, err := ReviewThreads("o/r", 1); return err }(),
		"compare": func() error { _, err := Compare("o/r", "main", "b"); return err }(),
		"user":    func() error { _, err := CurrentUser(); return err }(),
		"create":  func() error { _, err := PRCreate("main", "b", "t", "/dev/null", false); return err }(),
	}
	for k, err := range errs {
		if !errors.Is(err, ErrNoGitHub) || !strings.Contains(err.Error(), "needs the GitHub CLI or GH_TOKEN") {
			t.Errorf("%s: err = %v", k, err)
		}
	}
	if r, err := RepoName(); err != nil || r != "o/r" {
		t.Fatalf("RepoName works offline: %q %v", r, err)
	}
}

func TestCompareURL(t *testing.T) {
	noGH(t)
	if got := CompareURL("o/r", "main", "feat/x"); got != "https://github.com/o/r/compare/main...feat/x?expand=1" {
		t.Fatalf("got %q", got)
	}
	t.Setenv("GH_HOST", "ghe.example.com")
	if got := CompareURL("o/r", "main", "b"); got != "https://ghe.example.com/o/r/compare/main...b?expand=1" {
		t.Fatalf("GH_HOST: got %q", got)
	}
}

func TestEndpoints(t *testing.T) {
	noGH(t)
	for _, tc := range []struct{ host, rest, gql string }{
		{"", "https://api.github.com", "https://api.github.com/graphql"},
		{"github.com", "https://api.github.com", "https://api.github.com/graphql"},
		{"ghe.example.com", "https://ghe.example.com/api/v3", "https://ghe.example.com/api/graphql"},
	} {
		t.Setenv("GH_HOST", tc.host)
		rest, gql, _, err := endpoints()
		if err != nil || rest != tc.rest || gql != tc.gql {
			t.Errorf("%q: %s %s %v", tc.host, rest, gql, err)
		}
	}
	for _, bad := range []string{"evil.com/x", "user@evil.com", "http://evil.com", "a b"} {
		t.Setenv("GH_HOST", bad)
		if _, _, _, err := endpoints(); err == nil {
			t.Errorf("GH_HOST %q accepted", bad)
		}
	}
}

func TestOriginRepoParse(t *testing.T) {
	noGH(t)
	for in, want := range map[string]string{
		"git@github.com:o/r.git":      "o/r",
		"https://github.com/o/r":      "o/r",
		"https://github.com/o/r.git/": "o/r",
		"ssh://git@github.com/o/r":    "o/r",
		"https://gitlab.com/o/r":      "",
	} {
		if got := repoFromRemote(in); got != want {
			t.Errorf("%s: %q", in, got)
		}
	}
}

const viewJSON = `{"data":{"repository":{"pullRequest":{"url":"https://github.com/o/r/pull/7","number":7,
"state":"OPEN","mergeable":"MERGEABLE","reviewDecision":null,"isDraft":false,"headRefName":"feat/x",
"baseRefName":"main","headRefOid":"abc1234567","headRepositoryOwner":{"login":"o"},
"comments":{"totalCount":2},"reviews":{"totalCount":1},
"commits":{"nodes":[{"commit":{"statusCheckRollup":{"contexts":{"nodes":[
{"__typename":"CheckRun","name":"test","status":"COMPLETED","conclusion":"SUCCESS"},
{"__typename":"StatusContext","context":"ci/x","state":"PENDING"}]}}}}]}}}}}`

func TestPRViewAPI(t *testing.T) {
	apiMode(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/graphql" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		b := body(t, r)
		vars := b["variables"].(map[string]any)
		if vars["owner"] != "o" || vars["name"] != "r" || vars["num"] != float64(7) {
			t.Errorf("vars %v", vars)
		}
		io.WriteString(w, viewJSON)
	})
	var v struct {
		URL, State, Mergeable, ReviewDecision, HeadRefName, BaseRefName, HeadRefOid string
		Number                                                                      int
		IsDraft                                                                     bool
		HeadRepositoryOwner                                                         struct{ Login string }
		Comments, Reviews                                                           []json.RawMessage
		StatusCheckRollup                                                           []struct{ Name, Context, Status, Conclusion, State string }
	}
	for _, ref := range []string{"7", "https://github.com/o/r/pull/7"} {
		if err := PRView(ref, &v); err != nil {
			t.Fatal(err)
		}
		if v.Number != 7 || v.State != "OPEN" || v.Mergeable != "MERGEABLE" || v.HeadRefOid != "abc1234567" ||
			v.HeadRepositoryOwner.Login != "o" || len(v.Comments) != 2 || len(v.Reviews) != 1 || v.BaseRefName != "main" {
			t.Fatalf("got %+v", v)
		}
		if len(v.StatusCheckRollup) != 2 || v.StatusCheckRollup[0].Conclusion != "SUCCESS" || v.StatusCheckRollup[1].Context != "ci/x" || v.StatusCheckRollup[1].State != "PENDING" {
			t.Fatalf("rollup %+v", v.StatusCheckRollup)
		}
	}
}

func TestPRViewByBranchAPI(t *testing.T) {
	apiMode(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/pulls":
			if r.URL.Query().Get("head") != "o:feat/x" || r.URL.Query().Get("state") != "all" {
				t.Errorf("query %v", r.URL.RawQuery)
			}
			io.WriteString(w, `[{"number":7,"html_url":"u","head":{"ref":"feat/x"},"user":{"login":"me"},"labels":[],"updated_at":"t","state":"open"}]`)
		case "/graphql":
			io.WriteString(w, viewJSON)
		default:
			t.Errorf("path %s", r.URL.Path)
		}
	})
	var v struct{ Number int }
	if err := PRView("feat/x", &v); err != nil || v.Number != 7 {
		t.Fatalf("%+v %v", v, err)
	}
}

func TestPRListAPI(t *testing.T) {
	apiMode(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/pulls" || r.URL.Query().Get("state") != "open" || r.URL.Query().Get("head") != "o:b" {
			t.Errorf("%s %s", r.URL.Path, r.URL.RawQuery)
		}
		io.WriteString(w, `[{"number":3,"html_url":"https://github.com/o/r/pull/3","head":{"ref":"b"},"user":{"login":"me"},"labels":[{"name":"agent"}],"updated_at":"2026-01-01T00:00:00Z"}]`)
	})
	var prs []struct {
		Number           int
		URL, HeadRefName string
		UpdatedAt        string
		Author           struct{ Login string }
		Labels           []struct{ Name string }
	}
	if err := PRList("b", "open", &prs); err != nil {
		t.Fatal(err)
	}
	if len(prs) != 1 || prs[0].Number != 3 || prs[0].URL != "https://github.com/o/r/pull/3" || prs[0].HeadRefName != "b" ||
		prs[0].Author.Login != "me" || prs[0].Labels[0].Name != "agent" || prs[0].UpdatedAt == "" {
		t.Fatalf("got %+v", prs)
	}
}

func TestPRCreateAPI(t *testing.T) {
	f := t.TempDir() + "/body.md"
	os.WriteFile(f, []byte("the body"), 0o644)
	apiMode(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/repos/o/r/pulls" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		b := body(t, r)
		if b["base"] != "main" || b["head"] != "feat/x" || b["title"] != "t" || b["body"] != "the body" || b["draft"] != true {
			t.Errorf("body %v", b)
		}
		w.WriteHeader(201)
		io.WriteString(w, `{"html_url":"https://github.com/o/r/pull/9"}`)
	})
	u, err := PRCreate("main", "feat/x", "t", f, true)
	if err != nil || u != "https://github.com/o/r/pull/9" {
		t.Fatalf("%q %v", u, err)
	}
}

func TestPRMergeAPI(t *testing.T) {
	apiMode(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" || r.URL.Path != "/repos/o/r/pulls/7/merge" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		if b := body(t, r); b["merge_method"] != "rebase" {
			t.Errorf("body %v", b)
		}
		io.WriteString(w, `{"merged":true}`)
	})
	if err := PRMerge("https://github.com/o/r/pull/7", "rebase"); err != nil {
		t.Fatal(err)
	}
	if err := PRMerge("7", "octopus"); err == nil {
		t.Fatal("want error for unknown method")
	}
}

func TestRunsAPI(t *testing.T) {
	apiMode(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/actions/runs":
			if r.URL.Query().Get("branch") != "b" {
				t.Errorf("query %s", r.URL.RawQuery)
			}
			io.WriteString(w, `{"workflow_runs":[{"name":"ci","status":"completed","conclusion":"success","head_sha":"abc","created_at":"t1"},{"name":"lint","status":"in_progress","conclusion":null,"head_sha":"abc","created_at":"t2"}]}`)
		case "/repos/o/r/actions/runs/55":
			io.WriteString(w, `{"name":"ci","status":"completed","conclusion":"failure","html_url":"https://github.com/o/r/actions/runs/55"}`)
		case "/repos/o/r/actions/runs/55/jobs":
			io.WriteString(w, `{"jobs":[{"name":"unit","conclusion":"failure"}]}`)
		default:
			t.Errorf("path %s", r.URL.Path)
		}
	})
	var runs []struct{ Status, Conclusion, Name, HeadSha, CreatedAt string }
	if err := RunList("b", &runs); err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || runs[0].HeadSha != "abc" || runs[0].Conclusion != "success" || runs[1].Conclusion != "" || runs[1].CreatedAt != "t2" {
		t.Fatalf("got %+v", runs)
	}
	var run struct {
		Status, Conclusion, Name, URL string
		Jobs                          []struct{ Name, Conclusion string }
	}
	if err := RunView("https://github.com/o/r/actions/runs/55", &run); err != nil {
		t.Fatal(err)
	}
	if run.Conclusion != "failure" || run.URL == "" || len(run.Jobs) != 1 || run.Jobs[0].Name != "unit" {
		t.Fatalf("got %+v", run)
	}
}

func TestIssuesUserCompareThreadsAPI(t *testing.T) {
	apiMode(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/issues":
			if r.URL.Query().Get("labels") != "agent" {
				t.Errorf("query %s", r.URL.RawQuery)
			}
			io.WriteString(w, `[{"number":1,"html_url":"u1","updated_at":"t"},{"number":2,"html_url":"u2","updated_at":"t","pull_request":{}}]`)
		case "/user":
			io.WriteString(w, `{"login":"me"}`)
		case "/repos/o/r/compare/main...o:feat/x":
			io.WriteString(w, `{"behind_by":3}`)
		case "/graphql":
			io.WriteString(w, `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[{"isResolved":false}]}}}}}`)
		default:
			t.Errorf("path %s", r.URL.Path)
		}
	})
	var issues []struct {
		Number    int
		URL       string
		UpdatedAt string
	}
	if err := IssueList("agent", &issues); err != nil || len(issues) != 1 || issues[0].URL != "u1" {
		t.Fatalf("issues %+v %v", issues, err)
	}
	if u, err := CurrentUser(); err != nil || u != "me" {
		t.Fatalf("user %q %v", u, err)
	}
	if b, err := Compare("o/r", "main", "o:feat/x"); err != nil || !strings.Contains(string(b), `"behind_by":3`) {
		t.Fatalf("compare %s %v", b, err)
	}
	if b, err := ReviewThreads("o/r", 7); err != nil || !strings.Contains(string(b), "isResolved") {
		t.Fatalf("threads %s %v", b, err)
	}
	if r, err := RepoName(); err != nil || r != "o/r" {
		t.Fatalf("repo %q %v", r, err)
	}
}

func TestAPIErrorsHideToken(t *testing.T) {
	apiMode(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		io.WriteString(w, `{"message":"Bad credentials `+testToken+`"}`)
	})
	_, err := CurrentUser()
	if err == nil || strings.Contains(err.Error(), testToken) || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v", err)
	}
	apiMode(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"errors":[{"message":"no such PR"}]}`)
	})
	if _, err := ReviewThreads("o/r", 1); err == nil || !strings.Contains(err.Error(), "no such PR") {
		t.Fatalf("graphql err = %v", err)
	}
}

func TestRedirectToOtherHostDropsToken(t *testing.T) {
	var leaked string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("Authorization")
		io.WriteString(w, `{"login":"x"}`)
	}))
	defer other.Close()
	noGH(t)
	t.Setenv("GH_TOKEN", testToken)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, strings.Replace(other.URL, "127.0.0.1", "localhost", 1)+"/user", http.StatusFound)
	}))
	defer srv.Close()
	old := baseOverride
	baseOverride = srv.URL
	defer func() { baseOverride = old }()
	CurrentUser()
	if leaked != "" {
		t.Fatalf("token followed a redirect to another host: %q", leaked)
	}
}

func TestGHModeUsesCLI(t *testing.T) {
	noGH(t)
	log := t.TempDir() + "/args"
	fakeGH(t, `echo "$*" >> `+log+`; case "$1 $2" in "pr create") echo https://github.com/o/r/pull/1;; "api user") echo me;; "repo view") echo o/r;; *) echo '{}';; esac`)
	var v any
	if err := PRView("7", &v); err != nil {
		t.Fatal(err)
	}
	if err := PRMerge("7", "squash"); err != nil {
		t.Fatal(err)
	}
	if u, err := PRCreate("main", "b", "t", "/dev/null", false); err != nil || u != "https://github.com/o/r/pull/1" {
		t.Fatalf("%q %v", u, err)
	}
	if u, _ := CurrentUser(); u != "me" {
		t.Fatalf("user %q", u)
	}
	if r, _ := RepoName(); r != "o/r" {
		t.Fatalf("repo %q", r)
	}
	b, _ := os.ReadFile(log)
	for _, want := range []string{"pr view 7 --json", "pr merge 7 --squash", "pr create --base main --head b --title t --body-file /dev/null"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("missing %q in\n%s", want, b)
		}
	}
}

func TestDashRefRejected(t *testing.T) {
	noGH(t)
	fakeGH(t, `echo ran >&2; exit 9`)
	var v any
	if err := PRView("--web", &v); err == nil || !strings.Contains(err.Error(), "not a pull request") {
		t.Fatalf("view: %v", err)
	}
	if err := PRMerge("--admin", "merge"); err == nil || !strings.Contains(err.Error(), "not a pull request") {
		t.Fatalf("merge: %v", err)
	}
}

func TestOriginIsGitHub(t *testing.T) {
	old := originURL
	t.Cleanup(func() { originURL = old })
	for u, want := range map[string]bool{"git@github.com:o/r.git": true, "/tmp/bare.git": false, "https://gitlab.com/o/r.git": false} {
		originURL = func() string { return u }
		if got, _ := OriginIsGitHub(); got != want {
			t.Errorf("%s → %v", u, got)
		}
	}
}
