//go:build harness

// Package provider adapts the official SDKs to apiloop.Provider (docs/adr/0012): Anthropic
// (anthropic-sdk-go, prompt caching on), OpenAI (openai-go, Responses API), Gemini
// (google.golang.org/genai) and OpenAI-compatible endpoints (openai-go Chat Completions on a
// base URL: OpenRouter, DeepSeek, Ollama). SDK retries are off: apiloop retries 429/5xx itself.
package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	aoption "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/openai/openai-go/v3"
	ooption "github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
	"google.golang.org/genai"

	"github.com/Zovaris/smithy/internal/apiloop"
	"github.com/Zovaris/smithy/internal/usage"
)

// DefaultMaxTokens caps one reply when the request sets none.
const DefaultMaxTokens = 16384

// New builds the provider name with key; baseURL overrides the endpoint ("" = apiloop.Providers').
func New(name, key, baseURL string) (apiloop.Provider, error) {
	info, ok := apiloop.Providers[name]
	if !ok {
		return nil, fmt.Errorf("unknown provider %q", name)
	}
	if baseURL == "" {
		baseURL = info.BaseURL
	}
	switch name {
	case "anthropic":
		opts := []aoption.RequestOption{aoption.WithAPIKey(key), aoption.WithMaxRetries(0)}
		if baseURL != "" {
			opts = append(opts, aoption.WithBaseURL(baseURL))
		}
		return anthropicP{anthropic.NewClient(opts...)}, nil
	case "gemini":
		cc := &genai.ClientConfig{APIKey: key, Backend: genai.BackendGeminiAPI}
		if baseURL != "" {
			cc.HTTPOptions.BaseURL = baseURL
		}
		c, err := genai.NewClient(context.Background(), cc)
		if err != nil {
			return nil, err
		}
		return geminiP{c}, nil
	}
	opts := []ooption.RequestOption{ooption.WithAPIKey(key), ooption.WithMaxRetries(0)}
	if baseURL != "" {
		opts = append(opts, ooption.WithBaseURL(baseURL))
	}
	if name == "openai" {
		return responsesP{openai.NewClient(opts...)}, nil
	}
	return chatP{openai.NewClient(opts...), name == "openrouter"}, nil
}

func maxTokens(r apiloop.Request) int64 {
	if r.MaxTokens > 0 {
		return int64(r.MaxTokens)
	}
	return DefaultMaxTokens
}

func schema(s json.RawMessage) map[string]any {
	m := map[string]any{}
	if len(s) > 0 {
		_ = json.Unmarshal(s, &m)
	}
	if m["type"] == nil {
		m["type"] = "object"
	}
	return m
}

func args(a json.RawMessage) string {
	if len(a) == 0 {
		return "{}"
	}
	return string(a)
}

func retryAfter(h http.Header) time.Duration {
	if s, err := strconv.Atoi(strings.TrimSpace(h.Get("Retry-After"))); err == nil && s > 0 {
		return time.Duration(s) * time.Second
	}
	return 0
}

func statusErr(err error) error {
	if err == nil {
		return nil
	}
	var ae *anthropic.Error
	if errors.As(err, &ae) {
		se := &apiloop.StatusError{Code: ae.StatusCode, Err: err}
		if ae.Response != nil {
			se.RetryAfter = retryAfter(ae.Response.Header)
		}
		return se
	}
	var oe *openai.Error
	if errors.As(err, &oe) {
		se := &apiloop.StatusError{Code: oe.StatusCode, Err: err}
		if oe.Response != nil {
			se.RetryAfter = retryAfter(oe.Response.Header)
		}
		return se
	}
	var ge genai.APIError
	if errors.As(err, &ge) {
		return &apiloop.StatusError{Code: ge.Code, Err: err}
	}
	var gp *genai.APIError
	if errors.As(err, &gp) {
		return &apiloop.StatusError{Code: gp.Code, Err: err}
	}
	return err
}

type anthropicP struct{ c anthropic.Client }

func (p anthropicP) Stream(ctx context.Context, r apiloop.Request, onText func(string)) (apiloop.Reply, error) {
	params := anthropic.MessageNewParams{Model: r.Model, MaxTokens: maxTokens(r)}
	if r.System != "" {
		params.System = []anthropic.TextBlockParam{{Text: r.System, CacheControl: anthropic.NewCacheControlEphemeralParam()}}
	}
	for _, t := range r.Tools {
		s := schema(t.Schema)
		in := anthropic.ToolInputSchemaParam{Properties: s["properties"]}
		if req, ok := s["required"].([]any); ok {
			for _, x := range req {
				if n, ok := x.(string); ok {
					in.Required = append(in.Required, n)
				}
			}
		}
		params.Tools = append(params.Tools, anthropic.ToolUnionParam{OfTool: &anthropic.ToolParam{Name: t.Name, Description: anthropic.String(t.Description), InputSchema: in}})
	}
	if n := len(params.Tools); n > 0 {
		params.Tools[n-1].OfTool.CacheControl = anthropic.NewCacheControlEphemeralParam()
	}
	for i, m := range r.Messages {
		var blocks []anthropic.ContentBlockParamUnion
		for _, pt := range m.Parts {
			switch {
			case pt.ToolCall != nil:
				var in any = map[string]any{}
				_ = json.Unmarshal([]byte(args(pt.ToolCall.Args)), &in)
				blocks = append(blocks, anthropic.NewToolUseBlock(pt.ToolCall.ID, in, pt.ToolCall.Name))
			case pt.ToolResult != nil:
				blocks = append(blocks, anthropic.NewToolResultBlock(pt.ToolResult.ID, pt.ToolResult.Output, pt.ToolResult.IsError))
			case pt.Text != "":
				blocks = append(blocks, anthropic.NewTextBlock(pt.Text))
			}
		}
		if len(blocks) == 0 {
			continue
		}
		if i == len(r.Messages)-1 {
			if cc := blocks[len(blocks)-1].GetCacheControl(); cc != nil {
				*cc = anthropic.NewCacheControlEphemeralParam()
			}
		}
		role := anthropic.MessageParamRoleUser
		if m.Role == apiloop.RoleAssistant {
			role = anthropic.MessageParamRoleAssistant
		}
		params.Messages = append(params.Messages, anthropic.MessageParam{Role: role, Content: blocks})
	}
	s := p.c.Messages.NewStreaming(ctx, params)
	defer s.Close()
	var msg anthropic.Message
	for s.Next() {
		ev := s.Current()
		if err := msg.Accumulate(ev); err != nil {
			return apiloop.Reply{}, err
		}
		if d, ok := ev.AsAny().(anthropic.ContentBlockDeltaEvent); ok && d.Delta.Text != "" {
			onText(d.Delta.Text)
		}
	}
	if err := s.Err(); err != nil {
		return apiloop.Reply{}, statusErr(err)
	}
	rep := apiloop.Reply{Usage: usage.Usage{Input: msg.Usage.InputTokens, Output: msg.Usage.OutputTokens,
		CacheRead: msg.Usage.CacheReadInputTokens, CacheWrite: msg.Usage.CacheCreationInputTokens}}
	for _, b := range msg.Content {
		switch v := b.AsAny().(type) {
		case anthropic.TextBlock:
			rep.Parts = append(rep.Parts, apiloop.Part{Text: v.Text})
		case anthropic.ToolUseBlock:
			rep.Parts = append(rep.Parts, apiloop.Part{ToolCall: &apiloop.ToolCall{ID: v.ID, Name: v.Name, Args: v.Input}})
		}
	}
	switch msg.StopReason {
	case anthropic.StopReasonToolUse:
		rep.Stop = apiloop.StopTool
	case anthropic.StopReasonMaxTokens:
		rep.Stop = apiloop.StopLimit
	default:
		rep.Stop = apiloop.StopEnd
	}
	return rep, nil
}

type responsesP struct{ c openai.Client }

func (p responsesP) Stream(ctx context.Context, r apiloop.Request, onText func(string)) (apiloop.Reply, error) {
	params := responses.ResponseNewParams{Model: r.Model, MaxOutputTokens: openai.Int(maxTokens(r)), Store: openai.Bool(false)}
	if r.System != "" {
		params.Instructions = openai.String(r.System)
	}
	for _, t := range r.Tools {
		params.Tools = append(params.Tools, responses.ToolUnionParam{OfFunction: &responses.FunctionToolParam{
			Name: t.Name, Description: openai.String(t.Description), Parameters: schema(t.Schema), Strict: openai.Bool(false)}})
	}
	var in responses.ResponseInputParam
	for _, m := range r.Messages {
		for _, pt := range m.Parts {
			switch {
			case pt.ToolCall != nil:
				in = append(in, responses.ResponseInputItemParamOfFunctionCall(args(pt.ToolCall.Args), pt.ToolCall.ID, pt.ToolCall.Name))
			case pt.ToolResult != nil:
				in = append(in, responses.ResponseInputItemParamOfFunctionCallOutput(pt.ToolResult.Output))
				in[len(in)-1].OfFunctionCallOutput.CallID = openai.String(pt.ToolResult.ID)
			case pt.Text != "":
				role := responses.EasyInputMessageRoleUser
				if m.Role == apiloop.RoleAssistant {
					role = responses.EasyInputMessageRoleAssistant
				}
				in = append(in, responses.ResponseInputItemParamOfMessage(pt.Text, role))
			}
		}
	}
	params.Input = responses.ResponseNewParamsInputUnion{OfInputItemList: in}
	s := p.c.Responses.NewStreaming(ctx, params)
	defer s.Close()
	var done *responses.Response
	for s.Next() {
		ev := s.Current()
		switch ev.Type {
		case "response.output_text.delta":
			onText(ev.Delta)
		case "response.completed", "response.incomplete":
			r := ev.Response
			done = &r
		case "response.failed", "error":
			return apiloop.Reply{}, fmt.Errorf("openai: %s %s", ev.Code, ev.Message)
		}
	}
	if err := s.Err(); err != nil {
		return apiloop.Reply{}, statusErr(err)
	}
	if done == nil {
		return apiloop.Reply{}, errors.New("openai: the stream ended without a response")
	}
	cached := done.Usage.InputTokensDetails.CachedTokens
	rep := apiloop.Reply{Stop: apiloop.StopEnd, Usage: usage.Usage{Input: done.Usage.InputTokens - cached, Output: done.Usage.OutputTokens, CacheRead: cached}}
	for _, it := range done.Output {
		switch it.Type {
		case "message":
			for _, c := range it.Content {
				if c.Type == "output_text" {
					rep.Parts = append(rep.Parts, apiloop.Part{Text: c.Text})
				}
			}
		case "function_call":
			rep.Parts = append(rep.Parts, apiloop.Part{ToolCall: &apiloop.ToolCall{ID: it.CallID, Name: it.Name, Args: json.RawMessage(it.Arguments.OfString)}})
			rep.Stop = apiloop.StopTool
		}
	}
	if done.Status == "incomplete" && rep.Stop == apiloop.StopEnd {
		rep.Stop = apiloop.StopLimit
	}
	return rep, nil
}

type chatP struct {
	c          openai.Client
	reportCost bool
}

func (p chatP) Stream(ctx context.Context, r apiloop.Request, onText func(string)) (apiloop.Reply, error) {
	params := openai.ChatCompletionNewParams{Model: r.Model, MaxTokens: openai.Int(maxTokens(r)),
		StreamOptions: openai.ChatCompletionStreamOptionsParam{IncludeUsage: openai.Bool(true)}}
	if p.reportCost {
		params.SetExtraFields(map[string]any{"usage": map[string]any{"include": true}})
	}
	if r.System != "" {
		params.Messages = append(params.Messages, openai.SystemMessage(r.System))
	}
	for _, t := range r.Tools {
		params.Tools = append(params.Tools, openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name: t.Name, Description: openai.String(t.Description), Parameters: shared.FunctionParameters(schema(t.Schema))}))
	}
	for _, m := range r.Messages {
		if m.Role == apiloop.RoleAssistant {
			var a openai.ChatCompletionAssistantMessageParam
			var text strings.Builder
			for _, pt := range m.Parts {
				if pt.ToolCall != nil {
					a.ToolCalls = append(a.ToolCalls, openai.ChatCompletionMessageToolCallUnionParam{OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
						ID: pt.ToolCall.ID, Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{Name: pt.ToolCall.Name, Arguments: args(pt.ToolCall.Args)}}})
				}
				text.WriteString(pt.Text)
			}
			if text.Len() > 0 {
				a.Content.OfString = openai.String(text.String())
			}
			params.Messages = append(params.Messages, openai.ChatCompletionMessageParamUnion{OfAssistant: &a})
			continue
		}
		for _, pt := range m.Parts {
			switch {
			case pt.ToolResult != nil:
				params.Messages = append(params.Messages, openai.ToolMessage(pt.ToolResult.Output, pt.ToolResult.ID))
			case pt.Text != "":
				params.Messages = append(params.Messages, openai.UserMessage(pt.Text))
			}
		}
	}
	s := p.c.Chat.Completions.NewStreaming(ctx, params)
	defer s.Close()
	var acc openai.ChatCompletionAccumulator
	var cost *float64
	for s.Next() {
		ch := s.Current()
		acc.AddChunk(ch)
		for _, c := range ch.Choices {
			if c.Delta.Content != "" {
				onText(c.Delta.Content)
			}
		}
		if f, ok := ch.Usage.JSON.ExtraFields["cost"]; ok {
			if v, err := strconv.ParseFloat(f.Raw(), 64); err == nil {
				cost = &v
			}
		}
	}
	if err := s.Err(); err != nil {
		return apiloop.Reply{}, statusErr(err)
	}
	u := acc.Usage
	cached := u.PromptTokensDetails.CachedTokens
	rep := apiloop.Reply{Stop: apiloop.StopEnd, Usage: usage.Usage{Input: u.PromptTokens - cached, Output: u.CompletionTokens, CacheRead: cached, CostUSD: cost}}
	if len(acc.Choices) == 0 {
		return rep, nil
	}
	ch := acc.Choices[0]
	if ch.Message.Content != "" {
		rep.Parts = append(rep.Parts, apiloop.Part{Text: ch.Message.Content})
	}
	for _, tc := range ch.Message.ToolCalls {
		rep.Parts = append(rep.Parts, apiloop.Part{ToolCall: &apiloop.ToolCall{ID: tc.ID, Name: tc.Function.Name, Args: json.RawMessage(args(json.RawMessage(tc.Function.Arguments)))}})
		rep.Stop = apiloop.StopTool
	}
	if ch.FinishReason == "length" {
		rep.Stop = apiloop.StopLimit
	}
	return rep, nil
}

type geminiP struct{ c *genai.Client }

func (p geminiP) Stream(ctx context.Context, r apiloop.Request, onText func(string)) (apiloop.Reply, error) {
	cfg := &genai.GenerateContentConfig{MaxOutputTokens: int32(min(maxTokens(r), math.MaxInt32))} //nolint:gosec // clamped to int32 on this line
	if r.System != "" {
		cfg.SystemInstruction = genai.NewContentFromText(r.System, genai.RoleUser)
	}
	if len(r.Tools) > 0 {
		t := &genai.Tool{}
		for _, s := range r.Tools {
			t.FunctionDeclarations = append(t.FunctionDeclarations, &genai.FunctionDeclaration{Name: s.Name, Description: s.Description, ParametersJsonSchema: schema(s.Schema)})
		}
		cfg.Tools = []*genai.Tool{t}
	}
	var contents []*genai.Content
	for _, m := range r.Messages {
		c := &genai.Content{Role: genai.RoleUser}
		if m.Role == apiloop.RoleAssistant {
			c.Role = genai.RoleModel
		}
		for _, pt := range m.Parts {
			switch {
			case pt.ToolCall != nil:
				a := map[string]any{}
				_ = json.Unmarshal([]byte(args(pt.ToolCall.Args)), &a)
				c.Parts = append(c.Parts, &genai.Part{FunctionCall: &genai.FunctionCall{ID: pt.ToolCall.ID, Name: pt.ToolCall.Name, Args: a}})
			case pt.ToolResult != nil:
				key := "output"
				if pt.ToolResult.IsError {
					key = "error"
				}
				c.Parts = append(c.Parts, &genai.Part{FunctionResponse: &genai.FunctionResponse{ID: pt.ToolResult.ID, Name: pt.ToolResult.Name, Response: map[string]any{key: pt.ToolResult.Output}}})
			case pt.Text != "":
				c.Parts = append(c.Parts, &genai.Part{Text: pt.Text})
			}
		}
		if len(c.Parts) > 0 {
			contents = append(contents, c)
		}
	}
	rep := apiloop.Reply{Stop: apiloop.StopEnd}
	var text strings.Builder
	n := 0
	for resp, err := range p.c.Models.GenerateContentStream(ctx, r.Model, contents, cfg) {
		if err != nil {
			return apiloop.Reply{}, statusErr(err)
		}
		if u := resp.UsageMetadata; u != nil {
			cached := int64(u.CachedContentTokenCount)
			rep.Usage = usage.Usage{Input: int64(u.PromptTokenCount) - cached, Output: int64(u.CandidatesTokenCount) + int64(u.ThoughtsTokenCount), CacheRead: cached}
		}
		for _, cand := range resp.Candidates {
			if cand.FinishReason == genai.FinishReasonMaxTokens {
				rep.Stop = apiloop.StopLimit
			}
			if cand.Content == nil {
				continue
			}
			for _, pt := range cand.Content.Parts {
				switch {
				case pt.FunctionCall != nil:
					b, _ := json.Marshal(pt.FunctionCall.Args)
					id := pt.FunctionCall.ID
					if id == "" {
						n++
						id = fmt.Sprintf("call_%d", n)
					}
					rep.Parts = append(rep.Parts, apiloop.Part{ToolCall: &apiloop.ToolCall{ID: id, Name: pt.FunctionCall.Name, Args: b}})
					rep.Stop = apiloop.StopTool
				case pt.Text != "" && !pt.Thought:
					onText(pt.Text)
					text.WriteString(pt.Text)
				}
			}
		}
	}
	if text.Len() > 0 {
		rep.Parts = append([]apiloop.Part{{Text: text.String()}}, rep.Parts...)
	}
	return rep, nil
}
