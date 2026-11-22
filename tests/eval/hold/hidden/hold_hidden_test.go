package hold

import "testing"

func TestHoldOnlyWhenTrueOrReason(t *testing.T) {
	for _, tc := range []struct {
		hold any
		want string
	}{{false, "queued"}, {nil, "queued"}, {"", "queued"}, {true, "held"}, {"wait for infra", "held"}} {
		if got := Stage(map[string]any{"hold": tc.hold}); got != tc.want {
			t.Errorf("hold %v: got %q want %q", tc.hold, got, tc.want)
		}
	}
	if got := Stage(map[string]any{}); got != "queued" {
		t.Errorf("no hold key: got %q", got)
	}
}
