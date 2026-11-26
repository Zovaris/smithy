package agents

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// HarnessInventory is what one harness has installed: smithy's skills, other skills and the
// MCP servers its user config lists.
type HarnessInventory struct {
	Harness   string   `json:"harness"`
	Present   bool     `json:"present"`
	SkillsDir string   `json:"skills_dir"`
	Smithy    []string `json:"smithy"`
	Missing   []string `json:"missing"`
	Other     []string `json:"other"`
	MCP       []string `json:"mcp"`
	MCPFile   string   `json:"mcp_file,omitempty"`
	MCPError  string   `json:"mcp_error,omitempty"`
}

// Inventory reads every harness smithy installs skills into, in installHarnesses order.
func Inventory(home string, getenv func(string) string) []HarnessInventory {
	var out []HarnessInventory
	for _, h := range installHarnesses {
		inv := HarnessInventory{Harness: h, Smithy: []string{}, Missing: []string{}, Other: []string{}, MCP: []string{}}
		if _, err := os.Stat(harnessMarker(h, home, getenv)); err == nil {
			inv.Present = true
		}
		dirs := HarnessSkillDirs(harnessName(h), "", home, getenv)
		if len(dirs) > 0 {
			inv.SkillsDir = dirs[0]
		}
		seen := map[string]bool{}
		for _, d := range dirs {
			es, _ := os.ReadDir(d)
			for _, e := range es {
				if !isFile(filepath.Join(d, e.Name(), "SKILL.md")) || seen[e.Name()] {
					continue
				}
				seen[e.Name()] = true
				if slices.Contains(Known, e.Name()) {
					inv.Smithy = append(inv.Smithy, e.Name())
				} else {
					inv.Other = append(inv.Other, e.Name())
				}
			}
		}
		for _, k := range Known {
			if !seen[k] {
				inv.Missing = append(inv.Missing, k)
			}
		}
		slices.Sort(inv.Other)
		inv.MCPFile = mcpFile(h, home, getenv)
		if inv.MCPFile != "" {
			names, err := mcpNames(h, inv.MCPFile)
			if err != nil {
				inv.MCPError = err.Error()
			}
			inv.MCP = append(inv.MCP, names...)
		}
		out = append(out, inv)
	}
	return out
}

func mcpFile(h, home string, getenv func(string) string) string {
	switch h {
	case "claude":
		return filepath.Join(home, ".claude.json")
	case "codex":
		return filepath.Join(envOr(getenv, "CODEX_HOME", filepath.Join(home, ".codex")), "config.toml")
	case "opencode":
		return filepath.Join(envOr(getenv, "XDG_CONFIG_HOME", filepath.Join(home, ".config")), "opencode", "opencode.json")
	case "agy":
		return filepath.Join(home, ".gemini", "config", "mcp_config.json")
	case "cursor":
		return filepath.Join(home, ".cursor", "mcp.json")
	}
	return ""
}

const maxMCPConfig = 8 << 20

// mcpNames lists the MCP server names in a harness's user config, sorted; a missing file is none.
func mcpNames(h, file string) ([]string, error) {
	f, err := os.Open(file)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var names []string
	if h == "codex" {
		seen := map[string]bool{}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			l := strings.TrimSpace(sc.Text())
			rest, ok := strings.CutPrefix(l, "[mcp_servers.")
			if !ok || !strings.HasSuffix(rest, "]") {
				continue
			}
			rest = strings.TrimSuffix(rest, "]")
			name := rest
			if strings.HasPrefix(rest, `"`) {
				if q, err := strconv.QuotedPrefix(rest); err == nil {
					name, _ = strconv.Unquote(q)
				}
			} else if i := strings.IndexByte(rest, '.'); i >= 0 {
				name = rest[:i]
			}
			if name != "" && !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
		slices.Sort(names)
		return names, sc.Err()
	}
	var doc map[string]json.RawMessage
	if err := json.NewDecoder(io.LimitReader(f, maxMCPConfig)).Decode(&doc); err != nil {
		return nil, errors.New(filepath.Base(file) + ": not valid JSON")
	}
	key := "mcpServers"
	if h == "opencode" {
		key = "mcp"
	}
	var servers map[string]json.RawMessage
	if raw, ok := doc[key]; ok {
		if err := json.Unmarshal(raw, &servers); err != nil {
			return nil, errors.New(filepath.Base(file) + ": " + key + " is not an object")
		}
	}
	for n := range servers {
		names = append(names, n)
	}
	slices.Sort(names)
	return names, nil
}
