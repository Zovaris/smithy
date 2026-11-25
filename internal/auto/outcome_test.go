package auto

import (
	"errors"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/events"
)

func TestOutcomesRecordsEachAutoPRChangeOnce(t *testing.T) {
	t0 := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	es := []events.Event{
		{T: t0, Kind: "launched", Batch: "a", Item: "01", PR: "https://issue/1"},
		{T: t0, Kind: "stage", Batch: "a", Item: "01", PR: "https://x/pull/1", Detail: "pr_open"},
		{T: t0, Kind: "launched", Batch: "a", Item: "02"},
		{T: t0, Kind: "stage", Batch: "a", Item: "02", PR: "https://x/pull/2", Detail: "pr_open"},
		{T: t0, Kind: "outcome", Batch: "a", Item: "02", PR: "https://x/pull/2", Detail: "changes_requested"},
		{T: t0, Kind: "launched", Batch: "a", Item: "03"},
		{T: t0, Kind: "stage", Batch: "a", Item: "03", PR: "https://x/pull/3", Detail: "pr_open"},
		{T: t0, Kind: "outcome", Batch: "a", Item: "03", PR: "https://x/pull/3", Detail: "merged"},
		{T: t0, Kind: "launched", Batch: "a", Item: "04"},
		{T: t0, Kind: "stage", Batch: "w", Item: "09", PR: "https://x/pull/9", Detail: "pr_open"},
	}
	asked := map[string]bool{}
	view := func(pr string) (string, string, error) {
		asked[pr] = true
		switch pr {
		case "https://x/pull/1":
			return "CLOSED", "", nil
		case "https://x/pull/2":
			return "OPEN", "CHANGES_REQUESTED", nil
		}
		return "", "", errors.New("unexpected")
	}
	got := Outcomes(es, view)
	if len(got) != 1 || got[0].Kind != "outcome" || got[0].Item != "01" || got[0].Detail != "closed" || got[0].PR != "https://x/pull/1" {
		t.Fatalf("outcomes = %+v, want only a/01 closed", got)
	}
	if asked["https://x/pull/3"] || asked["https://x/pull/9"] {
		t.Fatalf("asked a final or non-auto PR: %v", asked)
	}
}

func TestOutcomeOf(t *testing.T) {
	for _, c := range []struct{ state, review, want string }{
		{"MERGED", "", "merged"}, {"CLOSED", "CHANGES_REQUESTED", "closed"},
		{"OPEN", "CHANGES_REQUESTED", "changes_requested"}, {"OPEN", "APPROVED", ""}, {"OPEN", "", ""},
	} {
		if got := outcomeOf(c.state, c.review); got != c.want {
			t.Fatalf("%s/%s = %q, want %q", c.state, c.review, got, c.want)
		}
	}
}
