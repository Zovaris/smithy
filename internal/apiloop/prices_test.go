package apiloop

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/usage"
)

const feed = `{"data":[
 {"id":"anthropic/claude-sonnet-4.5","pricing":{"prompt":"0.000003","completion":"0.000015","input_cache_read":"0.0000003","input_cache_write":"0.00000375"}},
 {"id":"anthropic/claude-3.5-haiku","pricing":{"prompt":"0.0000008","completion":"0.000004"}},
 {"id":"openai/gpt-5","pricing":{"prompt":"0.00000125","completion":"0.00001","input_cache_read":"0.000000125"}},
 {"id":"google/gemini-2.5-pro","pricing":{"prompt":"0.00000125","completion":"0.00001"}},
 {"id":"deepseek/deepseek-chat","pricing":{"prompt":"0.0000003","completion":"0.0000012"}},
 {"id":"openrouter/auto","pricing":{"prompt":"-1","completion":"-1"}},
 {"id":"meta-llama/llama-4-maverick","pricing":{"prompt":"0.00000015","completion":"0.0000006"}}
]}`

func useFeed(t *testing.T, fetch func() ([]byte, error)) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "prices.json")
	old, oldPath := fetchFeed, feedPath
	fetchFeed, feedPath = fetch, func() string { return path }
	resetPrices()
	t.Cleanup(func() { fetchFeed, feedPath = old, oldPath; resetPrices() })
	return path
}

func near(c *float64, want float64) bool { return c != nil && *c > want-0.001 && *c < want+0.001 }

func TestCostFromFeed(t *testing.T) {
	useFeed(t, func() ([]byte, error) { return []byte(feed), nil })
	u := usage.Usage{Input: 1_000_000, Output: 1_000_000, CacheRead: 1_000_000, CacheWrite: 1_000_000}
	for _, tc := range []struct {
		provider, model string
		want            float64
	}{
		{"anthropic", "claude-sonnet-4-5", 22.05},
		{"anthropic", "claude-sonnet-4-5-20250929", 22.05},
		{"anthropic", "claude-3-5-haiku-latest", 4.8},
		{"openai", "gpt-5", 11.375},
		{"openai", "gpt-5-2025-08-07", 11.375},
		{"gemini", "gemini-2.5-pro", 11.25},
		{"deepseek", "deepseek-chat", 1.5},
		{"openrouter", "meta-llama/llama-4-maverick", 0.75},
		{"openrouter", "anthropic/claude-sonnet-4.5", 22.05},
	} {
		if c := Cost(tc.provider, tc.model, u); !near(c, tc.want) {
			t.Errorf("%s/%s: cost %v, want %v", tc.provider, tc.model, c, tc.want)
		}
	}
	for _, tc := range [][2]string{{"openai", "gpt-5-pro"}, {"anthropic", "claude-opus-4-7"}, {"openrouter", "openrouter/auto"}, {"ollama", "llama3"}, {"openai", "claude-sonnet-4-5"}} {
		if c := Cost(tc[0], tc[1], u); c != nil {
			t.Errorf("%s/%s: unknown model priced at %v", tc[0], tc[1], *c)
		}
	}
}

func TestFeedIsCachedForADay(t *testing.T) {
	calls := 0
	path := useFeed(t, func() ([]byte, error) { calls++; return []byte(feed), nil })
	u := usage.Usage{Input: 1_000_000}
	Cost("openai", "gpt-5", u)
	resetPrices()
	if c := Cost("openai", "gpt-5", u); !near(c, 1.25) || calls != 1 {
		t.Fatalf("fresh cache: cost %v, fetches %d", c, calls)
	}
	old := time.Now().Add(-25 * time.Hour)
	os.Chtimes(path, old, old)
	resetPrices()
	Cost("openai", "gpt-5", u)
	if calls != 2 {
		t.Fatalf("stale cache not refreshed: fetches %d", calls)
	}
}

func TestOfflineFallsBackToLastCache(t *testing.T) {
	path := useFeed(t, func() ([]byte, error) { return nil, errors.New("offline") })
	u := usage.Usage{Input: 1_000_000}
	if c := Cost("openai", "gpt-5", u); c != nil {
		t.Fatalf("no feed and no cache priced at %v", *c)
	}
	os.WriteFile(path, []byte(feed), 0o600)
	old := time.Now().Add(-30 * 24 * time.Hour)
	os.Chtimes(path, old, old)
	resetPrices()
	if c := Cost("openai", "gpt-5", u); !near(c, 1.25) {
		t.Fatalf("offline with old cache: cost %v", c)
	}
}

func TestBadFeedKeepsCache(t *testing.T) {
	path := useFeed(t, func() ([]byte, error) { return []byte("<html>"), nil })
	os.WriteFile(path, []byte(feed), 0o600)
	old := time.Now().Add(-48 * time.Hour)
	os.Chtimes(path, old, old)
	if c := Cost("openai", "gpt-5", usage.Usage{Input: 1_000_000}); !near(c, 1.25) {
		t.Fatalf("bad feed replaced cache: cost %v", c)
	}
	if b, _ := os.ReadFile(path); string(b) != feed {
		t.Fatal("cache overwritten by a bad feed")
	}
}

func TestCostDoesNotWaitOnAnotherCallersFetch(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	useFeed(t, func() ([]byte, error) { close(started); <-release; return []byte(feed), nil })
	u := usage.Usage{Input: 1_000_000}
	first := make(chan *float64)
	go func() { first <- Cost("anthropic", "claude-sonnet-4-5", u) }()
	<-started
	second := make(chan *float64)
	go func() { second <- Cost("anthropic", "claude-sonnet-4-5", u) }()
	select {
	case <-second:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("a second caller waited for the first one's fetch")
	}
	close(release)
	if c := <-first; !near(c, 3) {
		t.Fatalf("fetching caller got %v", c)
	}
}
