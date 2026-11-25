package ghx

import (
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func strictAPI(t *testing.T, allowed map[string]string) *int32 {
	var hits int32
	apiMode(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		resp, ok := allowed[r.URL.EscapedPath()]
		if !ok {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
			w.WriteHeader(404)
			io.WriteString(w, `{"message":"Not Found"}`)
			return
		}
		io.WriteString(w, resp)
	})
	return &hits
}

func TestEdgeRunViewRefTraversal(t *testing.T) {
	strictAPI(t, map[string]string{})
	var v any
	for _, ref := range []string{"../../user", "55/../../../user", "55?x=1", "55#f", "-1", ""} {
		if err := RunView(ref, &v); err == nil {
			t.Errorf("RunView(%q) accepted", ref)
		}
	}
}

func TestEdgePRMergeRefInjection(t *testing.T) {
	strictAPI(t, map[string]string{})
	for _, ref := range []string{"https://evil.com/o/r/pull/7/../../x", "0"} {
		if err := PRMerge(ref, "merge"); err == nil {
			t.Errorf("PRMerge(%q) accepted", ref)
		}
	}
}

func TestEdgePRListBranchSpecialChars(t *testing.T) {
	var got string
	apiMode(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query().Get("head")
		if r.URL.Query().Get("state") != "open" {
			t.Errorf("state overridden: %s", r.URL.RawQuery)
		}
		if r.URL.Path != "/repos/o/r/pulls" {
			t.Errorf("path %s", r.URL.Path)
		}
		io.WriteString(w, `[]`)
	})
	var v []any
	br := "feat/a&state=closed#x y+z"
	if err := PRList(br, "open", &v); err != nil {
		t.Fatal(err)
	}
	if got != "o:"+br {
		t.Fatalf("head = %q", got)
	}
}

func TestEdgeRunListBranchSpecialChars(t *testing.T) {
	var got string
	apiMode(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query().Get("branch")
		io.WriteString(w, `{"workflow_runs":[]}`)
	})
	var v []any
	if err := RunList("fix/a&per_page=1", &v); err != nil {
		t.Fatal(err)
	}
	if got != "fix/a&per_page=1" {
		t.Fatalf("branch = %q", got)
	}
}

func TestEdgeCompareTraversal(t *testing.T) {
	strictAPI(t, map[string]string{"/repos/o/r/compare/main...b%3Fx=1": `{}`})
	for _, c := range [][3]string{{"o/r", "../../../user", "b"}, {"o/r/../../x", "main", "b"}, {"o/r", "main", "b?x=1"}, {"o", "main", "b"}} {
		Compare(c[0], c[1], c[2])
	}
}

func TestEdgeReviewThreadsBadRepo(t *testing.T) {
	strictAPI(t, map[string]string{})
	for _, r := range []string{"", "o", "o/r/x", "/r", "o/"} {
		if _, err := ReviewThreads(r, 1); err == nil {
			t.Errorf("ReviewThreads(%q) accepted", r)
		}
	}
}

func TestEdgePRViewNoPRForBranch(t *testing.T) {
	strictAPI(t, map[string]string{"/repos/o/r/pulls": `[]`})
	var v any
	if err := PRView("no/such", &v); err == nil {
		t.Fatal("no PR for branch should error")
	}
}

func TestEdgePRViewNullFields(t *testing.T) {
	strictAPI(t, map[string]string{"/graphql": `{"data":{"repository":{"pullRequest":{"url":"u","number":7,
"state":"MERGED","mergeable":"UNKNOWN","reviewDecision":null,"isDraft":false,"headRefName":"b","baseRefName":"main",
"headRefOid":"a","headRepositoryOwner":null,"comments":{"totalCount":0},"reviews":{"totalCount":0},
"commits":{"nodes":[{"commit":{"statusCheckRollup":null}}]}}}}}`})
	var v map[string]any
	if err := PRView("7", &v); err != nil {
		t.Fatal(err)
	}
	if rd, ok := v["reviewDecision"]; ok && rd != nil && rd != "" {
		t.Errorf("reviewDecision %v", rd)
	}
	if r, ok := v["statusCheckRollup"].([]any); !ok || len(r) != 0 {
		t.Errorf("statusCheckRollup should be [] like gh, got %#v", v["statusCheckRollup"])
	}
	if c, ok := v["comments"].([]any); !ok || len(c) != 0 {
		t.Errorf("comments should be [] like gh, got %#v", v["comments"])
	}
}

func TestEdgePRViewMissingPR(t *testing.T) {
	strictAPI(t, map[string]string{"/graphql": `{"data":{"repository":{"pullRequest":null}},"errors":[{"type":"NOT_FOUND","message":"Could not resolve to a PullRequest"}]}`})
	var v map[string]any
	if err := PRView("999", &v); err == nil {
		t.Fatal("missing PR should error")
	}
}

func TestEdgeServerErrorBodyHidesToken(t *testing.T) {
	for _, code := range []int{500, 403, 422} {
		apiMode(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
			io.WriteString(w, `<html>Authorization: Bearer `+testToken+`</html>`)
		})
		if _, err := CurrentUser(); err == nil || strings.Contains(err.Error(), testToken) {
			t.Errorf("%d: err = %v", code, err)
		}
		var v any
		if err := PRList("b", "open", &v); err == nil || strings.Contains(err.Error(), testToken) {
			t.Errorf("%d list: err = %v", code, err)
		}
	}
}

func TestEdgeTokenWithNewline(t *testing.T) {
	apiMode(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Evil") != "" {
			t.Errorf("header injection")
		}
		io.WriteString(w, `{"login":"me"}`)
	})
	t.Setenv("GH_TOKEN", testToken+"\r\nX-Evil: 1")
	if _, err := CurrentUser(); err != nil && strings.Contains(err.Error(), testToken) {
		t.Fatalf("err leaks token: %v", err)
	}
}

func TestEdgeTokenTrailingWhitespace(t *testing.T) {
	apiMode(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"login":"me"}`) })
	t.Setenv("GH_TOKEN", testToken+"\n")
	if u, err := CurrentUser(); err != nil || u != "me" {
		t.Fatalf("%q %v", u, err)
	}
}

func TestEdgeGHTokenPrecedence(t *testing.T) {
	apiMode(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"login":"me"}`) })
	t.Setenv("GITHUB_TOKEN", "other")
	if _, err := CurrentUser(); err != nil {
		t.Fatal(err)
	}
}

func TestEdgeEndpointsHostEdge(t *testing.T) {
	noGH(t)
	for host, want := range map[string]string{"GitHub.com": "https://api.github.com", "github.com.": "https://api.github.com", " github.com ": "https://api.github.com"} { //nolint:gocritic // mapKey: the padded host is the case under test
		t.Setenv("GH_HOST", host)
		rest, _, _, err := endpoints()
		if err == nil && rest != want {
			t.Errorf("%q -> %s", host, rest)
		}
	}
	for _, bad := range []string{"evil.com?x=", "evil.com#", "evil.com\\x", "evil.com:443/x", "%65vil.com", "evil.com\n", "evil.com@github.com"} {
		t.Setenv("GH_HOST", bad)
		if _, _, _, err := endpoints(); err == nil {
			t.Errorf("GH_HOST %q accepted", bad)
		}
	}
}

func TestEdgeBadGHHostNoRequest(t *testing.T) {
	noGH(t)
	t.Setenv("GH_TOKEN", testToken)
	t.Setenv("GH_HOST", "evil.com/x")
	old := originURL
	originURL = func() string { return "git@github.com:o/r.git" }
	t.Cleanup(func() { originURL = old })
	_, err := CurrentUser()
	if err == nil || strings.Contains(err.Error(), testToken) {
		t.Fatalf("err = %v", err)
	}
	if m := Mode(); m == "token" {
		if Ready() == nil {
			t.Errorf("Ready ok with invalid GH_HOST")
		}
	}
}

func TestEdgeRemoteParseEdge(t *testing.T) {
	noGH(t)
	for in, want := range map[string]string{
		"git@github.com:o.x/r.y.git":           "o.x/r.y",
		"https://user:pw@github.com/o/r.git":   "o/r",
		"https://github.com/o/r/extra":         "",
		"git@github.com:o/r.git\n":             "o/r",
		"https://github.com.evil.com/o/r":      "",
		"https://evil.com/github.com/o/r":      "",
		"git@github.com:../r.git":              "",
		"https://github.com/o/r%2F..%2F..%2Fx": "",
	} {
		if got := repoFromRemote(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
	t.Setenv("GH_HOST", "ghe.example.com")
	if got := repoFromRemote("git@ghe.example.com:o/r.git"); got != "o/r" {
		t.Errorf("GHE remote: %q", got)
	}
}

func TestEdgeCompareURLSpecialBranch(t *testing.T) {
	noGH(t)
	got := CompareURL("o/r", "main", "feat/a#b?c")
	if strings.Contains(got, "#b") || strings.Contains(got, "?c") {
		t.Errorf("unescaped branch in URL: %q", got)
	}
}

func TestEdgeNoneModeTokenVar(t *testing.T) {
	noGH(t)
	if TokenVar() != "" {
		t.Errorf("TokenVar with none = %q", TokenVar())
	}
	t.Setenv("GITHUB_TOKEN", "x")
	if TokenVar() != "GITHUB_TOKEN" {
		t.Errorf("TokenVar = %q", TokenVar())
	}
}
