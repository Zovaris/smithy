package agents

import (
	"io"
	"testing"

	"github.com/Zovaris/smithy/internal/acp"
)

func TestAutoPermitMapsACPRequestsThroughAutoRules(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	bins := []string{"/bin/smithy"}
	cases := []struct {
		p    acp.Permission
		want bool
	}{
		{acp.Permission{Kind: "execute", Command: "go test ./..."}, true},
		{acp.Permission{Kind: "execute", Command: "git status"}, true},
		{acp.Permission{Kind: "execute", Command: "/bin/smithy status"}, true},
		{acp.Permission{Kind: "execute", Command: "curl https://x"}, false},
		{acp.Permission{Kind: "execute", Command: "cat ~/.ssh/id_ed25519"}, false},
		{acp.Permission{Kind: "execute", Command: "go test ./... && curl x"}, false},
		{acp.Permission{Kind: "execute", Command: "echo $(id)"}, false},
		{acp.Permission{Kind: "execute", Command: "/bin/smithy proxy approve ab12"}, false},
		{acp.Permission{Kind: "execute", Command: "rm -rf /"}, false},
		{acp.Permission{Kind: "execute"}, false},
		{acp.Permission{Kind: "edit", Paths: []string{"/w/r/a.go"}}, true},
		{acp.Permission{Kind: "edit", Paths: []string{"/w/r/.git/hooks/pre-commit"}}, false},
		{acp.Permission{Kind: "edit", Paths: []string{"/w/r/.claude/settings.json"}}, false},
		{acp.Permission{Kind: "edit", Paths: []string{"/w/r/../x/a.go"}}, false},
		{acp.Permission{Kind: "edit", Paths: []string{"/etc/passwd"}}, false},
		{acp.Permission{Kind: "edit"}, false},
		{acp.Permission{Kind: "read", Paths: []string{"/w/r/a.go"}}, true},
		{acp.Permission{Kind: "read", Paths: []string{"/h/.ssh/id_rsa"}}, false},
		{acp.Permission{Kind: "search"}, true},
		{acp.Permission{Kind: "fetch"}, false},
		{acp.Permission{Kind: "delete", Paths: []string{"/w/r/a.go"}}, false},
		{acp.Permission{Kind: "other", Title: "something"}, false},
		{acp.Permission{Title: "unknown"}, false},
	}
	for _, c := range cases {
		if got, _ := autoPermit(c.p, "/w/r", bins); got != c.want {
			t.Errorf("autoPermit(%+v) = %v, want %v", c.p, got, c.want)
		}
	}
}

func TestACPSinkUnderAutoRejectsUnclassified(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	old := addDecision
	addDecision = func(q, choice, why, ref string) error { return nil }
	defer func() { addDecision = old }()
	opts := []acp.Option{{ID: "yes", Kind: "allow_once"}, {ID: "no", Kind: "reject_once"}}
	s := acpSink{out: io.Discard, allow: true, auto: true, cwd: "/w/r", bins: []string{"/bin/smithy"}}
	if id := s.Permit(acp.Permission{Title: "do a thing", Options: opts}); id != "no" {
		t.Fatalf("unclassified request got %q", id)
	}
	if id := s.Permit(acp.Permission{Kind: "execute", Command: "go vet ./...", Options: opts}); id != "yes" {
		t.Fatalf("allowed command got %q", id)
	}
}
