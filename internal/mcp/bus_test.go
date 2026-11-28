package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeBus struct{ calls []string }

func (f *fakeBus) Send(_ context.Context, to, text string) (any, error) {
	f.calls = append(f.calls, "send "+to+" "+text)
	return map[string]string{"to": to}, nil
}
func (f *fakeBus) Inbox(_ context.Context, since int64) (any, error) {
	if since < 0 {
		return nil, errors.New("bad")
	}
	f.calls = append(f.calls, "inbox")
	return []string{"m"}, nil
}
func (f *fakeBus) Start(_ context.Context, agent, dir, msg string) (any, error) {
	f.calls = append(f.calls, "start "+agent+" "+dir+" "+msg)
	return map[string]string{"id": "s-2"}, nil
}

func TestBusTools(t *testing.T) {
	fb := &fakeBus{}
	tools := map[string]Tool{}
	for _, tl := range BusTools("s-1", func() (Bus, error) { return fb, nil }) {
		tools[tl.Name] = tl
	}
	for _, n := range []string{"send", "inbox", "start_session"} {
		if _, ok := tools[n]; !ok {
			t.Fatalf("missing %s: %v", n, tools)
		}
	}
	ctx := context.Background()
	if out, err := tools["send"].Call(ctx, Args{"to": "parent", "text": "done"}); err != nil || !strings.Contains(out, "parent") {
		t.Fatal(out, err)
	}
	if _, err := tools["send"].Call(ctx, Args{"to": "../x", "text": "done"}); err == nil {
		t.Error("to must be parent or a session id")
	}
	if _, err := tools["send"].Call(ctx, Args{"to": "parent"}); err == nil {
		t.Error("text is required")
	}
	if out, err := tools["inbox"].Call(ctx, Args{}); err != nil || !strings.Contains(out, "m") {
		t.Fatal(out, err)
	}
	if out, err := tools["start_session"].Call(ctx, Args{"message": "build"}); err != nil || !strings.Contains(out, "s-2") {
		t.Fatal(out, err)
	}
	if fb.calls[0] != "send parent done" || fb.calls[2] != "start   build" {
		t.Fatalf("%q", fb.calls)
	}
	down := BusTools("s-1", func() (Bus, error) { return nil, errors.New("no smithy daemon is running") })
	if _, err := down[0].Call(ctx, Args{"to": "parent", "text": "x"}); err == nil || !strings.Contains(err.Error(), "daemon") {
		t.Fatalf("no daemon: %v", err)
	}
}
