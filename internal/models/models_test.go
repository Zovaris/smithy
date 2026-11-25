package models

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/acp"
)

func deps(t *testing.T) Deps {
	return Deps{
		Dir:    t.TempDir(),
		Now:    func() time.Time { return time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC) },
		Output: func(context.Context, ...string) ([]byte, error) { return nil, errors.New("no command") },
		ACP:    func(context.Context, []string) (acp.ModelList, error) { return acp.ModelList{}, errors.New("no acp") },
		Get:    func(context.Context, string, http.Header) ([]byte, error) { return nil, errors.New("no network") },
		Key:    func(string) string { return "" },
	}
}

func ids(l List) string {
	var s []string
	for _, m := range l.Models {
		s = append(s, m.ID)
	}
	return strings.Join(s, ",")
}

func TestClaudeAliases(t *testing.T) {
	l, err := Discover(context.Background(), deps(t), "claude")
	if err != nil || ids(l) != "sonnet,opus,haiku" {
		t.Fatalf("got %q %v", ids(l), err)
	}
}

func TestClaudeAddsConfiguredID(t *testing.T) {
	d := deps(t)
	d.ClaudeModel = func() string { return "claude-opus-x-1" }
	l, _ := Discover(context.Background(), d, "claude")
	if ids(l) != "claude-opus-x-1,sonnet,opus,haiku" || !l.Models[0].Default {
		t.Fatalf("got %q", ids(l))
	}
}

func TestACPAgentDefaultFirst(t *testing.T) {
	d := deps(t)
	d.ACP = func(_ context.Context, argv []string) (acp.ModelList, error) {
		if argv[0] != "gemini" {
			t.Fatalf("argv %v", argv)
		}
		return acp.ModelList{Current: "g-pro", Models: []acp.Model{{ID: "g-flash", Name: "Flash"}, {ID: "g-pro", Name: "Pro"}}}, nil
	}
	l, err := Discover(context.Background(), d, "gemini")
	if err != nil || ids(l) != "g-pro,g-flash" || !l.Models[0].Default || l.Models[0].Name != "Pro" {
		t.Fatalf("got %+v %v", l, err)
	}
}

func TestOpencodeFallsBackToListCommand(t *testing.T) {
	d := deps(t)
	d.Output = func(_ context.Context, argv ...string) ([]byte, error) {
		if strings.Join(argv, " ") != "opencode models" {
			t.Fatalf("argv %v", argv)
		}
		return []byte("oc/a\n\noc/b\n"), nil
	}
	l, err := Discover(context.Background(), d, "opencode")
	if err != nil || ids(l) != "oc/a,oc/b" || l.Source != "opencode models" {
		t.Fatalf("got %+v %v", l, err)
	}
}

func TestNoListCommand(t *testing.T) {
	l, err := Discover(context.Background(), deps(t), "codex")
	if err != nil || len(l.Models) != 0 {
		t.Fatalf("got %+v %v", l, err)
	}
}

func TestAPIProviders(t *testing.T) {
	cases := map[string]struct{ url, body, want, hdr string }{
		"anthropic":  {"https://api.anthropic.com/v1/models?limit=1000", `{"data":[{"id":"c-1","display_name":"C One"}]}`, "c-1", "X-Api-Key"},
		"openai":     {"https://api.openai.com/v1/models", `{"data":[{"id":"o-2"},{"id":"o-1"}]}`, "o-1,o-2", "Authorization"},
		"gemini":     {"https://generativelanguage.googleapis.com/v1beta/models?pageSize=1000", `{"models":[{"name":"models/g-1","displayName":"G","supportedGenerationMethods":["generateContent"]},{"name":"models/emb","supportedGenerationMethods":["embedContent"]}]}`, "g-1", "X-Goog-Api-Key"},
		"deepseek":   {"https://api.deepseek.com/v1/models", `{"data":[{"id":"ds"}]}`, "ds", "Authorization"},
		"openrouter": {"https://openrouter.ai/api/v1/models", `{"data":[{"id":"x/y","name":"Y","pricing":{"prompt":"0.000003","completion":"0.000015"}}]}`, "x/y", ""},
	}
	for p, c := range cases {
		d := deps(t)
		d.Key = func(string) string { return "k" }
		d.Get = func(_ context.Context, u string, h http.Header) ([]byte, error) {
			if u != c.url {
				t.Fatalf("%s: url %s", p, u)
			}
			if c.hdr != "" && h.Get(c.hdr) == "" {
				t.Fatalf("%s: no %s header", p, c.hdr)
			}
			return []byte(c.body), nil
		}
		l, err := Discover(context.Background(), d, "api:"+p)
		if err != nil || ids(l) != c.want {
			t.Fatalf("%s: got %q %v", p, ids(l), err)
		}
		if p == "openrouter" && (l.Models[0].InPerM == nil || *l.Models[0].InPerM != 3 || *l.Models[0].OutPerM != 15) {
			t.Fatalf("openrouter prices %+v", l.Models[0])
		}
	}
}

func TestAPINeedsKey(t *testing.T) {
	if _, err := Discover(context.Background(), deps(t), "api:openai"); err == nil || !strings.Contains(err.Error(), "smithy auth set openai") {
		t.Fatalf("err %v", err)
	}
}

func TestUnknownAgent(t *testing.T) {
	if _, err := Discover(context.Background(), deps(t), "../x"); err == nil {
		t.Fatal("want error")
	}
}

func TestCacheDailyAndRefresh(t *testing.T) {
	d := deps(t)
	n := 0
	d.Output = func(context.Context, ...string) ([]byte, error) { n++; return []byte("oc/a\n"), nil }
	ctx := context.Background()
	Get(ctx, d, "opencode", false)
	Get(ctx, d, "opencode", false)
	if n != 1 {
		t.Fatalf("fetched %d times, want 1 (cached)", n)
	}
	Get(ctx, d, "opencode", true)
	if n != 2 {
		t.Fatalf("refresh did not fetch: %d", n)
	}
	now := d.Now()
	d.Now = func() time.Time { return now.Add(25 * time.Hour) }
	Get(ctx, d, "opencode", false)
	if n != 3 {
		t.Fatalf("a day-old cache was not refreshed: %d", n)
	}
	if _, err := filepath.Glob(filepath.Join(d.Dir, "*.json")); err != nil {
		t.Fatal(err)
	}
}

func TestCacheKeptWhenRefreshFails(t *testing.T) {
	d := deps(t)
	d.Output = func(context.Context, ...string) ([]byte, error) { return []byte("oc/a\n"), nil }
	ctx := context.Background()
	Get(ctx, d, "opencode", false)
	d.Output = func(context.Context, ...string) ([]byte, error) { return nil, errors.New("offline") }
	d.ACP = func(context.Context, []string) (acp.ModelList, error) { return acp.ModelList{}, errors.New("offline") }
	l := Get(ctx, d, "opencode", true)
	if ids(l) != "oc/a" || l.Error == "" {
		t.Fatalf("got %+v", l)
	}
}
