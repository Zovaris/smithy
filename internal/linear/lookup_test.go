package linear

import (
	"strings"
	"testing"
)

func TestLookupReadsStatusAssigneeMilestoneWithTheStoredKey(t *testing.T) {
	setup(t, nil)
	calls := fake(t, `{"data":{"issue":{"identifier":"TK-112","title":"Live links","url":"https://linear.app/x/issue/TK-112","state":{"name":"In Progress","type":"started"},"assignee":{"name":"Ana"},"projectMilestone":{"name":"v0.26"}}}}`)
	got, err := Lookup("TK-112")
	if err != nil {
		t.Fatal(err)
	}
	want := Issue{ID: "TK-112", Title: "Live links", URL: "https://linear.app/x/issue/TK-112", Status: "In Progress", StateType: "started", Assignee: "Ana", Milestone: "v0.26"}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	c := (*calls)[0]
	if c.Auth != testKey || c.Variables["id"] != "TK-112" || !strings.Contains(c.Query, "issue(id:") {
		t.Fatalf("request %+v", c)
	}
}

func TestLookupMissingIssueAndMissingKey(t *testing.T) {
	setup(t, nil)
	fake(t, `{"data":{"issue":null}}`, `{"errors":[{"message":"Entity not found: Issue"}]}`)
	for range 2 {
		if _, err := Lookup("TK-1"); err != ErrNotFound {
			t.Fatalf("missing issue: %v", err)
		}
	}
	if _, err := Lookup("not an id"); err == nil {
		t.Fatal("bad id accepted")
	}
	t.Setenv("LINEAR_API_KEY", "")
	if _, err := Lookup("TK-1"); err == nil || strings.Contains(err.Error(), testKey) {
		t.Fatalf("no key: %v", err)
	}
}
