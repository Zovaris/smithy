//go:build harness

package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/apiloop"
)

const testKey = "test-placeholder"

type seen struct {
	auth string
	body map[string]any
	path string
}

func sse(t *testing.T, events []string, got *seen) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got.path = r.URL.Path
		got.auth = r.Header.Get("x-api-key") + r.Header.Get("Authorization") + r.Header.Get("x-goog-api-key")
		_ = json.Unmarshal(b, &got.body)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range events {
			io.WriteString(w, e+"\n\n")
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func req() apiloop.Request {
	call := apiloop.ToolCall{ID: "c0", Name: "echo", Args: json.RawMessage(`{"s":0}`)}
	return apiloop.Request{Model: "m-1", System: "be brief", MaxTokens: 100,
		Tools: []apiloop.ToolSpec{{Name: "echo", Description: "echo s", Schema: json.RawMessage(`{"type":"object","properties":{"s":{"type":"integer"}},"required":["s"]}`)}},
		Messages: []apiloop.Message{
			apiloop.UserText("hi"),
			{Role: apiloop.RoleAssistant, Parts: []apiloop.Part{{ToolCall: &call}}},
			{Role: apiloop.RoleUser, Parts: []apiloop.Part{{ToolResult: &apiloop.ToolResult{ID: "c0", Name: "echo", Output: "0"}}}},
		}}
}

func check(t *testing.T, p apiloop.Provider, wantStop string) apiloop.Reply {
	t.Helper()
	var text strings.Builder
	rep, err := p.Stream(context.Background(), req(), func(s string) { text.WriteString(s) })
	if err != nil {
		t.Fatal(err)
	}
	if text.String() != "hello" {
		t.Fatalf("streamed %q", text.String())
	}
	if rep.Stop != wantStop || len(rep.Parts) != 2 || rep.Parts[0].Text != "hello" || rep.Parts[1].ToolCall == nil || rep.Parts[1].ToolCall.Name != "echo" {
		t.Fatalf("reply %+v", rep)
	}
	var a struct{ S int }
	if err := json.Unmarshal(rep.Parts[1].ToolCall.Args, &a); err != nil || a.S != 1 {
		t.Fatalf("args %s", rep.Parts[1].ToolCall.Args)
	}
	if rep.Usage.Input != 6 || rep.Usage.Output != 5 || rep.Usage.CacheRead != 4 {
		t.Fatalf("usage %+v", rep.Usage)
	}
	return rep
}

func TestAnthropicStreamsCachesAndParsesToolCalls(t *testing.T) {
	var got seen
	srv := sse(t, []string{
		`event: message_start` + "\n" + `data: {"type":"message_start","message":{"id":"m","type":"message","role":"assistant","model":"m-1","content":[],"stop_reason":null,"usage":{"input_tokens":6,"output_tokens":0,"cache_read_input_tokens":4,"cache_creation_input_tokens":0}}}`,
		`event: content_block_start` + "\n" + `data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hel"}}`,
		`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"lo"}}`,
		`event: content_block_stop` + "\n" + `data: {"type":"content_block_stop","index":0}`,
		`event: content_block_start` + "\n" + `data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"t1","name":"echo","input":{}}}`,
		`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"s\":1}"}}`,
		`event: content_block_stop` + "\n" + `data: {"type":"content_block_stop","index":1}`,
		`event: message_delta` + "\n" + `data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":5}}`,
		`event: message_stop` + "\n" + `data: {"type":"message_stop"}`,
	}, &got)
	p, _ := New("anthropic", testKey, srv.URL)
	check(t, p, apiloop.StopTool)
	if got.auth != testKey {
		t.Fatalf("auth header %q", got.auth)
	}
	b, _ := json.Marshal(got.body)
	if strings.Count(string(b), `"cache_control"`) < 3 {
		t.Fatalf("want cache_control on system, tools and the last message: %s", b)
	}
}

func TestResponsesStreamsAndParsesToolCalls(t *testing.T) {
	var got seen
	srv := sse(t, []string{
		`data: {"type":"response.output_text.delta","delta":"hel","item_id":"i","output_index":0,"content_index":0,"sequence_number":1}`,
		`data: {"type":"response.output_text.delta","delta":"lo","item_id":"i","output_index":0,"content_index":0,"sequence_number":2}`,
		`data: {"type":"response.completed","sequence_number":3,"response":{"id":"r","object":"response","status":"completed","model":"m-1","output":[{"type":"message","id":"i","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hello","annotations":[]}]},{"type":"function_call","id":"f","call_id":"c1","name":"echo","arguments":"{\"s\":1}","status":"completed"}],"usage":{"input_tokens":10,"input_tokens_details":{"cached_tokens":4},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":15}}}`,
	}, &got)
	p, _ := New("openai", testKey, srv.URL)
	check(t, p, apiloop.StopTool)
	if got.path != "/responses" || got.auth != "Bearer "+testKey {
		t.Fatalf("path %q auth %q", got.path, got.auth)
	}
	b, _ := json.Marshal(got.body["input"])
	if !strings.Contains(string(b), `"call_id":"c0"`) || !strings.Contains(string(b), `"function_call_output"`) {
		t.Fatalf("input %s", b)
	}
}

func TestChatCompatStreamsAndReadsOpenRouterCost(t *testing.T) {
	var got seen
	srv := sse(t, []string{
		`data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m-1","choices":[{"index":0,"delta":{"role":"assistant","content":"hel"}}]}`,
		`data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m-1","choices":[{"index":0,"delta":{"content":"lo"}}]}`,
		`data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m-1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"echo","arguments":"{\"s\":1}"}}]},"finish_reason":"tool_calls"}]}`,
		`data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"m-1","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15,"prompt_tokens_details":{"cached_tokens":4},"cost":0.25}}`,
		`data: [DONE]`,
	}, &got)
	p, _ := New("openrouter", testKey, srv.URL)
	rep := check(t, p, apiloop.StopTool)
	if rep.Usage.CostUSD == nil || *rep.Usage.CostUSD != 0.25 {
		t.Fatalf("cost %v", rep.Usage.CostUSD)
	}
	if got.path != "/chat/completions" || got.body["usage"] == nil {
		t.Fatalf("path %q body %v", got.path, got.body)
	}
}

func TestGeminiStreamsAndParsesToolCalls(t *testing.T) {
	var got seen
	srv := sse(t, []string{
		`data: {"candidates":[{"content":{"role":"model","parts":[{"text":"hel"}]}}]}`,
		`data: {"candidates":[{"content":{"role":"model","parts":[{"text":"lo"},{"functionCall":{"name":"echo","args":{"s":1}}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"cachedContentTokenCount":4}}`,
	}, &got)
	p, err := New("gemini", testKey, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	check(t, p, apiloop.StopTool)
	if !strings.Contains(got.path, "m-1:streamGenerateContent") || got.auth != testKey {
		t.Fatalf("path %q auth %q", got.path, got.auth)
	}
}

func TestRateLimitBecomesRetryableStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(429)
		io.WriteString(w, `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`)
	}))
	defer srv.Close()
	for _, name := range []string{"anthropic", "openai", "deepseek"} {
		p, _ := New(name, testKey, srv.URL)
		_, err := p.Stream(context.Background(), req(), func(string) {})
		var se *apiloop.StatusError
		if !errors.As(err, &se) || se.Code != 429 || se.RetryAfter != 7*time.Second || !apiloop.Retryable(err) {
			t.Fatalf("%s: %T %v", name, err, err)
		}
		if strings.Contains(err.Error(), testKey) {
			t.Fatalf("%s: key in error", name)
		}
	}
}

func TestBaseURLEnvVarsCannotRedirectTheKey(t *testing.T) {
	hit := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit <- r.URL.Path }))
	defer srv.Close()
	t.Setenv("ANTHROPIC_BASE_URL", srv.URL)
	t.Setenv("OPENAI_BASE_URL", srv.URL)
	t.Setenv("GOOGLE_GEMINI_BASE_URL", srv.URL)
	for _, name := range []string{"anthropic", "openai", "gemini"} {
		p, err := New(name, testKey, "")
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		p.Stream(ctx, req(), func(string) {})
		cancel()
	}
	select {
	case path := <-hit:
		t.Fatalf("a request went to the env var's host: %s", path)
	default:
	}
}
