package apiloop

import (
	"context"
	"testing"
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
