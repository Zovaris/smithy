package gitx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/Zovaris/smithy/internal/ghx"
)

const prStateUsage = "usage: smithy pr-state <pr-url|number|branch>"

var (
	stateFailRe    = regexp.MustCompile(`FAIL|ERROR|CANCEL|TIMED_OUT|ACTION_REQUIRED|STARTUP_FAILURE`)
	statePendingRe = regexp.MustCompile(`PENDING|QUEUED|IN_PROGRESS|EXPECTED|WAITING|REQUESTED`)
)

// PRState is the compact state of one PR that tend decides from.
type PRState struct {
	URL        any    `json:"url"`
	State      any    `json:"state"`
	Checks     string `json:"checks"`
	Mergeable  any    `json:"mergeable"`
	Review     any    `json:"review"`
	Unresolved int    `json:"unresolved"`
	Behind     any    `json:"behind"`
	Head       any    `json:"head"`
	Base       any    `json:"base"`
	Action     string `json:"action"`
}

// Action is the first step tend takes on a PR in this state, in tend's order: cleanup (merged
// or closed), fix-ci, sync-base (conflicting or behind), answer-review, wait (checks pending),
// needs-you (changes requested or review required with no open thread, or not mergeable for
// another reason), done (green, mergeable, approved or no review required).
func Action(s PRState) string {
	switch {
	case s.State == "MERGED" || s.State == "CLOSED":
		return "cleanup"
	case s.Checks == "red":
		return "fix-ci"
	case s.Mergeable == "CONFLICTING" || behind(s.Behind) > 0:
		return "sync-base"
	case s.Unresolved > 0:
		return "answer-review"
	case s.Checks == "pending" || s.Mergeable == "UNKNOWN":
		return "wait"
	case s.Review == "CHANGES_REQUESTED" || s.Review == "REVIEW_REQUIRED" || s.Mergeable != "MERGEABLE":
		return "needs-you"
	}
	return "done"
}

func behind(v any) int {
	switch n := v.(type) {
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	case int:
		return n
	case float64:
		return int(n)
	}
	return 0
}

type stateView struct {
	URL               any      `json:"url"`
	Number            int      `json:"number"`
	State             any      `json:"state"`
	Mergeable         any      `json:"mergeable"`
	ReviewDecision    any      `json:"reviewDecision"`
	StatusCheckRollup []Rollup `json:"statusCheckRollup"`
	HeadRefName       any      `json:"headRefName"`
	BaseRefName       any      `json:"baseRefName"`
	HeadOwner         *struct {
		Login string `json:"login"`
	} `json:"headRepositoryOwner"`
}

type threads struct {
	Data struct {
		Repository struct {
			PullRequest struct {
				ReviewThreads struct {
					Nodes []struct {
						IsResolved bool `json:"isResolved"`
					} `json:"nodes"`
				} `json:"reviewThreads"`
			} `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
}

type compare struct {
	BehindBy any `json:"behind_by"`
}

func orElse(v, fallback any) any {
	if v == nil || v == false {
		return fallback
	}
	return v
}

func decode(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	return d.Decode(v)
}

// Summarize builds the PR state from the raw JSON of `gh pr view`, the review-threads GraphQL
// query and the compare API.
func Summarize(view, thr, cmp []byte) (PRState, error) {
	var v stateView
	var t threads
	var c compare
	if err := decode(view, &v); err != nil {
		return PRState{}, fmt.Errorf("view: %w", err)
	}
	if err := decode(thr, &t); err != nil {
		return PRState{}, fmt.Errorf("threads: %w", err)
	}
	if err := decode(cmp, &c); err != nil {
		return PRState{}, fmt.Errorf("compare: %w", err)
	}
	up := make([]Rollup, len(v.StatusCheckRollup))
	for i, r := range v.StatusCheckRollup {
		up[i] = Rollup{Conclusion: strings.ToUpper(r.outcome())}
	}
	unresolved := 0
	for _, n := range t.Data.Repository.PullRequest.ReviewThreads.Nodes {
		if !n.IsResolved {
			unresolved++
		}
	}
	review := orElse(v.ReviewDecision, "")
	if review == "" {
		review = "none"
	}
	st := PRState{
		URL:        v.URL,
		State:      v.State,
		Checks:     Checks(up, stateFailRe, statePendingRe),
		Mergeable:  orElse(v.Mergeable, "UNKNOWN"),
		Review:     review,
		Unresolved: unresolved,
		Behind:     orElse(c.BehindBy, 0),
		Head:       v.HeadRefName,
		Base:       v.BaseRefName,
	}
	st.Action = Action(st)
	return st, nil
}

func printState(s PRState) int {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		fmt.Fprintln(os.Stderr, "smithy pr-state:", err)
		return 1
	}
	os.Stdout.Write(buf.Bytes())
	return 0
}

// PRStateMain runs `smithy pr-state`.
func PRStateMain(args []string) int {
	if len(args) > 0 && args[0] == "--from" {
		if len(args) != 4 {
			fmt.Fprintln(os.Stderr, "usage: smithy pr-state --from <view.json> <threads.json> <compare.json>")
			return 2
		}
		var raw [3][]byte
		for i, f := range args[1:] {
			b, err := os.ReadFile(f)
			if err != nil {
				fmt.Fprintln(os.Stderr, "smithy pr-state:", err)
				return 2
			}
			raw[i] = b
		}
		s, err := Summarize(raw[0], raw[1], raw[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, "smithy pr-state:", err)
			return 2
		}
		return printState(s)
	}
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, prStateUsage)
		return 2
	}
	if err := ghx.Ready(); err != nil {
		fmt.Fprintln(os.Stderr, "smithy pr-state:", err)
		return 3
	}
	var raw json.RawMessage
	if err := ghx.PRView(args[0], &raw); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	view := []byte(raw)
	repo, err := ghx.RepoName()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	var v stateView
	if err := decode(view, &v); err != nil {
		fmt.Fprintln(os.Stderr, "smithy pr-state:", err)
		return 1
	}
	thr, err := ghx.ReviewThreads(repo, v.Number)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	head := jqText(v.HeadRefName)
	if v.HeadOwner != nil && v.HeadOwner.Login != "" {
		head = v.HeadOwner.Login + ":" + head
	}
	cmp, err := ghx.Compare(repo, jqText(v.BaseRefName), head)
	if err != nil {
		cmp = []byte(`{"behind_by":0}`)
	}
	s, err := Summarize(view, thr, cmp)
	if err != nil {
		fmt.Fprintln(os.Stderr, "smithy pr-state:", err)
		return 1
	}
	return printState(s)
}
