//go:build harness

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/agents"
	"github.com/Zovaris/smithy/internal/apiloop"
	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/models"
	"github.com/Zovaris/smithy/internal/text"
)

// Setup is GET /v1/setup: what first run and the pickers show.
type Setup struct {
	Agents    []agents.Ready            `json:"agents"`
	Default   AgentChoice               `json:"default"`
	Chosen    bool                      `json:"chosen"` // false on first run: no agent picked or used yet
	Kit       KitChoice                 `json:"kit"`
	Skills    []string                  `json:"skills"` // installed skills the kit can link in
	Harnesses []agents.HarnessInventory `json:"harnesses"`
}

// AgentChoice is the agent (and model, for a CLI) new sessions start with.
type AgentChoice struct {
	Agent string `json:"agent"`
	Model string `json:"model,omitempty"`
}

// KitChoice is what goes into every driven CLI (kit.mcp, kit.skills).
type KitChoice struct {
	MCP    bool     `json:"mcp"`
	Skills []string `json:"skills"`
}

const skillsInstallTimeout = 3 * time.Minute

func (sv *Server) ready() []agents.Ready {
	if sv.Ready != nil {
		return sv.Ready()
	}
	return agents.DetectReady(agents.DefaultReadyDeps())
}

func home() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return h
}

// installedSkills are the Known skills FindSkill finds, the ones the kit may link in.
func installedSkills() []string {
	out := []string{}
	for _, s := range agents.Known {
		if _, ok := agents.FindSkill(s); ok {
			out = append(out, s)
		}
	}
	return out
}

// agentModels is GET /v1/agents/{id}/models[?refresh=1]: the models agent id reports, cached
// for a day on this server.
func (sv *Server) agentModels(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.PathValue("id"), "acp:")
	p, api := strings.CutPrefix(id, "api:")
	if _, known := apiloop.Providers[p]; !(api && known) && !slices.Contains(agents.CLIs, id) {
		writeJSON(w, 404, map[string]string{"error": "no such agent: " + text.Clean(id)})
		return
	}
	get := sv.Models
	if get == nil {
		get = func(ctx context.Context, agent string, refresh bool) models.List {
			return models.Get(ctx, models.DefaultDeps(), agent, refresh)
		}
	}
	writeJSON(w, 200, get(r.Context(), id, r.URL.Query().Get("refresh") == "1"))
}

func (sv *Server) setup(w http.ResponseWriter, r *http.Request) {
	s := Setup{Agents: sv.ready(), Skills: installedSkills(), Harnesses: agents.Inventory(home(), os.Getenv)}
	s.Default.Agent, s.Default.Model = sv.Defaults()
	s.Chosen = s.Default.Agent != ""
	if s.Default.Agent == "" {
		if i := slices.IndexFunc(s.Agents, func(a agents.Ready) bool { return a.Ready && a.Kind == "cli" }); i >= 0 {
			s.Default.Agent = s.Agents[i].Name
		}
	}
	s.Kit.MCP, s.Kit.Skills = agents.KitChoice()
	if s.Kit.Skills == nil {
		s.Kit.Skills = []string{}
	}
	writeJSON(w, 200, s)
}

// SetDefault makes agent and model what new sessions start with, now and after a restart.
func (sv *Server) SetDefault(agent, model string) error {
	if agent == "" {
		return errors.New("agent: required")
	}
	if err := ValidAgent(agent); err != nil {
		return err
	}
	if strings.HasPrefix(agent, "api:") {
		model = ""
	}
	if err := ValidModel(model); err != nil {
		return err
	}
	f := config.UserFile()
	if err := config.Set(f, "session.agent", agent); err != nil {
		return err
	}
	if model == "" {
		if _, err := config.Unset(f, "session.model"); err != nil {
			return err
		}
	} else if err := config.Set(f, "session.model", model); err != nil {
		return err
	}
	sv.mu.Lock()
	sv.lastAgent, sv.lastModel = agent, model
	sv.mu.Unlock()
	return nil
}

func (sv *Server) setDefault(w http.ResponseWriter, r *http.Request) {
	if code, msg := sameOrigin(r); code != 0 {
		writeJSON(w, code, map[string]string{"error": msg})
		return
	}
	var req AgentChoice
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "body must be {\"agent\":…,\"model\":…}"})
		return
	}
	if err := sv.SetDefault(req.Agent, req.Model); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	a, m := sv.Defaults()
	writeJSON(w, 200, AgentChoice{Agent: a, Model: m})
}

func (sv *Server) setKit(w http.ResponseWriter, r *http.Request) {
	if !localOnly(w, r) {
		return
	}
	if code, msg := sameOrigin(r); code != 0 {
		writeJSON(w, code, map[string]string{"error": msg})
		return
	}
	var req KitChoice
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "body must be {\"mcp\":true|false,\"skills\":[…]}"})
		return
	}
	have := installedSkills()
	for _, s := range req.Skills {
		if !slices.Contains(have, s) {
			writeJSON(w, 400, map[string]string{"error": "skills: " + text.Clean(s) + " is not an installed smithy skill"})
			return
		}
	}
	f := config.UserFile()
	mcp := "true"
	if !req.MCP {
		mcp = "false"
	}
	if err := config.Set(f, "kit.mcp", mcp); err != nil {
		writeJSON(w, 500, map[string]string{"error": "settings not saved"})
		return
	}
	var err error
	if len(req.Skills) == 0 {
		_, err = config.Unset(f, "kit.skills")
	} else {
		err = config.Set(f, "kit.skills", strings.Join(req.Skills, ","))
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "settings not saved"})
		return
	}
	var out KitChoice
	out.MCP, out.Skills = agents.KitChoice()
	if out.Skills == nil {
		out.Skills = []string{}
	}
	writeJSON(w, 200, out)
}

// installSkills runs `smithy skills install` (checksum-checked download of this release's
// skills) for every detected harness, or only req.harness.
func (sv *Server) installSkills(w http.ResponseWriter, r *http.Request) {
	if !localOnly(w, r) {
		return
	}
	if code, msg := sameOrigin(r); code != 0 {
		writeJSON(w, code, map[string]string{"error": msg})
		return
	}
	var req struct {
		Harness string `json:"harness"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "body must be {\"harness\":…} (empty: every detected one)"})
		return
	}
	if req.Harness != "" && !slices.Contains(agents.CLIs, req.Harness) {
		writeJSON(w, 400, map[string]string{"error": "harness: one of " + strings.Join(agents.CLIs, ", ")})
		return
	}
	if !sv.installing.CompareAndSwap(false, true) {
		writeJSON(w, 429, map[string]string{"error": "an install is already running"})
		return
	}
	defer sv.installing.Store(false)
	run := sv.SkillsInstall
	if run == nil {
		run = runSkillsInstall
	}
	ctx, cancel := context.WithTimeout(r.Context(), skillsInstallTimeout)
	defer cancel()
	out, err := run(ctx, req.Harness)
	out = text.Clean(out)
	if len(out) > 8<<10 {
		out = out[len(out)-8<<10:]
	}
	if err != nil {
		last := strings.TrimSpace(out)
		if i := strings.LastIndexByte(last, '\n'); i >= 0 {
			last = last[i+1:]
		}
		if last == "" {
			last = err.Error()
		}
		writeJSON(w, 502, map[string]string{"error": "install failed: " + last, "output": out})
		return
	}
	writeJSON(w, 200, map[string]string{"output": out})
}

func runSkillsInstall(ctx context.Context, harness string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	args := []string{"skills", "install"}
	if harness != "" {
		args = append(args, "--harness", harness)
	}
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Env = agents.Scrub(os.Environ())
	b, err := cmd.CombinedOutput()
	return string(b), err
}
