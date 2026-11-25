//go:build harness

package codemode

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/apiloop"
	"github.com/Zovaris/smithy/internal/usage"
)

type sinkRec struct{ events []string }

func (s *sinkRec) Text(string) {}
func (s *sinkRec) Tool(c apiloop.ToolCall, r apiloop.ToolResult) {
	s.events = append(s.events, "tool:"+c.Name)
}
func (s *sinkRec) Permission(c apiloop.ToolCall, ok bool, why string) {
	s.events = append(s.events, "permission:"+c.Name)
}
func (s *sinkRec) Usage(usage.Usage) {}

func TestInnerCallsReachTheSink(t *testing.T) {
	sink := &sinkRec{}
	ctx := apiloop.WithSink(context.Background(), sink)
	args, _ := json.Marshal(map[string]string{"code": `tools.search_issues({query: "x"}); try { tools.delete_repo({}) } catch (e) {}`})
	if _, err := Wrap(&fakeTools{deny: "delete_repo"}, Limits{}).Call(ctx, apiloop.ToolCall{ID: "1", Name: ToolName, Args: args}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(sink.events, ","); got != "tool:search_issues,permission:delete_repo" {
		t.Fatalf("sink events %q", got)
	}
}

func TestMemoryCapIsTheDocumentedLimit(t *testing.T) {
	_, err := run(t, &fakeTools{}, `let a = []; while (true) { a.push(new Uint8Array(1 << 20)) }`, Limits{Time: 20 * time.Second})
	if err == nil || !strings.Contains(err.Error(), "memory") {
		t.Fatalf("want a memory-limit error, got %v", err)
	}
	var grown uint64
	for _, size := range []uint64{32 << 20, 64 << 20, 65 << 20, 128 << 20} {
		l := capAlloc(defaultMemory).Allocate(0, 1<<32)
		if l.Reallocate(size) != nil {
			grown = size
		}
	}
	if grown != 64<<20 {
		t.Fatalf("memory grew to %d bytes, want at most %d", grown, defaultMemory)
	}
}
