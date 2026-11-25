package notify

import (
	"strings"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/cards"
	"github.com/Zovaris/smithy/internal/events"
)

func TestDecisionNotificationsCarryTheDaemonID(t *testing.T) {
	it, ok := classify(events.Event{Kind: "stage", Batch: "nb", Item: "n1", PR: "https://github.com/o/r/pull/3", Detail: "ready"})
	if !ok {
		t.Fatal("not classified")
	}
	for _, f := range []func(item) (string, []button){render, renderPlain} {
		if m, _ := f(it); !strings.Contains(m, "merge:nb/n1") {
			t.Fatalf("no id in %q", m)
		}
	}
	if _, err := cards.Set("nb", "n2", []string{"state=needs_decision", "decision_key=k", "question=q?", "impact=low"}); err != nil {
		t.Fatal(err)
	}
	it, _ = classify(events.Event{Kind: "stage", Batch: "nb", Item: "n2", Detail: "needs_decision"})
	enrich(&it, nil, time.Now())
	if m, _ := render(it); !strings.Contains(m, "decide:nb/n2/k") {
		t.Fatalf("no id in %q", m)
	}
}
