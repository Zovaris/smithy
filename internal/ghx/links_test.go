package ghx

import (
	"net/http"
	"strings"
	"testing"
)

func TestLinkPRReadsStateChecksReviewLines(t *testing.T) {
	apiMode(t, func(w http.ResponseWriter, r *http.Request) {
		b := body(t, r)
		v := b["variables"].(map[string]any)
		if v["owner"] != "o" || v["name"] != "r" || v["num"] != float64(7) {
			t.Errorf("vars %v", v)
		}
		w.Write([]byte(`{"data":{"repository":{"pullRequest":{"url":"https://github.com/o/r/pull/7","number":7,"title":"Add links","body":"Closes #12","state":"OPEN","isDraft":false,"mergeable":"MERGEABLE","reviewDecision":"APPROVED","additions":120,"deletions":8,"headRefName":"feat/tk-112","commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"FAILURE"}}}]}}}}}`))
	})
	got, err := LinkPR("https://github.com/o/r/pull/7")
	if err != nil {
		t.Fatal(err)
	}
	want := PRLink{URL: "https://github.com/o/r/pull/7", Number: 7, Title: "Add links", Body: "Closes #12", State: "OPEN", Mergeable: "MERGEABLE", Review: "APPROVED", Checks: "red", Additions: 120, Deletions: 8, Head: "feat/tk-112"}
	if got != want {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestLinkPRChecksMapping(t *testing.T) {
	for in, want := range map[string]string{"SUCCESS": "green", "FAILURE": "red", "ERROR": "red", "PENDING": "pending", "EXPECTED": "pending", "": ""} {
		if got := rollupChecks(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}

func TestLinkIssueAndBranchPR(t *testing.T) {
	apiMode(t, func(w http.ResponseWriter, r *http.Request) {
		q := body(t, r)["query"].(string)
		switch {
		case strings.Contains(q, "issueOrPullRequest"):
			w.Write([]byte(`{"data":{"repository":{"issueOrPullRequest":{"__typename":"Issue","number":12,"title":"Bug","url":"https://github.com/o/r/issues/12","state":"OPEN","assignees":{"nodes":[{"login":"ana"}]},"milestone":{"title":"v1"}}}}}`))
		case strings.Contains(q, "pullRequests"):
			w.Write([]byte(`{"data":{"repository":{"pullRequests":{"nodes":[{"url":"https://github.com/o/r/pull/9"}]}}}}`))
		default:
			t.Errorf("query %s", q)
		}
	})
	is, err := LinkIssue("o/r", 12)
	if err != nil {
		t.Fatal(err)
	}
	if is != (IssueLink{Number: 12, Title: "Bug", URL: "https://github.com/o/r/issues/12", State: "OPEN", Assignee: "ana", Milestone: "v1"}) {
		t.Fatalf("issue %+v", is)
	}
	u, err := BranchPR("o/r", "feat/x")
	if err != nil || u != "https://github.com/o/r/pull/9" {
		t.Fatalf("branch pr %q %v", u, err)
	}
	if _, err := LinkIssue("bad repo", 1); err == nil {
		t.Fatal("bad repo accepted")
	}
}

func TestLinkIssueThatIsAPRIsNotFound(t *testing.T) {
	apiMode(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":{"repository":{"issueOrPullRequest":{"__typename":"PullRequest"}}}}`))
	})
	if _, err := LinkIssue("o/r", 3); err != ErrNotFound {
		t.Fatalf("got %v", err)
	}
}
