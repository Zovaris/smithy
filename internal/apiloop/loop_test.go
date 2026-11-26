package apiloop

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/usage"
)

type step struct {
	reply Reply
	err   error
	text  string
}

type fakeProvider struct {
	steps []step
	seen  []Request
}

func (f *fakeProvider) Stream(_ context.Context, r Request, onText func(string)) (Reply, error) {
	f.seen = append(f.seen, r)
	s := f.steps[0]
	f.steps = f.steps[1:]
	if s.text != "" {
		onText(s.text)
	}
	return s.reply, s.err
}

type rec struct {
	text  strings.Builder
	tools []string
	perms []string
	usage []usage.Usage
}

func (r *rec) Text(t string)                   { r.text.WriteString(t) }
func (r *rec) Tool(c ToolCall, res ToolResult) { r.tools = append(r.tools, c.Name+":"+res.Output) }
func (r *rec) Permission(c ToolCall, ok bool, why string) {
	r.perms = append(r.perms, c.Name+":"+map[bool]string{true: "allowed", false: "rejected:" + why}[ok])
}
func (r *rec) Usage(u usage.Usage) { r.usage = append(r.usage, u) }

func noSleep(context.Context, time.Duration) error { return nil }

func TestRunAnswersWithoutTools(t *testing.T) {
	p := &fakeProvider{steps: []step{{text: "hi", reply: Reply{Parts: []Part{{Text: "hi"}}, Stop: StopEnd, Usage: usage.Usage{Input: 10, Output: 2}}}}}
	var r rec
	res, err := Run(context.Background(), p, Request{Model: "m", Messages: []Message{UserText("q")}}, nil, &r, Options{Sleep: noSleep})
	if err != nil || r.text.String() != "hi" || res.Stop != StopEnd || res.Usage.Input != 10 || res.Turns != 1 {
		t.Fatalf("res=%+v err=%v text=%q", res, err, r.text.String())
	}
	if len(r.usage) != 1 || r.usage[0].Output != 2 {
		t.Fatalf("usage events %+v", r.usage)
	}
}

type echoTools struct{ deny string }

func (e echoTools) Specs() []ToolSpec { return []ToolSpec{{Name: "echo"}, {Name: "rm"}} }
func (e echoTools) Permit(c ToolCall) (bool, string) {
	if c.Name == e.deny {
		return false, "not allowed"
	}
	return true, ""
}
func (e echoTools) Call(_ context.Context, c ToolCall) (string, error) {
	if c.Name == "rm" {
		return "", errors.New("boom")
	}
	var a struct{ S string }
	_ = json.Unmarshal(c.Args, &a)
	return a.S, nil
}

func TestRunFeedsToolResultsBackAndSumsUsage(t *testing.T) {
	call := ToolCall{ID: "1", Name: "echo", Args: json.RawMessage(`{"S":"pong"}`)}
	bad := ToolCall{ID: "2", Name: "rm", Args: json.RawMessage(`{}`)}
	p := &fakeProvider{steps: []step{
		{reply: Reply{Parts: []Part{{ToolCall: &call}, {ToolCall: &bad}}, Stop: StopTool, Usage: usage.Usage{Input: 5}}},
		{text: "done", reply: Reply{Parts: []Part{{Text: "done"}}, Stop: StopEnd, Usage: usage.Usage{Input: 7}}},
	}}
	var r rec
	res, err := Run(context.Background(), p, Request{Messages: []Message{UserText("q")}}, echoTools{}, &r, Options{Sleep: noSleep})
	if err != nil || res.Usage.Input != 12 || res.Turns != 2 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if strings.Join(r.tools, ",") != "echo:pong,rm:boom" {
		t.Fatalf("tools %v", r.tools)
	}
	second := p.seen[1].Messages
	last := second[len(second)-1]
	if last.Role != RoleUser || len(last.Parts) != 2 || last.Parts[0].ToolResult.Output != "pong" || !last.Parts[1].ToolResult.IsError {
		t.Fatalf("tool results not fed back: %+v", last)
	}
	if second[1].Role != RoleAssistant || second[1].Parts[0].ToolCall.ID != "1" {
		t.Fatalf("assistant turn not kept: %+v", second[1])
	}
}

func TestRunRejectedPermissionBecomesDecisionAndErrorResult(t *testing.T) {
	bad := ToolCall{ID: "1", Name: "rm"}
	p := &fakeProvider{steps: []step{
		{reply: Reply{Parts: []Part{{ToolCall: &bad}}, Stop: StopTool}},
		{reply: Reply{Stop: StopEnd}},
	}}
	var r rec
	if _, err := Run(context.Background(), p, Request{}, echoTools{deny: "rm"}, &r, Options{Sleep: noSleep}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(r.perms, ",") != "rm:rejected:not allowed" || len(r.tools) != 0 {
		t.Fatalf("perms %v tools %v", r.perms, r.tools)
	}
	res := p.seen[1].Messages[1].Parts[0].ToolResult
	if !res.IsError || !strings.Contains(res.Output, "not allowed") {
		t.Fatalf("result %+v", res)
	}
}

func TestRunRetriesRateLimitsAndServerErrors(t *testing.T) {
	p := &fakeProvider{steps: []step{
		{err: &StatusError{Code: 429, RetryAfter: 3 * time.Second}},
		{err: &StatusError{Code: 503}},
		{reply: Reply{Stop: StopEnd}},
	}}
	var waits []time.Duration
	sleep := func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }
	if _, err := Run(context.Background(), p, Request{}, nil, &rec{}, Options{Sleep: sleep, Retries: 3}); err != nil {
		t.Fatal(err)
	}
	if len(waits) != 2 || waits[0] != 3*time.Second || waits[1] <= 0 {
		t.Fatalf("waits %v", waits)
	}
}

func TestRunDoesNotRetryClientErrorsAndGivesUpAfterRetries(t *testing.T) {
	p := &fakeProvider{steps: []step{{err: &StatusError{Code: 400, Err: errors.New("bad")}}}}
	if _, err := Run(context.Background(), p, Request{}, nil, &rec{}, Options{Sleep: noSleep, Retries: 3}); err == nil || len(p.seen) != 1 {
		t.Fatalf("err=%v calls=%d", err, len(p.seen))
	}
	p = &fakeProvider{steps: []step{{err: &StatusError{Code: 500}}, {err: &StatusError{Code: 500}}, {err: &StatusError{Code: 500}}}}
	if _, err := Run(context.Background(), p, Request{}, nil, &rec{}, Options{Sleep: noSleep, Retries: 2}); err == nil || len(p.seen) != 3 {
		t.Fatalf("err=%v calls=%d", err, len(p.seen))
	}
}

func TestRunStopsAtMaxTurns(t *testing.T) {
	call := ToolCall{ID: "1", Name: "echo", Args: json.RawMessage(`{}`)}
	loop := step{reply: Reply{Parts: []Part{{ToolCall: &call}}, Stop: StopTool}}
	p := &fakeProvider{steps: []step{loop, loop, loop}}
	res, err := Run(context.Background(), p, Request{}, echoTools{}, &rec{}, Options{Sleep: noSleep, MaxTurns: 2})
	if !errors.Is(err, ErrMaxTurns) || res.Turns != 2 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestBackoffGrowsAndIsCapped(t *testing.T) {
	if Backoff(0) <= 0 || Backoff(1) <= Backoff(0)/2 || Backoff(20) > 60*time.Second {
		t.Fatalf("%v %v %v", Backoff(0), Backoff(1), Backoff(20))
	}
}

func TestCost(t *testing.T) {
	useFeed(t, func() ([]byte, error) { return []byte(feed), nil })
	u := usage.Usage{Input: 1_000_000, Output: 1_000_000, CacheRead: 1_000_000, CacheWrite: 1_000_000}
	c := Cost("anthropic", "claude-sonnet-4-5-20250929", u)
	if c == nil || *c < 22.049 || *c > 22.051 {
		t.Fatalf("cost %v", c)
	}
	if Cost("openrouter", "some/unknown", u) != nil {
		t.Fatal("unknown model must have no cost")
	}
}

func TestParseTarget(t *testing.T) {
	p, m, err := ParseTarget("api:openrouter/anthropic/claude-sonnet-4.5")
	if err != nil || p != "openrouter" || m != "anthropic/claude-sonnet-4.5" {
		t.Fatalf("%q %q %v", p, m, err)
	}
	if _, _, err := ParseTarget("openai"); err == nil {
		t.Fatal("no model must fail")
	}
	if _, _, err := ParseTarget("nope/x"); err == nil {
		t.Fatal("unknown provider must fail")
	}
}
