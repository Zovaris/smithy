package auto

import (
	"testing"
)

func TestWorkerCannotReport(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	t.Setenv("SMITHY_WARDEN_ITEM", "b1/i1")
	for _, st := range []string{"blocked", "completed", "started"} {
		if rc := Main([]string{"report", "github:1", st}); rc != 2 {
			t.Fatalf("worker report %s: rc %d", st, rc)
		}
	}
}
