//go:build harness

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/ghx"
	"github.com/Zovaris/smithy/internal/linear"
)

func TestLinkRefsFindLinearAndGitHubIssues(t *testing.T) {
	lin, gh := linkRefs("feat/tk-112-live-pr", []string{"Fix TK-7 and SHA-256, see #12; Closes #34", "no a#5 or x/#6 &#7"}, []string{"ENG-3", "#9"}, nil)
	if want := []string{"TK-112", "ENG-3", "TK-7", "SHA-256"}; !slices.Equal(lin, want) {
		t.Fatalf("linear %v, want %v", lin, want)
	}
	if want := []int{9, 12, 34}; !slices.Equal(gh, want) {
		t.Fatalf("github %v, want %v", gh, want)
	}
	lin, _ = linkRefs("feat/tk-112", []string{"ENG-4 SHA-256"}, nil, []string{"ENG"})
	if want := []string{"ENG-4"}; !slices.Equal(lin, want) {
		t.Fatalf("team filter: %v, want %v", lin, want)
	}
}

type fakeLinks struct {
	mu     sync.Mutex
	prs    map[string]ghx.PRLink
	issues map[int]ghx.IssueLink
	lin    map[string]linear.Issue
	calls  int
}

func (f *fakeLinks) linker() *Linker {
	return &Linker{
		PR: func(u string) (ghx.PRLink, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.calls++
			p, ok := f.prs[u]
			if !ok {
				return p, ghx.ErrNotFound
			}
			return p, nil
		},
		GHIssue: func(repo string, n int) (ghx.IssueLink, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			if repo != "acme/app" {
				return ghx.IssueLink{}, errors.New("wrong repo " + repo)
			}
			i, ok := f.issues[n]
			if !ok {
				return i, ghx.ErrNotFound
			}
			return i, nil
		},
		Linear: func(id string) (linear.Issue, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			i, ok := f.lin[id]
			if !ok {
				return i, linear.ErrNotFound
			}
			return i, nil
		},
		BranchPR: func(repo, branch string) (string, error) {
			if branch == "feat/tk-112-live" {
				return "https://github.com/acme/app/pull/8", nil
			}
			return "", nil
		},
		Branch: func(string) string { return "feat/tk-112-live" },
		Base:   time.Minute,
		Max:    8 * time.Minute,
	}
}

func TestSessionLinksLiveFromPRsBranchPromptAndLink(t *testing.T) {
	dir := gitRepo(t, "git@github.com:acme/app.git")
	sv, url, _ := sessionTest(t, func(ctx context.Context, msg string, h []Exchange, e Emit) (string, error) {
		e.Tool(ToolEvent{Name: "Bash", Args: json.RawMessage(`{"command":"gh pr create"}`), Output: "https://github.com/acme/app/pull/7"})
		return "ok", nil
	})
	f := &fakeLinks{
		prs: map[string]ghx.PRLink{
			"https://github.com/acme/app/pull/7": {URL: "https://github.com/acme/app/pull/7", Number: 7, Title: "Live links", Body: "Closes #12", State: "OPEN", Checks: "pending", Additions: 10, Deletions: 2},
			"https://github.com/acme/app/pull/8": {URL: "https://github.com/acme/app/pull/8", Number: 8, Title: "Branch PR", State: "OPEN"},
		},
		issues: map[int]ghx.IssueLink{12: {Number: 12, Title: "Bug", URL: "https://github.com/acme/app/issues/12", State: "OPEN", Assignee: "ana"}},
		lin:    map[string]linear.Issue{"TK-112": {ID: "TK-112", Title: "Live", Status: "In Progress", Assignee: "Ana", Milestone: "v0.26", URL: "https://linear.app/i/TK-112"}},
	}
	sv.Links = f.linker()
	code, m := post(t, url+"/v1/sessions", `{"agent":"claude","dir":"`+dir+`","message":"build it, see ENG-9"}`)
	if code != 201 {
		t.Fatalf("start: %d %v", code, m)
	}
	id := m["id"].(string)
	waitStatus(t, sv, id, StatusDone)

	now := time.Unix(1000, 0)
	sv.Links.Now = func() time.Time { return now }
	seq := sv.lastSeqNow()
	sv.Links.Tick(sv)
	var got SessionLinks
	res := do(t, "GET", url+"/v1/sessions/"+id+"/links", "", auth)
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if err := json.Unmarshal(b, &got); err != nil || res.StatusCode != 200 {
		t.Fatalf("links %d %s", res.StatusCode, b)
	}
	if len(got.PRs) != 2 || got.PRs[0].Number != 7 || got.PRs[0].Additions != 10 || got.PRs[1].Number != 8 {
		t.Fatalf("prs %+v", got.PRs)
	}
	if len(got.Issues) != 2 || got.Issues[0].ID != "TK-112" || got.Issues[0].Milestone != "v0.26" || got.Issues[1].ID != "#12" || got.Issues[1].Assignee != "ana" {
		t.Fatalf("issues %+v (ENG-9 is unknown to Linear and dropped)", got.Issues)
	}
	if n := countKind(t, sv, seq, KindSessionLinks); n != 1 {
		t.Fatalf("%d links events pushed, want 1", n)
	}

	calls := f.calls
	sv.Links.Tick(sv)
	if f.calls != calls {
		t.Fatal("polled again before the backoff ran out")
	}
	now = now.Add(time.Minute)
	seq = sv.lastSeqNow()
	sv.Links.Tick(sv)
	if f.calls == calls || countKind(t, sv, seq, KindSessionLinks) != 0 {
		t.Fatal("an unchanged poll must fetch but push nothing")
	}
	if e := sv.Links.every(id); e != 2*time.Minute {
		t.Fatalf("backoff %v after an unchanged poll, want 2m", e)
	}

	f.mu.Lock()
	p := f.prs["https://github.com/acme/app/pull/7"]
	p.Checks = "green"
	f.prs[p.URL] = p
	f.issues[40] = ghx.IssueLink{Number: 40, Title: "Linked", URL: "u40", State: "OPEN"}
	f.mu.Unlock()
	code, m = post(t, url+"/v1/sessions/"+id+"/links", `{"ref":"#40"}`)
	if code != 200 {
		t.Fatalf("link: %d %v", code, m)
	}
	if issues := m["issues"].([]any); len(issues) != 3 {
		t.Fatalf("after /link: %v", m)
	}
	if e := sv.Links.every(id); e != time.Minute {
		t.Fatalf("a change resets the backoff, got %v", e)
	}
	for _, bad := range []string{`{"ref":"rm -rf"}`, `{"ref":"https://evil.com/x"}`, `{"ref":""}`} {
		if code, _ := post(t, url+"/v1/sessions/"+id+"/links", bad); code != 400 {
			t.Fatalf("%s: %d, want 400", bad, code)
		}
	}

	again, err := New(sv.st, testToken, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	l, ok := again.SessionLinksOf(id)
	if !ok || len(l.Issues) != 3 || l.PRs[0].Checks != "green" {
		t.Fatalf("after a restart the last links are replayed: %+v", l)
	}
	if s := again.Sessions(); len(s) != 1 || !slices.Contains(s[0].Links, "#40") {
		t.Fatalf("the /link ref survives a restart: %+v", s)
	}
}

func TestProjectWorkListsOpenPRsAndIssuesOnce(t *testing.T) {
	dir := gitRepo(t, "git@github.com:acme/app.git")
	sv, url, _ := sessionTest(t, func(ctx context.Context, msg string, h []Exchange, e Emit) (string, error) { return "ok", nil })
	f := &fakeLinks{
		prs: map[string]ghx.PRLink{"https://github.com/acme/app/pull/8": {URL: "https://github.com/acme/app/pull/8", Number: 8, State: "OPEN"}},
		lin: map[string]linear.Issue{"TK-112": {ID: "TK-112", Status: "Todo"}},
	}
	sv.Links = f.linker()
	for range 2 {
		_, m := post(t, url+"/v1/sessions", `{"agent":"claude","dir":"`+dir+`","message":"x"}`)
		waitStatus(t, sv, m["id"].(string), StatusDone)
	}
	sv.Links.Tick(sv)
	res := do(t, "GET", url+"/v1/links?dir="+dir, "", auth)
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	var w ProjectWork
	if err := json.Unmarshal(b, &w); err != nil {
		t.Fatalf("%s", b)
	}
	if len(w.PRs) != 1 || len(w.Issues) != 1 || len(w.PRs[0].Sessions) != 2 {
		t.Fatalf("work %s", b)
	}
	res = do(t, "GET", url+"/v1/links", "", auth)
	b, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if !strings.Contains(string(b), "pull/8") {
		t.Fatalf("no dir lists every project's work: %s", b)
	}
	res = do(t, "GET", url+"/v1/links?dir="+t.TempDir(), "", auth)
	b, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if strings.Contains(string(b), "pull/8") {
		t.Fatalf("other project sees this one's work: %s", b)
	}
}

func countKind(t *testing.T, sv *Server, after int64, kind string) int {
	t.Helper()
	evs, err := sv.st.Since(after)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range evs {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

func (sv *Server) lastSeqNow() int64 {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	return sv.lastSeq
}

func TestClientLinks(t *testing.T) {
	dir := gitRepo(t, "git@github.com:acme/app.git")
	sv, url, _ := sessionTest(t, func(ctx context.Context, msg string, h []Exchange, e Emit) (string, error) { return "ok", nil })
	f := &fakeLinks{lin: map[string]linear.Issue{"TK-112": {ID: "TK-112"}, "TK-5": {ID: "TK-5"}}}
	sv.Links = f.linker()
	_, m := post(t, url+"/v1/sessions", `{"agent":"claude","dir":"`+dir+`","message":"x"}`)
	id := m["id"].(string)
	waitStatus(t, sv, id, StatusDone)
	c := &Client{Base: url, Token: testToken}
	l, err := c.SessionLinks(context.Background(), id)
	if err != nil || len(l.Issues) != 1 {
		t.Fatalf("links %+v %v", l, err)
	}
	l, err = c.AddLink(context.Background(), id, "tk-5")
	if err != nil || len(l.Issues) != 2 || l.Issues[1].ID != "TK-5" {
		t.Fatalf("add %+v %v", l, err)
	}
}
