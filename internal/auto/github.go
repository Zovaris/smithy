package auto

import (
	"fmt"
	"strconv"
	"time"

	"github.com/Zovaris/smithy/internal/ghx"
)

// GitHub is this repo's open issues, through gh or GH_TOKEN. In labeled mode it lists only
// issues with auto.label.
type GitHub struct{ Settings Settings }

// Name is "github".
func (GitHub) Name() string { return "github" }

// Poll lists the open issues; since is not needed, seen ids dedupe.
func (g GitHub) Poll(time.Time) ([]Issue, error) {
	label := ""
	if g.Settings.Mode != "all" {
		label = g.Settings.Label
	}
	var raw []struct {
		Number int
		URL    string
		Title  string
		Body   string
		Labels []struct{ Name string }
	}
	if err := ghx.IssueListFull(label, &raw); err != nil {
		return nil, err
	}
	out := make([]Issue, 0, len(raw))
	for _, r := range raw {
		i := Issue{Source: "github", ID: strconv.Itoa(r.Number), Title: r.Title, URL: r.URL, Body: r.Body}
		for _, l := range r.Labels {
			i.Labels = append(i.Labels, l.Name)
		}
		out = append(out, i)
	}
	return out, nil
}

// Report comments on the issue.
func (GitHub) Report(id, state, pr string) error {
	n, err := strconv.Atoi(id)
	if err != nil || n <= 0 {
		return fmt.Errorf("github issue id must be a number, not %q", id)
	}
	return ghx.IssueComment(n, Comment(state, pr))
}

// Comment is the report text a source posts.
func Comment(state, pr string) string {
	msg := map[string]string{
		"started":   "smithy auto: work started.",
		"completed": "smithy auto: done.",
		"blocked":   "smithy auto: blocked; it needs a person.",
	}[state]
	if pr != "" {
		msg += " PR: " + pr
	}
	return msg
}
