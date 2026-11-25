// Package models lists the models each agent offers, from what the agent itself reports: an ACP
// agent's session/new answer, a CLI's own list command, Claude Code's aliases, or an API
// provider's models endpoint. Lists are cached per agent for a day.
package models

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Zovaris/smithy/internal/acp"
	"github.com/Zovaris/smithy/internal/agents"
	"github.com/Zovaris/smithy/internal/apiloop"
)

// Model is one model an agent can run. Default marks the one it starts with when none is given;
// the prices (USD per million tokens) come only when the source states them.
type Model struct {
	ID      string   `json:"id"`
	Name    string   `json:"name,omitempty"`
	Default bool     `json:"default,omitempty"`
	InPerM  *float64 `json:"in_per_m,omitempty"`
	OutPerM *float64 `json:"out_per_m,omitempty"`
}

// List is an agent's models, the default first, where they came from and when.
type List struct {
	Agent   string    `json:"agent"`
	Models  []Model   `json:"models"`
	Source  string    `json:"source"`
	Fetched time.Time `json:"fetched"`
	Error   string    `json:"error,omitempty"` // the last refresh failed; Models are the cached ones
}

// MaxAge is how long a cached list is used before it is fetched again.
const MaxAge = 24 * time.Hour

// Deps is what discovery runs: a list command, an ACP probe, an HTTP GET, the stored API key for
// a smithy auth service, and Claude Code's configured model. Dir holds the cache.
type Deps struct {
	Dir         string
	Now         func() time.Time
	Output      func(ctx context.Context, argv ...string) ([]byte, error)
	ACP         func(ctx context.Context, argv []string) (acp.ModelList, error)
	Get         func(ctx context.Context, url string, h http.Header) ([]byte, error)
	Key         func(service string) string
	ClaudeModel func() string
}

// ClaudeAliases are the model names Claude Code accepts for its current models.
var ClaudeAliases = []string{"sonnet", "opus", "haiku"}

// listCmd is the command a CLI lists its models with, for the ones that have one.
var listCmd = map[string][]string{"opencode": {"opencode", "models"}}

var validAgent = regexp.MustCompile(`^(api:)?[a-z][a-z0-9-]*$`)

// Discover asks agent for its models now, without the cache.
func Discover(ctx context.Context, d Deps, agent string) (List, error) {
	l := List{Agent: agent, Models: []Model{}, Fetched: d.Now()}
	if !validAgent.MatchString(agent) {
		return l, fmt.Errorf("unknown agent %q", agent)
	}
	if p, ok := strings.CutPrefix(agent, "api:"); ok {
		ms, err := apiModels(ctx, d, p)
		l.Source = "the " + p + " models endpoint"
		if err != nil {
			return l, err
		}
		l.Models = ms
		return l, nil
	}
	if !slices.Contains(agents.CLIs, agent) {
		return l, fmt.Errorf("unknown agent %q", agent)
	}
	if agent == "claude" {
		l.Source = "Claude Code aliases"
		if d.ClaudeModel != nil {
			if id := strings.TrimSpace(d.ClaudeModel()); id != "" && !slices.Contains(ClaudeAliases, id) {
				l.Models = append(l.Models, Model{ID: id, Default: true})
			}
		}
		for _, a := range ClaudeAliases {
			l.Models = append(l.Models, Model{ID: a})
		}
		return l, nil
	}
	var errs []error
	if argv, ok := agents.ACPCLIs[agent]; ok {
		ml, err := d.ACP(ctx, argv)
		if err == nil && len(ml.Models) > 0 {
			l.Source = "ACP session/new"
			for _, m := range ml.Models {
				l.Models = append(l.Models, Model{ID: m.ID, Name: m.Name, Default: m.ID == ml.Current})
			}
			l.Models = defaultFirst(l.Models)
			return l, nil
		}
		errs = append(errs, err)
	}
	if argv, ok := listCmd[agent]; ok {
		out, err := d.Output(ctx, argv...)
		if err == nil {
			l.Source = strings.Join(argv, " ")
			for _, line := range strings.Split(string(out), "\n") {
				if id := strings.TrimSpace(line); id != "" && !strings.ContainsAny(id, " \t") {
					l.Models = append(l.Models, Model{ID: id})
				}
			}
			return l, nil
		}
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return l, errors.Join(errs...)
	}
	l.Source = agent + " has no list command"
	return l, nil
}

func defaultFirst(ms []Model) []Model {
	slices.SortStableFunc(ms, func(a, b Model) int {
		switch {
		case a.Default == b.Default:
			return 0
		case a.Default:
			return -1
		}
		return 1
	})
	return ms
}

func apiModels(ctx context.Context, d Deps, p string) ([]Model, error) {
	prov, ok := apiloop.Providers[p]
	if !ok {
		return nil, fmt.Errorf("unknown provider %q", p)
	}
	key := ""
	if prov.Service != "" {
		if key = strings.TrimSpace(d.Key(prov.Service)); key == "" && p != "openrouter" {
			return nil, fmt.Errorf("no %s key; store one with: smithy auth set %s", p, prov.Service)
		}
	}
	h := http.Header{}
	base := strings.TrimRight(prov.BaseURL, "/")
	var u string
	switch p {
	case "anthropic":
		u = base + "/v1/models?limit=1000"
		h.Set("X-Api-Key", key)
		h.Set("Anthropic-Version", "2023-06-01")
	case "gemini":
		u = base + "/v1beta/models?pageSize=1000"
		h.Set("X-Goog-Api-Key", key)
	default:
		u = base + "/models"
		if key != "" {
			h.Set("Authorization", "Bearer "+key)
		}
	}
	b, err := d.Get(ctx, u, h)
	if err != nil {
		return nil, err
	}
	var f struct {
		Data []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Display string `json:"display_name"`
			Pricing *struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
			} `json:"pricing"`
		} `json:"data"`
		Models []struct {
			Name    string   `json:"name"`
			Display string   `json:"displayName"`
			Methods []string `json:"supportedGenerationMethods"`
		} `json:"models"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s models: %w", p, err)
	}
	out := []Model{}
	for _, m := range f.Data {
		if m.ID == "" {
			continue
		}
		x := Model{ID: m.ID, Name: m.Display}
		if p == "openrouter" {
			x.Name = m.Name
		}
		if m.Pricing != nil {
			x.InPerM, x.OutPerM = perM(m.Pricing.Prompt), perM(m.Pricing.Completion)
		}
		out = append(out, x)
	}
	for _, m := range f.Models {
		if id, ok := strings.CutPrefix(m.Name, "models/"); ok && slices.Contains(m.Methods, "generateContent") {
			out = append(out, Model{ID: id, Name: m.Display})
		}
	}
	if p != "anthropic" && p != "openrouter" {
		slices.SortFunc(out, func(a, b Model) int { return strings.Compare(a.ID, b.ID) })
	}
	return out, nil
}

func perM(s string) *float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return nil
	}
	v *= 1e6
	return &v
}

var (
	locksMu sync.Mutex
	locks   = map[string]*sync.Mutex{}
)

func lock(agent string) *sync.Mutex {
	locksMu.Lock()
	defer locksMu.Unlock()
	if locks[agent] == nil {
		locks[agent] = &sync.Mutex{}
	}
	return locks[agent]
}

func cacheFile(d Deps, agent string) string {
	return filepath.Join(d.Dir, url.PathEscape(strings.ReplaceAll(agent, ":", "-"))+".json")
}

// Get is agent's cached list, fetched anew when refresh is set, the cache is missing or a day
// old. A failed fetch keeps the cached list and says why in Error.
func Get(ctx context.Context, d Deps, agent string, refresh bool) List {
	mu := lock(agent)
	mu.Lock()
	defer mu.Unlock()
	var cached List
	have := false
	if validAgent.MatchString(agent) {
		if b, err := os.ReadFile(cacheFile(d, agent)); err == nil && json.Unmarshal(b, &cached) == nil && cached.Agent == agent {
			have = true
			if !refresh && d.Now().Sub(cached.Fetched) < MaxAge && d.Now().Sub(cached.Fetched) >= 0 {
				return cached
			}
		}
	}
	l, err := Discover(ctx, d, agent)
	if err != nil {
		if have {
			cached.Error = err.Error()
			return cached
		}
		l.Error = err.Error()
		return l
	}
	if b, err := json.Marshal(l); err == nil && os.MkdirAll(d.Dir, 0o700) == nil {
		tmp := cacheFile(d, agent) + ".tmp"
		if os.WriteFile(tmp, b, 0o600) == nil {
			_ = os.Rename(tmp, cacheFile(d, agent))
		}
	}
	return l
}
