package auto

import (
	"github.com/Zovaris/smithy/internal/events"
	"github.com/Zovaris/smithy/internal/ghx"
)

// outcomeOf is the outcome of a PR in GitHub's state and review decision: merged, closed,
// changes_requested, or "" while it is open without one.
func outcomeOf(state, review string) string {
	switch {
	case state == "MERGED":
		return "merged"
	case state == "CLOSED":
		return "closed"
	case review == "CHANGES_REQUESTED":
		return "changes_requested"
	}
	return ""
}

// Outcomes are the new "outcome" events for the PRs of launched (auto) items: one per change of
// outcome, none once a PR is merged or closed. view returns a PR's state and review decision.
func Outcomes(es []events.Event, view func(pr string) (state, review string, err error)) []events.Event {
	auto := map[string]bool{}
	pr := map[string]string{}
	last := map[string]string{}
	var order []string
	for _, e := range es {
		k := e.Batch + "/" + e.Item
		switch {
		case e.Item == "":
		case e.Kind == "launched":
			if !auto[k] {
				auto[k] = true
				order = append(order, k)
			}
		case e.Kind == "outcome":
			last[k] = e.Detail
		case e.PR != "":
			pr[k] = e.PR
		}
	}
	var out []events.Event
	for _, k := range order {
		if pr[k] == "" || last[k] == "merged" || last[k] == "closed" {
			continue
		}
		state, review, err := view(pr[k])
		if err != nil {
			continue
		}
		if o := outcomeOf(state, review); o != "" && o != last[k] {
			b, i := splitKey(k)
			out = append(out, events.Event{Kind: "outcome", Batch: b, Item: i, PR: pr[k], Detail: o})
		}
	}
	return out
}

func splitKey(k string) (batch, item string) {
	for i := len(k) - 1; i >= 0; i-- {
		if k[i] == '/' {
			return k[:i], k[i+1:]
		}
	}
	return "", k
}

// prOutcome reads a PR's state and review decision from GitHub.
func prOutcome(pr string) (string, string, error) {
	var v struct {
		State          string  `json:"state"`
		ReviewDecision *string `json:"reviewDecision"`
	}
	if err := ghx.PRView(pr, &v); err != nil {
		return "", "", err
	}
	review := ""
	if v.ReviewDecision != nil {
		review = *v.ReviewDecision
	}
	return v.State, review, nil
}
