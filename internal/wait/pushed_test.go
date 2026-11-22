package wait

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestPushedAhead(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("commit", "-q", "--allow-empty", "-m", "a")
	sha := git("rev-parse", "HEAD")
	git("commit", "-q", "--allow-empty", "-m", "b")
	next := git("rev-parse", "HEAD")
	git("update-ref", "refs/remotes/origin/feat", next)
	t.Chdir(dir)
	if pushedAhead(&prState{branch: "feat", state: "OPEN", head: next}) {
		t.Error("same head reported as ahead")
	}
	if !pushedAhead(&prState{branch: "feat", state: "OPEN", head: sha}) {
		t.Error("newer push not reported")
	}
	if pushedAhead(&prState{branch: "feat", state: "OPEN", head: "1111111111111111111111111111111111111111"}) {
		t.Error("unknown head (fork, other repo) reported as ahead")
	}
	git("update-ref", "refs/remotes/origin/feat", sha)
	if pushedAhead(&prState{branch: "feat", state: "OPEN", head: next}) {
		t.Error("ref behind the head reported as ahead")
	}
	if pushedAhead(&prState{branch: "other", state: "OPEN", head: "0000"}) {
		t.Error("missing ref reported as ahead")
	}
}
