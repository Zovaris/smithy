package gitx

import (
	"encoding/json"
	"testing"
)

func TestActionFollowsTendOrder(t *testing.T) {
	cases := []struct {
		s    PRState
		want string
	}{
		{PRState{State: "MERGED", Checks: "red"}, "cleanup"},
		{PRState{State: "CLOSED"}, "cleanup"},
		{PRState{State: "OPEN", Checks: "red", Mergeable: "CONFLICTING", Unresolved: 2}, "fix-ci"},
		{PRState{State: "OPEN", Checks: "green", Mergeable: "CONFLICTING"}, "sync-base"},
		{PRState{State: "OPEN", Checks: "pending", Mergeable: "MERGEABLE", Behind: json.Number("3")}, "sync-base"},
		{PRState{State: "OPEN", Checks: "pending", Mergeable: "MERGEABLE", Behind: json.Number("0"), Unresolved: 1}, "answer-review"},
		{PRState{State: "OPEN", Checks: "pending", Mergeable: "MERGEABLE"}, "wait"},
		{PRState{State: "OPEN", Checks: "green", Mergeable: "MERGEABLE", Behind: 0}, "done"},
		{PRState{State: "OPEN", Checks: "none", Mergeable: "UNKNOWN"}, "wait"},
		{PRState{State: "OPEN", Checks: "green", Mergeable: "MERGEABLE", Review: "CHANGES_REQUESTED"}, "needs-you"},
		{PRState{State: "OPEN", Checks: "green", Mergeable: "BLOCKED"}, "needs-you"},
		{PRState{State: "OPEN", Checks: "none", Mergeable: "MERGEABLE", Review: "APPROVED"}, "done"},
	}
	for _, c := range cases {
		if got := Action(c.s); got != c.want {
			t.Errorf("%+v → %s, want %s", c.s, got, c.want)
		}
	}
}
