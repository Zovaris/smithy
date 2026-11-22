package cleanup

import "testing"

func TestResult(t *testing.T) {
	for _, tc := range []struct {
		wt, exists bool
		msg        string
		code       int
	}{
		{false, false, "no branch b", 1},
		{true, false, "cleaned b", 0},
		{false, true, "kept b: branch still exists", 1},
		{true, true, "kept b: branch still exists", 1},
	} {
		msg, code := Result("b", tc.wt, tc.exists)
		if msg != tc.msg || code != tc.code {
			t.Errorf("wt=%v exists=%v: got %q,%d want %q,%d", tc.wt, tc.exists, msg, code, tc.msg, tc.code)
		}
	}
}
