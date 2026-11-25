package apiloop

import (
	"context"
	"testing"

	"github.com/Zovaris/smithy/internal/usage"
)

func TestRetryDoesNotRepeatStreamedText(t *testing.T) {
	p := &fakeProvider{steps: []step{
		{text: "Hello, wor", err: &StatusError{Code: 503}},
		{text: "Hello, world", reply: Reply{Stop: StopEnd}},
	}}
	r := &rec{}
	if _, err := Run(context.Background(), p, Request{}, nil, r, Options{Sleep: noSleep}); err != nil {
		t.Fatal(err)
	}
	if got := r.text.String(); got != "Hello, world" {
		t.Fatalf("sink got %q", got)
	}
}

func TestCostMatchesOnlyExactOrSnapshotIDs(t *testing.T) {
	u := usage.Usage{Input: 1_000_000, Output: 1_000_000}
	for _, tc := range []struct {
		provider, model string
		want            float64
	}{
		{"openai", "gpt-5", 11.25},
		{"openai", "gpt-5-2025-08-07", 11.25},
		{"openai", "gpt-5-mini", 2.25},
		{"openai", "gpt-4.1-nano", 0.5},
		{"openai", "gpt-4o-mini", 0.75},
		{"anthropic", "claude-haiku-4-5-20251001", 6},
		{"anthropic", "claude-opus-4-1-20250805", 90},
	} {
		c := Cost(tc.provider, tc.model, u)
		if c == nil || *c < tc.want-0.001 || *c > tc.want+0.001 {
			t.Errorf("%s/%s: cost %v, want %v", tc.provider, tc.model, c, tc.want)
		}
	}
	for _, m := range []string{"gpt-5-pro", "gpt-5-codex-max", "gpt-4o-audio-preview", "claude-opus-4-7"} {
		if c := Cost("openai", m, u); c != nil {
			t.Errorf("%s: unknown model priced at %v", m, *c)
		}
		if c := Cost("anthropic", m, u); c != nil {
			t.Errorf("%s: unknown model priced at %v", m, *c)
		}
	}
}

type sinkTools struct{ echoTools }

func (sinkTools) Call(ctx context.Context, c ToolCall) (string, error) {
	if SinkFrom(ctx) == nil {
		return "no sink", nil
	}
	return "sink", nil
}

func TestToolCallsSeeTheSink(t *testing.T) {
	r := &rec{}
	res := run(context.Background(), sinkTools{}, ToolCall{ID: "1", Name: "echo"}, r)
	if res.Output != "sink" {
		t.Fatalf("tool saw %q", res.Output)
	}
}
