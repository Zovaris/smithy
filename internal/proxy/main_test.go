package proxy

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/events"
)

func TestEventsApproverApproveDenyTimeout(t *testing.T) {
	t.Setenv("SMITHY_HOME", filepath.Join(t.TempDir(), "h"))
	approve := EventsApprover(2*time.Second, 20*time.Millisecond)
	answer := func(verb string) {
		for i := 0; i < 100; i++ {
			es, _ := events.Read(time.Time{}, 0)
			for j := len(es) - 1; j >= 0; j-- {
				if es[j].Kind == "secret_ask" && !answered[es[j].ID] {
					answered[es[j].ID] = true
					var out, errb bytes.Buffer
					if rc := Main([]string{verb, es[j].ID}, &out, &errb); rc != 0 {
						t.Errorf("%s rc %d %s", verb, rc, errb.String())
					}
					return
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	go answer("approve")
	if !approve(context.Background(), Ask{Name: "linear", Host: "api.linear.app", Method: "POST", Path: "/graphql"}) {
		t.Error("approved ask denied")
	}
	go answer("deny")
	if approve(context.Background(), Ask{Name: "linear", Host: "api.linear.app", Method: "POST", Path: "/graphql"}) {
		t.Error("denied ask approved")
	}
	short := EventsApprover(100*time.Millisecond, 20*time.Millisecond)
	if short(context.Background(), Ask{Name: "linear", Host: "api.linear.app", Method: "DELETE"}) {
		t.Error("timed-out ask approved")
	}
	es, _ := events.Read(time.Time{}, 0)
	for _, e := range es {
		if e.Kind == "secret_ask" && !strings.Contains(e.Detail, "linear POST api.linear.app/graphql") && !strings.Contains(e.Detail, "DELETE") {
			t.Errorf("detail %q", e.Detail)
		}
	}
}

var answered = map[string]bool{}

func TestMainRefusesBadArgs(t *testing.T) {
	var out, errb bytes.Buffer
	for _, a := range [][]string{{}, {"approve"}, {"approve", "x y"}, {"serve", "--addr"}} {
		if rc := Main(a, &out, &errb); rc == 0 {
			t.Errorf("%q accepted", a)
		}
	}
}
