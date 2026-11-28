package apiloop

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/layout"
	"github.com/Zovaris/smithy/internal/usage"
)

// feedURL is OpenRouter's public model list: per-token USD prices for every model it routes,
// including the first-party ones under their vendor prefix. No key needed.
const feedURL = "https://openrouter.ai/api/v1/models"

const feedMaxAge = 24 * time.Hour

// vendor is each provider's prefix in the feed; a provider not listed (ollama) is never priced.
var vendor = map[string]string{
	"anthropic":  "anthropic/",
	"openai":     "openai/",
	"gemini":     "google/",
	"deepseek":   "deepseek/",
	"openrouter": "",
}

type price struct{ in, out, read, write float64 }

var (
	feedPath  = func() string { return filepath.Join(layout.Home(), "cache", "model-prices.json") }
	fetchFeed = fetchOpenRouter

	pricesMu sync.Mutex
	loaded   string
	prices   map[string]price
)

func resetPrices() {
	pricesMu.Lock()
	loaded, prices = "", nil
	pricesMu.Unlock()
}

func fetchOpenRouter() ([]byte, error) {
	if testing.Testing() {
		return nil, errors.New("no network in tests")
	}
	c := &http.Client{Timeout: 10 * time.Second}
	resp, err := c.Get(feedURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pricing feed: %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 32<<20))
}

// parseFeed keys prices by the feed id with dots turned into dashes, so "claude-sonnet-4.5" and
// the provider's own "claude-sonnet-4-5" meet. Negative or unparsable prices (routers such as
// openrouter/auto) are left out.
func parseFeed(b []byte) (map[string]price, error) {
	var f struct {
		Data []struct {
			ID      string `json:"id"`
			Pricing struct {
				Prompt, Completion string
				Read               string `json:"input_cache_read"`
				Write              string `json:"input_cache_write"`
			} `json:"pricing"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	if len(f.Data) == 0 {
		return nil, errors.New("pricing feed: no models")
	}
	per := func(s string) (float64, bool) {
		if s == "" {
			return 0, true
		}
		v, err := strconv.ParseFloat(s, 64)
		return v * 1e6, err == nil && v >= 0
	}
	m := make(map[string]price, len(f.Data))
	for _, d := range f.Data {
		in, ok1 := per(d.Pricing.Prompt)
		out, ok2 := per(d.Pricing.Completion)
		rd, ok3 := per(d.Pricing.Read)
		wr, ok4 := per(d.Pricing.Write)
		if d.Pricing.Prompt == "" || !ok1 || !ok2 || !ok3 || !ok4 {
			continue
		}
		m[key(d.ID)] = price{in, out, rd, wr}
	}
	return m, nil
}

func key(id string) string { return strings.ToLower(strings.ReplaceAll(id, ".", "-")) }

// loadPrices reads the cached feed (once per cache path, so a new SMITHY_HOME is read anew); once it is a day old it fetches a new one and keeps the old
// cache when the fetch or the answer fails (offline). nil when neither exists. The fetch runs
// outside the lock: other callers meanwhile get the old cache (or nil) instead of waiting.
func loadPrices() map[string]price {
	pricesMu.Lock()
	path := feedPath()
	if loaded == path {
		defer pricesMu.Unlock()
		return prices
	}
	loaded, prices = path, nil
	cached, err := os.ReadFile(path)
	if err == nil {
		prices, _ = parseFeed(cached)
		if st, err := os.Stat(path); err == nil && time.Since(st.ModTime()) < feedMaxAge && prices != nil {
			defer pricesMu.Unlock()
			return prices
		}
	}
	old := prices
	pricesMu.Unlock()
	b, err := fetchFeed()
	if err != nil {
		return old
	}
	m, err := parseFeed(b)
	if err != nil {
		return old
	}
	if os.MkdirAll(filepath.Dir(path), 0o700) == nil {
		tmp := path + ".tmp"
		if os.WriteFile(tmp, b, 0o600) == nil {
			_ = os.Rename(tmp, path)
		}
	}
	pricesMu.Lock()
	defer pricesMu.Unlock()
	if loaded == path {
		prices = m
	}
	return m
}

// snapshot is what may follow a listed model id: a date (-20250929, -2025-08-07) or -latest.
var snapshot = regexp.MustCompile(`-(\d{8}|\d{4}-\d{2}-\d{2}|latest)$`)

// Cost prices u at the feed's price for provider's model: the id must equal a listed one, or be
// one followed by a snapshot date. nil (unknown cost) for any other model, never a guess.
func Cost(provider, model string, u usage.Usage) *float64 {
	pre, ok := vendor[provider]
	if !ok {
		return nil
	}
	m := loadPrices()
	p, ok := m[key(pre+model)]
	if !ok {
		p, ok = m[key(pre+snapshot.ReplaceAllString(model, ""))]
	}
	if !ok {
		return nil
	}
	c := (float64(u.Input)*p.in + float64(u.Output)*p.out + float64(u.CacheRead)*p.read + float64(u.CacheWrite)*p.write) / 1e6
	return &c
}
