package gitx

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Zovaris/smithy/internal/ghx"
)

const verifyUsage = "usage: smithy verify <repo-dir> <branch> [pr-url|number]\n  a PR URL is checked against <repo-dir>'s origin (exit 2 on a mismatch); prefer it over a number\n  <repo-dir> is the local checkout path (\".\" for here), not owner/name\nverdicts: green or merged (exit 0); draft, everything green but still a draft (exit 3);\nconflict, the PR can't merge into its base (exit 4: rebase or merge the base, then re-verify);\npending (checks running, or GitHub has not computed mergeable yet), red, unpushed, closed, no-pr (exit 1). checks= is green, red, pending or none (no checks, or only skipped or neutral ones);\nnone counts as pending when .github/workflows holds a workflow (CI has not registered yet); with no workflow there, none counts green (no CI configured), and a PR whose checks are all skipped is none."

var (
	failRe    = regexp.MustCompile(`(?i)FAIL|ERROR|CANCEL|TIMED_OUT|ACTION_REQUIRED`)
	pendingRe = regexp.MustCompile(`(?i)PENDING|QUEUED|IN_PROGRESS|EXPECTED|WAITING`)
)

var (
	prURLRe  = regexp.MustCompile(`github\.com/([^/\s]+/[^/\s]+)/pull/\d+`)
	originRe = regexp.MustCompile(`github\.com[:/]([^/\s]+/[^/\s]+?)(?:\.git)?/?$`)
)

// PRRepo is the owner/name of a PR URL, or "" for a bare number.
func PRRepo(pr string) string {
	if m := prURLRe.FindStringSubmatch(pr); m != nil {
		return m[1]
	}
	return ""
}

// OriginRepo is the owner/name of a GitHub remote URL (https or ssh), or "".
func OriginRepo(url string) string {
	if m := originRe.FindStringSubmatch(strings.TrimSpace(url)); m != nil {
		return m[1]
	}
	return ""
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// Rollup is one entry of a PR's statusCheckRollup.
type Rollup struct {
	Conclusion string `json:"conclusion"`
	State      string `json:"state"`
}

func (r Rollup) outcome() string {
	switch {
	case r.Conclusion != "":
		return r.Conclusion
	case r.State != "":
		return r.State
	}
	return "PENDING"
}

// Checks sums a statusCheckRollup into green, red, pending or none, using these patterns.
func Checks(rollup []Rollup, fail, pending *regexp.Regexp) string {
	if len(rollup) == 0 {
		return "none"
	}
	anyOf := func(re *regexp.Regexp) bool {
		for _, r := range rollup {
			if re.MatchString(r.outcome()) {
				return true
			}
		}
		return false
	}
	switch {
	case anyOf(fail):
		return "red"
	case anyOf(pending):
		return "pending"
	case !anyOf(regexp.MustCompile(`^SUCCESS$`)):
		// Only skipped or neutral checks: nothing ran, so there is no proof yet.
		return "none"
	}
	return "green"
}

// CIPending reports whether checks "none" must count as pending: the repo at dir has a
// workflow file in .github/workflows, so CI will register checks soon.
func CIPending(dir string) bool {
	entries, _ := os.ReadDir(filepath.Join(dir, ".github", "workflows"))
	for _, e := range entries {
		if n := e.Name(); !e.IsDir() && (strings.HasSuffix(n, ".yml") || strings.HasSuffix(n, ".yaml")) {
			return true
		}
	}
	return false
}

type prView struct {
	URL               any      `json:"url"`
	State             any      `json:"state"`
	HeadRefOid        any      `json:"headRefOid"`
	Mergeable         any      `json:"mergeable"`
	IsDraft           any      `json:"isDraft"`
	StatusCheckRollup []Rollup `json:"statusCheckRollup"`
}

func jqText(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// Verdict is the verify verdict for a PR given the worker's local head; ancestor reports whether
// local is an ancestor of the PR head.
func Verdict(state, checks, mergeable string, draft bool, local, head string, ancestor func() bool) string {
	switch state {
	case "MERGED":
		return "merged"
	case "CLOSED":
		return "closed"
	}
	if local != "" && local != head && !ancestor() {
		return "unpushed"
	}
	switch {
	case mergeable == "CONFLICTING":
		return "conflict"
	case checks == "red":
		return "red"
	case checks == "pending" || mergeable == "UNKNOWN":
		return "pending"
	case draft:
		return "draft"
	}
	return "green"
}

// VerifyMain runs `smithy verify`: one line of proof for a worker's PR claim; exit 0 for green or merged.
func VerifyMain(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, verifyUsage)
		return 2
	}
	branch, pr := args[1], ""
	if len(args) > 2 {
		pr = args[2]
	}
	if err := os.Chdir(args[0]); err != nil {
		fmt.Fprintln(os.Stderr, "smithy verify:", err)
		return 1
	}
	if want := PRRepo(pr); want != "" {
		if got := OriginRepo(git("remote", "get-url", "origin")); !strings.EqualFold(got, want) {
			fmt.Fprintf(os.Stderr, "smithy verify: %s is a PR of %s, but %s is a checkout of %s\n", pr, want, args[0], or(got, "no origin"))
			return 2
		}
	}
	if pr == "" {
		var list []struct {
			Number int `json:"number"`
		}
		if ghx.PRList(branch, "all", &list) == nil && len(list) > 0 {
			pr = strconv.Itoa(list[0].Number)
		}
	}
	if pr == "" {
		fmt.Printf("no-pr branch=%s\n", branch)
		return 1
	}
	var v prView
	if err := ghx.PRView(pr, &v); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	state, head, mergeable, draft := jqText(v.State), jqText(v.HeadRefOid), jqText(v.Mergeable), jqText(v.IsDraft)
	checks := Checks(v.StatusCheckRollup, failRe, pendingRe)
	if checks == "none" && CIPending(".") {
		checks = "pending"
	}
	local := git("rev-parse", "--verify", "-q", "refs/heads/"+branch)
	verdict := Verdict(state, checks, mergeable, draft == "true", local, head, func() bool {
		return quiet("git", "merge-base", "--is-ancestor", local, head) == nil
	})
	short := head
	if len(short) > 7 {
		short = short[:7]
	}
	fmt.Printf("%s pr=%s state=%s head=%s checks=%s mergeable=%s draft=%s\n", verdict, jqText(v.URL), state, short, checks, mergeable, draft)
	switch verdict {
	case "green", "merged":
		return 0
	case "draft":
		return 3
	case "conflict":
		return 4
	}
	return 1
}
