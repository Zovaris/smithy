package skills

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func write(t *testing.T, p, s string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInventoryPerHarness(t *testing.T) {
	home := t.TempDir()
	write(t, filepath.Join(home, ".claude", "skills", "smelt", "SKILL.md"), "x")
	write(t, filepath.Join(home, ".claude", "skills", "mine", "SKILL.md"), "x")
	write(t, filepath.Join(home, ".claude.json"), `{"mcpServers":{"linear":{},"github":{}},"projects":{}}`)
	write(t, filepath.Join(home, ".codex", "config.toml"), "model = \"o3\"\n[mcp_servers.figma]\ncommand = \"x\"\n[mcp_servers.figma.env]\nA = \"b\"\n[mcp_servers.\"my docs\"]\n")
	write(t, filepath.Join(home, ".config", "opencode", "opencode.json"), `{"mcp":{"ctx":{}}}`)
	write(t, filepath.Join(home, ".gemini", "config", "mcp_config.json"), `not json`)
	env := map[string]string{}
	inv := Inventory(home, func(k string) string { return env[k] })
	by := map[string]HarnessInventory{}
	for _, h := range inv {
		by[h.Harness] = h
	}
	c := by["claude"]
	if !c.Present || c.SkillsDir != filepath.Join(home, ".claude", "skills") {
		t.Fatalf("claude: %+v", c)
	}
	if !slices.Equal(c.Smithy, []string{"smelt"}) || !slices.Contains(c.Missing, "warden") || slices.Contains(c.Missing, "smelt") {
		t.Fatalf("claude smithy skills: have %v missing %v", c.Smithy, c.Missing)
	}
	if !slices.Equal(c.Other, []string{"mine"}) {
		t.Fatalf("claude other skills: %v", c.Other)
	}
	if !slices.Equal(c.MCP, []string{"github", "linear"}) {
		t.Fatalf("claude mcp: %v", c.MCP)
	}
	if x := by["codex"]; !x.Present || !slices.Equal(x.MCP, []string{"figma", "my docs"}) {
		t.Fatalf("codex mcp from config.toml: %+v", x)
	}
	if o := by["opencode"]; !slices.Equal(o.MCP, []string{"ctx"}) {
		t.Fatalf("opencode: %+v", o)
	}
	if g := by["agy"]; !g.Present || g.MCPError == "" {
		t.Fatalf("agy with a broken MCP file says so: %+v", g)
	}
	if p := by["pi"]; p.Present {
		t.Fatalf("pi is not installed: %+v", p)
	}
}
