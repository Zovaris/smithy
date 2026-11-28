//go:build harness

package daemon

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/ghx"
	"github.com/Zovaris/smithy/internal/linear"
)

func TestLinksTakeAnyURLMentionsAndRemoval(t *testing.T) {
	dir := gitRepo(t, "git@github.com:acme/app.git")
	sv, base, _ := sessionTest(t, func(ctx context.Context, msg string, h []Exchange, e Emit) (string, error) {
		return "opened https://github.com/acme/app/pull/9 and filed https://github.com/acme/app/issues/3", nil
	})
	f := &fakeLinks{
		prs: map[string]ghx.PRLink{
			"https://github.com/acme/app/pull/8": {URL: "https://github.com/acme/app/pull/8", Number: 8, State: "OPEN"},
			"https://github.com/acme/app/pull/9": {URL: "https://github.com/acme/app/pull/9", Number: 9, State: "OPEN"},
		},
		issues: map[int]ghx.IssueLink{3: {Number: 3, URL: "https://github.com/acme/app/issues/3", State: "OPEN"}},
		lin:    map[string]linear.Issue{"TK-112": {ID: "TK-112", URL: "https://linear.app/acme/issue/TK-112"}, "TK-7": {ID: "TK-7", URL: "https://linear.app/acme/issue/TK-7/x"}},
	}
	sv.Links = f.linker()
	_, m := post(t, base+"/v1/sessions", `{"agent":"claude","dir":"`+dir+`","message":"go"}`)
	id := m["id"].(string)
	waitStatus(t, sv, id, StatusDone)

	l, _ := sv.refreshLinks(id)
	if len(l.PRs) != 2 || l.PRs[0].Number != 9 || l.PRs[1].Number != 8 {
		t.Fatalf("the agent's PR and the branch PR: %+v", l.PRs)
	}
	if issueIDs(l) != "TK-112 #3" {
		t.Fatalf("the branch's Linear id and the mentioned issue: %+v", l.Issues)
	}

	for _, ref := range []string{"https://docs.example.com/guide/setup", "https://linear.app/acme/issue/TK-7/x"} {
		if code, m := post(t, base+"/v1/sessions/"+id+"/links", `{"ref":"`+ref+`"}`); code != 200 {
			t.Fatalf("%s: %d %v", ref, code, m)
		}
	}
	l, _ = sv.SessionLinksOf(id)
	if len(l.URLs) != 1 || l.URLs[0].Host != "docs.example.com" || l.URLs[0].Title != "docs.example.com/guide/setup" {
		t.Fatalf("a generic URL shows its host: %+v", l.URLs)
	}
	if issueIDs(l) != "TK-112 TK-7 #3" {
		t.Fatalf("a Linear URL links its issue: %+v", l.Issues)
	}
	for _, bad := range []string{`{"ref":"javascript:alert(1)"}`, `{"ref":"ftp://x/y"}`, `{"ref":"https://"}`} {
		if code, _ := post(t, base+"/v1/sessions/"+id+"/links", bad); code != 400 {
			t.Fatalf("%s: %d, want 400", bad, code)
		}
	}

	for _, ref := range []string{"https://github.com/acme/app/pull/8", "TK-112", "https://docs.example.com/guide/setup"} {
		res := do(t, "DELETE", base+"/v1/sessions/"+id+"/links?ref="+url.QueryEscape(ref), "", auth)
		res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatalf("remove %s: %d", ref, res.StatusCode)
		}
	}
	l, _ = sv.SessionLinksOf(id)
	if len(l.PRs) != 1 || l.PRs[0].Number != 9 || len(l.URLs) != 0 || issueIDs(l) != "TK-7 #3" {
		t.Fatalf("removed links stay gone, even found ones: %+v", l)
	}
	again, err := New(sv.st, testToken, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	again.Links = f.linker()
	if l, _ := again.refreshLinks(id); len(l.PRs) != 1 || len(l.Issues) != 2 {
		t.Fatalf("removal survives a restart: %+v", l)
	}

	c := &Client{Base: base, Token: testToken}
	if l, err := c.RemoveLink(context.Background(), id, "https://github.com/acme/app/pull/9"); err != nil || len(l.PRs) != 0 {
		t.Fatalf("client remove %+v %v", l, err)
	}
}

func issueIDs(l SessionLinks) string {
	var out []string
	for _, i := range l.Issues {
		out = append(out, i.ID)
	}
	return strings.Join(out, " ")
}
