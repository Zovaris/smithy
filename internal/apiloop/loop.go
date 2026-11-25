// Package apiloop is smithy's own agent loop for `smithy agent --backend api`: it sends the
// conversation to a provider, runs the tool calls the model asks for (each through a permission
// check), feeds the results back and repeats until the model ends its turn. Providers (the
// harness build's SDK adapters) stream text and report token usage; the loop retries 429 and 5xx
// answers with backoff and prices the usage. It never sees or logs a key: providers are built
// with one already.
package apiloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"regexp"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/usage"
)

const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

const (
	StopEnd   = "end_turn"
	StopTool  = "tool_use"
	StopLimit = "max_tokens"
)

// ToolCall is one tool the model asked to run, with its JSON arguments.
type ToolCall struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

// ToolResult answers the ToolCall with the same ID.
type ToolResult struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Output  string `json:"output"`
	IsError bool   `json:"is_error,omitempty"`
}

// Part is one piece of a message: text, a tool call or a tool result.
type Part struct {
	Text       string      `json:"text,omitempty"`
	ToolCall   *ToolCall   `json:"tool_call,omitempty"`
	ToolResult *ToolResult `json:"tool_result,omitempty"`
}

// Message is one turn of the conversation.
type Message struct {
	Role  string `json:"role"`
	Parts []Part `json:"parts"`
}

// UserText is a user message holding only text.
func UserText(s string) Message { return Message{Role: RoleUser, Parts: []Part{{Text: s}}} }

// ToolSpec describes a tool to the model; Schema is its arguments' JSON schema.
type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema,omitempty"`
}

// Request is one call to a provider.
type Request struct {
	Model     string
	System    string
	Messages  []Message
	Tools     []ToolSpec
	MaxTokens int
}

// Reply is a provider's answer to one Request. Usage.CostUSD is set only when the provider
// reports the cost itself.
type Reply struct {
	Parts []Part
	Stop  string
	Usage usage.Usage
}

// Provider streams one model reply; onText gets the text as it arrives.
type Provider interface {
	Stream(ctx context.Context, r Request, onText func(string)) (Reply, error)
}

// StatusError is a provider's HTTP error; 429 and 5xx are retried.
type StatusError struct {
	Code       int
	RetryAfter time.Duration
	Err        error
}

func (e *StatusError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("HTTP %d: %v", e.Code, e.Err)
	}
	return fmt.Sprintf("HTTP %d", e.Code)
}

func (e *StatusError) Unwrap() error { return e.Err }

// Retryable reports whether err is a rate limit or a server error.
func Retryable(err error) bool {
	var se *StatusError
	return errors.As(err, &se) && (se.Code == 429 || se.Code >= 500)
}

// Tools are the tools the loop may run. Permit decides each call first (a rejected call is
// answered with an error result and reported as a permission); Call runs it.
type Tools interface {
	Specs() []ToolSpec
	Permit(c ToolCall) (bool, string)
	Call(ctx context.Context, c ToolCall) (string, error)
}

// Sink receives what the loop does: streamed text, finished tool calls, permission verdicts and
// each reply's priced usage.
type Sink interface {
	Text(t string)
	Tool(c ToolCall, r ToolResult)
	Permission(c ToolCall, allowed bool, why string)
	Usage(u usage.Usage)
}

// Options tune a run: Provider names the provider for pricing, MaxTurns caps the replies
// (default 50) and Retries the retries per reply on 429/5xx (default 5).
type Options struct {
	Provider string
	MaxTurns int
	Retries  int
	Sleep    func(ctx context.Context, d time.Duration) error
}

// Result is how a run ended.
type Result struct {
	Stop     string
	Turns    int
	Usage    usage.Usage
	Messages []Message
}

// ErrMaxTurns means the model was still calling tools after Options.MaxTurns replies.
var ErrMaxTurns = errors.New("stopped: too many turns")

// Backoff is the wait before retry n (0-based): exponential from 1s with jitter, at most 60s.
func Backoff(n int) time.Duration {
	d := time.Second << min(n, 6)
	d = d/2 + time.Duration(rand.Int64N(int64(d/2)+1))
	return min(d, 60*time.Second)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Run drives the conversation in req until the model ends its turn. tools may be nil.
func Run(ctx context.Context, p Provider, req Request, tools Tools, sink Sink, o Options) (Result, error) {
	if o.MaxTurns <= 0 {
		o.MaxTurns = 50
	}
	if o.Retries <= 0 {
		o.Retries = 5
	}
	if o.Sleep == nil {
		o.Sleep = sleepCtx
	}
	if tools != nil {
		req.Tools = tools.Specs()
	}
	var res Result
	for res.Turns < o.MaxTurns {
		rep, err := stream(ctx, p, req, sink, o)
		res.Messages = req.Messages
		if err != nil {
			return res, err
		}
		res.Turns++
		if rep.Usage.CostUSD == nil {
			rep.Usage.CostUSD = Cost(o.Provider, req.Model, rep.Usage)
		}
		res.Usage = res.Usage.Add(rep.Usage)
		sink.Usage(rep.Usage)
		res.Stop = rep.Stop
		req.Messages = append(req.Messages, Message{Role: RoleAssistant, Parts: rep.Parts})
		res.Messages = req.Messages
		calls := toolCalls(rep.Parts)
		if len(calls) == 0 {
			return res, nil
		}
		results := make([]Part, 0, len(calls))
		for _, c := range calls {
			r := run(ctx, tools, c, sink)
			results = append(results, Part{ToolResult: &r})
		}
		req.Messages = append(req.Messages, Message{Role: RoleUser, Parts: results})
		res.Messages = req.Messages
	}
	return res, ErrMaxTurns
}

// stream asks p for one reply, retrying 429 and 5xx. Text reaches the sink live; after a retry,
// text the sink already got is not sent again: a retry's text is held until it succeeds and only
// what goes past the earlier attempts is sent (a reply that took another turn starts on a new line).
func stream(ctx context.Context, p Provider, req Request, sink Sink, o Options) (Reply, error) {
	var sent strings.Builder
	for n := 0; ; n++ {
		var held strings.Builder
		onText := func(t string) {
			if n == 0 {
				sent.WriteString(t)
				sink.Text(t)
				return
			}
			held.WriteString(t)
		}
		rep, err := p.Stream(ctx, req, onText)
		if n > 0 && err == nil {
			h, s := held.String(), sent.String()
			if rest, ok := strings.CutPrefix(h, s); ok {
				sink.Text(rest)
			} else if h != "" {
				sink.Text("\n" + h)
			}
		}
		if err == nil || !Retryable(err) || n >= o.Retries || ctx.Err() != nil {
			return rep, err
		}
		wait := Backoff(n)
		var se *StatusError
		if errors.As(err, &se) && se.RetryAfter > 0 {
			wait = min(se.RetryAfter, 5*time.Minute)
		}
		if err := o.Sleep(ctx, wait); err != nil {
			return Reply{}, err
		}
	}
}

func toolCalls(ps []Part) []ToolCall {
	var out []ToolCall
	for _, p := range ps {
		if p.ToolCall != nil {
			out = append(out, *p.ToolCall)
		}
	}
	return out
}

func run(ctx context.Context, tools Tools, c ToolCall, sink Sink) ToolResult {
	r := ToolResult{ID: c.ID, Name: c.Name}
	if tools == nil {
		r.Output, r.IsError = "no tools are available", true
		sink.Permission(c, false, "no tools are available")
		return r
	}
	if ok, why := tools.Permit(c); !ok {
		r.Output, r.IsError = "permission denied: "+why, true
		sink.Permission(c, false, why)
		return r
	}
	out, err := tools.Call(WithSink(ctx, sink), c)
	if err != nil {
		r.Output, r.IsError = err.Error(), true
	} else {
		r.Output = out
	}
	sink.Tool(c, r)
	return r
}

type sinkKey struct{}

// WithSink carries the loop's sink to a tool, so tools that run inner calls (code mode) report
// them as events and permission decisions too.
func WithSink(ctx context.Context, s Sink) context.Context {
	return context.WithValue(ctx, sinkKey{}, s)
}

// SinkFrom is the loop's sink in ctx, or nil.
func SinkFrom(ctx context.Context) Sink {
	s, _ := ctx.Value(sinkKey{}).(Sink)
	return s
}

// Providers smithy knows: the service name `smithy auth set` keeps the key under ("" for ollama,
// which needs none) and the endpoint, always explicit so an SDK's *_BASE_URL env var can't
// redirect the key.
var Providers = map[string]struct{ Service, BaseURL string }{
	"anthropic":  {"anthropic", "https://api.anthropic.com/"},
	"openai":     {"openai", "https://api.openai.com/v1/"},
	"gemini":     {"gemini", "https://generativelanguage.googleapis.com/"},
	"openrouter": {"openrouter", "https://openrouter.ai/api/v1"},
	"deepseek":   {"deepseek", "https://api.deepseek.com/v1"},
	"ollama":     {"", "http://localhost:11434/v1"},
}

// ParseTarget splits "[api:]<provider>/<model>" (the model may hold slashes).
func ParseTarget(s string) (provider, model string, err error) {
	s = strings.TrimPrefix(s, "api:")
	provider, model, ok := strings.Cut(s, "/")
	if !ok || model == "" {
		return "", "", fmt.Errorf("api backend: want <provider>/<model>, got %q", s)
	}
	if _, ok := Providers[provider]; !ok {
		return "", "", fmt.Errorf("api backend: unknown provider %q (known: anthropic, openai, gemini, openrouter, deepseek, ollama)", provider)
	}
	return provider, model, nil
}

type price struct{ in, out, read, write float64 }

// prices are list prices in USD per million tokens (input, output, cache read, cache write), from
// https://docs.anthropic.com/en/docs/about-claude/pricing and https://openai.com/api/pricing/.
var prices = map[string]map[string]price{
	"anthropic": {
		"claude-opus-4-5":   {5, 25, 0.5, 6.25},
		"claude-opus-4-1":   {15, 75, 1.5, 18.75},
		"claude-opus-4":     {15, 75, 1.5, 18.75},
		"claude-sonnet-4-5": {3, 15, 0.3, 3.75},
		"claude-sonnet-4":   {3, 15, 0.3, 3.75},
		"claude-haiku-4-5":  {1, 5, 0.1, 1.25},
		"claude-3-5-haiku":  {0.8, 4, 0.08, 1},
	},
	"openai": {
		"gpt-5":        {1.25, 10, 0.125, 0},
		"gpt-5-mini":   {0.25, 2, 0.025, 0},
		"gpt-5-nano":   {0.05, 0.4, 0.005, 0},
		"gpt-4.1":      {2, 8, 0.5, 0},
		"gpt-4.1-mini": {0.4, 1.6, 0.1, 0},
		"gpt-4.1-nano": {0.1, 0.4, 0.025, 0},
		"gpt-4o":       {2.5, 10, 1.25, 0},
		"gpt-4o-mini":  {0.15, 0.6, 0.075, 0},
		"o4-mini":      {1.1, 4.4, 0.275, 0},
	},
}

// snapshot is what may follow a listed model id: a date (-20250929, -2025-08-07) or -latest.
var snapshot = regexp.MustCompile(`^-(\d{8}|\d{4}-\d{2}-\d{2}|latest)$`)

// Cost prices u at the list price of provider's model: the id must equal a listed one or be one
// followed by a snapshot date. nil (unknown cost) for any other model, never a guess.
func Cost(provider, model string, u usage.Usage) *float64 {
	p, ok := prices[provider][model]
	if !ok {
		for pre, pr := range prices[provider] {
			if rest, cut := strings.CutPrefix(model, pre); cut && snapshot.MatchString(rest) {
				p, ok = pr, true
				break
			}
		}
	}
	if !ok {
		return nil
	}
	c := (float64(u.Input)*p.in + float64(u.Output)*p.out + float64(u.CacheRead)*p.read + float64(u.CacheWrite)*p.write) / 1e6
	return &c
}
