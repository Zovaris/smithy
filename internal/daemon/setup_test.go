//go:build harness

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/agents"
	"github.com/Zovaris/smithy/internal/config"
)

func setupTest(t *testing.T) (*Server, string, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := filepath.Join(home, "config.json")
	t.Setenv("SMITHY_CONFIG", cfg)
	sv, url, _ := sessionTest(t, func(ctx context.Context, msg string, h []Exchange, e Emit) (string, error) { return "ok", nil })
	sv.Ready = func() []agents.Ready {
		return []agents.Ready{{Name: "claude", Kind: "cli", Ready: true, Detail: "/bin/claude"}, {Name: "codex", Kind: "cli", Fix: "npm install -g @openai/codex"}}
	}
	return sv, url, home
}

func getSetup(t *testing.T, url string) Setup {
	t.Helper()
	res := do(t, "GET", url+"/v1/setup", "", auth)
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("GET /v1/setup: %d", res.StatusCode)
	}
	var s Setup
	if err := json.NewDecoder(res.Body).Decode(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSetupReportsAgentsSkillsAndKit(t *testing.T) {
	_, url, home := setupTest(t)
	os.MkdirAll(filepath.Join(home, ".claude", "skills", "smelt"), 0o755)
	os.WriteFile(filepath.Join(home, ".claude", "skills", "smelt", "SKILL.md"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{"mcpServers":{"linear":{}}}`), 0o644)
	s := getSetup(t, url)
	if len(s.Agents) != 2 || s.Agents[0].Name != "claude" || !s.Agents[0].Ready || s.Agents[1].Fix == "" {
		t.Fatalf("agents: %+v", s.Agents)
	}
	if s.Default.Agent != "claude" || s.Chosen {
		t.Fatalf("with nothing chosen yet the default is the first ready agent: %+v", s.Default)
	}
	if !s.Kit.MCP || len(s.Kit.Skills) != 0 {
		t.Fatalf("default kit: %+v", s.Kit)
	}
	var claude *agents.HarnessInventory
	for i := range s.Harnesses {
		if s.Harnesses[i].Harness == "claude" {
			claude = &s.Harnesses[i]
		}
	}
	if claude == nil || !claude.Present || len(claude.Smithy) != 1 || len(claude.MCP) != 1 {
		t.Fatalf("claude inventory: %+v", claude)
	}
	if !slices.Contains(s.Skills, "smelt") {
		t.Fatalf("installed skills to toggle: %v", s.Skills)
	}
}

func TestSetupDefaultIsSavedAndUsed(t *testing.T) {
	sv, url, home := setupTest(t)
	if code, m := put(t, url+"/v1/setup/default", `{"agent":"codex","model":"gpt-5"}`); code != 200 || m["agent"] != "codex" {
		t.Fatalf("set default: %d %v", code, m)
	}
	if v, _ := config.UserGet("session.agent", ""); v != "codex" {
		t.Fatalf("session.agent saved: %q", v)
	}
	if s := getSetup(t, url); s.Default.Agent != "codex" || s.Default.Model != "gpt-5" || !s.Chosen {
		t.Fatalf("default: %+v", s.Default)
	}
	code, m := post(t, url+"/v1/sessions", `{"dir":"`+home+`"}`)
	if code != 201 || m["agent"] != "codex" || m["model"] != "gpt-5" {
		t.Fatalf("a session with no agent starts with the picked one: %d %v", code, m)
	}
	code, m = post(t, url+"/v1/sessions", `{"agent":"claude","model":"opus","dir":"`+home+`"}`)
	if code != 201 || m["model"] != "opus" {
		t.Fatalf("explicit model: %d %v", code, m)
	}
	if a, md := sv.Defaults(); a != "claude" || md != "opus" {
		t.Fatalf("last used becomes the default: %s %s", a, md)
	}
	for _, bad := range []string{`{"agent":"rm -rf"}`, `{"agent":"claude","model":"--dangerously"}`, `{`} {
		if code, _ := put(t, url+"/v1/setup/default", bad); code != 400 {
			t.Errorf("%s: want 400, got %d", bad, code)
		}
	}
	if code, m := post(t, url+"/v1/sessions", `{"agent":"claude","model":"-x","dir":"`+home+`"}`); code != 400 || !strings.Contains(m["error"].(string), "model") {
		t.Errorf("a flag-like model is refused: %d %v", code, m)
	}
}

func TestSetupKitToggles(t *testing.T) {
	_, url, home := setupTest(t)
	os.MkdirAll(filepath.Join(home, ".claude", "skills", "hush"), 0o755)
	os.WriteFile(filepath.Join(home, ".claude", "skills", "hush", "SKILL.md"), []byte("x"), 0o644)
	if code, m := put(t, url+"/v1/setup/kit", `{"mcp":false,"skills":["hush"]}`); code != 200 {
		t.Fatalf("set kit: %d %v", code, m)
	}
	mcp, skills := agents.KitChoice()
	if mcp || len(skills) != 1 || skills[0] != "hush" {
		t.Fatalf("kit saved: %v %v", mcp, skills)
	}
	if s := getSetup(t, url); s.Kit.MCP || len(s.Kit.Skills) != 1 {
		t.Fatalf("kit read back: %+v", s.Kit)
	}
	for _, bad := range []string{`{"mcp":true,"skills":["nope"]}`, `{"mcp":true,"skills":["../x"]}`} {
		if code, _ := put(t, url+"/v1/setup/kit", bad); code != 400 {
			t.Errorf("%s: want 400, got %d", bad, code)
		}
	}
}

func TestSetupNeedsSameOrigin(t *testing.T) {
	_, url, _ := setupTest(t)
	h := map[string]string{"Authorization": auth["Authorization"], "Origin": "https://evil.example"}
	res := do(t, "PUT", url+"/v1/setup/kit", `{"mcp":false}`, h)
	if res.StatusCode != 403 {
		t.Fatalf("cross-origin write: %d", res.StatusCode)
	}
}

func put(t *testing.T, url, body string) (int, map[string]any) {
	t.Helper()
	res := do(t, "PUT", url, body, auth)
	defer res.Body.Close()
	var m map[string]any
	json.NewDecoder(res.Body).Decode(&m)
	return res.StatusCode, m
}

func TestSetupSkillsInstall(t *testing.T) {
	sv, url, _ := setupTest(t)
	var got []string
	sv.SkillsInstall = func(ctx context.Context, h string) (string, error) {
		got = append(got, h)
		if h == "pi" {
			return "no network", errors.New("exit 1")
		}
		return "installed 8 skills into claude", nil
	}
	if code, m := post(t, url+"/v1/setup/skills/install", `{}`); code != 200 || !strings.Contains(m["output"].(string), "8 skills") {
		t.Fatalf("install all: %d %v", code, m)
	}
	if code, m := post(t, url+"/v1/setup/skills/install", `{"harness":"pi"}`); code != 502 || m["output"] != "no network" {
		t.Fatalf("a failed install shows its output: %d %v", code, m)
	}
	if code, _ := post(t, url+"/v1/setup/skills/install", `{"harness":"; rm"}`); code != 400 {
		t.Fatalf("unknown harness: %d", code)
	}
	if strings.Join(got, ",") != ",pi" {
		t.Fatalf("runs: %q", got)
	}
}

func TestSetupListsUserSkillFolders(t *testing.T) {
	_, url, home := setupTest(t)
	sm := t.TempDir()
	t.Setenv("SMITHY_HOME", sm)
	for _, d := range []string{filepath.Join(home, ".agents", "skills", "pdf"), filepath.Join(sm, "skills", "mine"), filepath.Join(home, ".agents", "skills", "nofile")} {
		os.MkdirAll(d, 0o755)
	}
	os.WriteFile(filepath.Join(home, ".agents", "skills", "pdf", "SKILL.md"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(sm, "skills", "mine", "SKILL.md"), []byte("x"), 0o644)
	s := getSetup(t, url)
	if !slices.Contains(s.Skills, "pdf") || !slices.Contains(s.Skills, "mine") || slices.Contains(s.Skills, "nofile") {
		t.Fatalf("skills from ~/.agents/skills and ~/.smithy/skills: %v", s.Skills)
	}
	if !strings.HasSuffix(s.Paths["mine"], filepath.Join("mine", "SKILL.md")) || !strings.HasSuffix(s.Paths["pdf"], filepath.Join("pdf", "SKILL.md")) {
		t.Fatalf("paths: %v", s.Paths)
	}
	if code, m := put(t, url+"/v1/setup/kit", `{"mcp":true,"skills":["mine"]}`); code != 200 {
		t.Fatalf("a user skill can be toggled on: %d %v", code, m)
	}
}
