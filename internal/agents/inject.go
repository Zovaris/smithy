package agents

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/Zovaris/smithy/internal/config"
)

// MCPServer is one stdio MCP server given to a driven CLI.
type MCPServer struct {
	Command string
	Args    []string
	Env     map[string]string
}

// Kit is what smithy gives one run of a driven CLI: MCP servers, skill folders (each holding a
// SKILL.md), text appended to the system prompt and a settings JSON object.
type Kit struct {
	MCP          map[string]MCPServer
	Skills       []string
	SystemPrompt string
	Settings     string
}

// Injection is a Kit turned into one CLI's flags and env, with files written only under the
// run's own directory. Unsupported names the kit pieces the CLI has no way to take
// (mcp, settings, skills, system-prompt); they are reported, never faked.
type Injection struct {
	Args        []string
	Env         []string
	Unsupported []string
	Temp        string // a temp kit dir RunKit made; Remove deletes it after the run
}

// Remove deletes the temp kit dir (claude-mcp.json, a CODEX_HOME linking auth.json), if any.
func (in *Injection) Remove() {
	if in != nil && in.Temp != "" {
		os.RemoveAll(in.Temp)
	}
}

var safeName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// Inject writes the kit's files for cli under dir and returns the flags and env that load them.
// The user's global config is never written: claude gets flags, codex a CODEX_HOME under dir
// (auth.json linked from the user's home so the login keeps working), opencode an
// OPENCODE_CONFIG_DIR, gemini a system settings file through GEMINI_CLI_SYSTEM_SETTINGS_PATH,
// and pi flags.
func Inject(cli string, k Kit, dir string, getenv func(string) string) (Injection, error) {
	var in Injection
	for name := range k.MCP {
		if !safeName.MatchString(name) {
			return in, fmt.Errorf("mcp server name %q: use letters, digits, - and _", name)
		}
	}
	var settings map[string]any
	if k.Settings != "" {
		if err := json.Unmarshal([]byte(k.Settings), &settings); err != nil {
			return in, fmt.Errorf("kit settings: %w", err)
		}
	}
	has := map[string]bool{"mcp": len(k.MCP) > 0, "settings": k.Settings != "", "skills": len(k.Skills) > 0, "system-prompt": k.SystemPrompt != ""}
	unsupported := func(pieces ...string) {
		for _, p := range pieces {
			if has[p] {
				in.Unsupported = append(in.Unsupported, p)
			}
		}
	}
	if !slices.ContainsFunc([]string{"mcp", "settings", "skills", "system-prompt"}, func(p string) bool { return has[p] }) {
		return in, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return in, err
	}
	switch Kind(cli) {
	case "claude":
		if has["mcp"] {
			p := filepath.Join(dir, "claude-mcp.json")
			if err := writeJSON(p, map[string]any{"mcpServers": mcpJSON(k.MCP, false)}); err != nil {
				return in, err
			}
			in.Args = append(in.Args, "--mcp-config", p, "--strict-mcp-config")
		}
		if has["skills"] {
			plug := filepath.Join(dir, "claude-plugin")
			if err := writeJSON(filepath.Join(plug, ".claude-plugin", "plugin.json"), map[string]any{"name": "smithy-kit", "description": "Skills smithy gives this run"}); err != nil {
				return in, err
			}
			if err := linkSkills(k.Skills, filepath.Join(plug, "skills")); err != nil {
				return in, err
			}
			in.Args = append(in.Args, "--plugin-dir", plug)
		}
		if has["system-prompt"] {
			in.Args = append(in.Args, "--append-system-prompt", k.SystemPrompt)
		}
		if has["settings"] {
			p := filepath.Join(dir, "claude-settings.json")
			if err := os.WriteFile(p, []byte(k.Settings), 0o600); err != nil {
				return in, err
			}
			in.Args = append(in.Args, "--settings", p)
		}
	case "codex":
		home := filepath.Join(dir, "codex-home")
		if err := os.MkdirAll(home, 0o700); err != nil {
			return in, err
		}
		src := getenv("CODEX_HOME")
		if src == "" {
			if h, err := os.UserHomeDir(); err == nil {
				src = filepath.Join(h, ".codex")
			}
		}
		if src != "" {
			if _, err := os.Stat(filepath.Join(src, "auth.json")); err == nil {
				os.Remove(filepath.Join(home, "auth.json"))
				if err := os.Symlink(filepath.Join(src, "auth.json"), filepath.Join(home, "auth.json")); err != nil {
					return in, err
				}
			}
		}
		var b strings.Builder
		b.WriteString("profile = \"smithy\"\n\n[profiles.smithy]\n")
		if has["system-prompt"] {
			fmt.Fprintf(&b, "developer_instructions = %s\n", tomlQuote(k.SystemPrompt))
		}
		for _, name := range sortedKeys(k.MCP) {
			s := k.MCP[name]
			fmt.Fprintf(&b, "\n[mcp_servers.%s]\ncommand = %s\nargs = [%s]\n", name, tomlQuote(s.Command), quoteList(s.Args))
			if len(s.Env) > 0 {
				fmt.Fprintf(&b, "\n[mcp_servers.%s.env]\n", name)
				for _, key := range sortedKeys(s.Env) {
					kq := key
					if !safeName.MatchString(key) {
						kq = tomlQuote(key)
					}
					fmt.Fprintf(&b, "%s = %s\n", kq, tomlQuote(s.Env[key]))
				}
			}
		}
		if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(b.String()), 0o600); err != nil {
			return in, err
		}
		if has["skills"] {
			if err := linkSkills(k.Skills, filepath.Join(home, "skills")); err != nil {
				return in, err
			}
		}
		in.Args = []string{"--profile", "smithy"}
		in.Env = []string{"CODEX_HOME=" + home}
		unsupported("settings")
	case "opencode":
		cfg := filepath.Join(dir, "opencode")
		conf := map[string]any{"$schema": "https://opencode.ai/config.json"}
		if has["mcp"] {
			conf["mcp"] = mcpJSON(k.MCP, true)
		}
		if has["system-prompt"] {
			p := filepath.Join(cfg, "smithy-instructions.md")
			if err := os.MkdirAll(cfg, 0o700); err != nil {
				return in, err
			}
			if err := os.WriteFile(p, []byte(k.SystemPrompt), 0o600); err != nil {
				return in, err
			}
			conf["instructions"] = []string{p}
		}
		if err := writeJSON(filepath.Join(cfg, "opencode.json"), conf); err != nil {
			return in, err
		}
		if has["skills"] {
			if err := linkSkills(k.Skills, filepath.Join(cfg, "skills")); err != nil {
				return in, err
			}
		}
		in.Env = []string{"OPENCODE_CONFIG_DIR=" + cfg}
		unsupported("settings")
	case "gemini":
		if has["mcp"] || has["settings"] {
			if settings == nil {
				settings = map[string]any{}
			}
			if has["mcp"] {
				settings["mcpServers"] = mcpJSON(k.MCP, false)
			}
			p := filepath.Join(dir, "gemini-settings.json")
			if err := writeJSON(p, settings); err != nil {
				return in, err
			}
			in.Env = []string{"GEMINI_CLI_SYSTEM_SETTINGS_PATH=" + p}
		}
		unsupported("skills", "system-prompt")
	case "pi":
		for _, s := range k.Skills {
			if _, err := os.Stat(filepath.Join(s, "SKILL.md")); err != nil {
				return in, fmt.Errorf("skill %s: no SKILL.md", s)
			}
			in.Args = append(in.Args, "--skill", s)
		}
		if has["system-prompt"] {
			in.Args = append(in.Args, "--append-system-prompt", k.SystemPrompt)
		}
		unsupported("mcp", "settings")
	default:
		unsupported("mcp", "settings", "skills", "system-prompt")
	}
	return in, nil
}

func mcpJSON(servers map[string]MCPServer, opencode bool) map[string]any {
	out := map[string]any{}
	for name, s := range servers {
		env := s.Env
		if env == nil {
			env = map[string]string{}
		}
		args := s.Args
		if args == nil {
			args = []string{}
		}
		if opencode {
			out[name] = map[string]any{"type": "local", "command": append([]string{s.Command}, args...), "environment": env, "enabled": true}
		} else {
			out[name] = map[string]any{"type": "stdio", "command": s.Command, "args": args, "env": env}
		}
	}
	return out
}

func writeJSON(p string, v any) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

// linkSkills links each skill folder into dst under its own name.
func linkSkills(skills []string, dst string) error {
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return err
	}
	for _, s := range skills {
		abs, err := filepath.Abs(s)
		if err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Join(abs, "SKILL.md")); err != nil {
			return fmt.Errorf("skill %s: no SKILL.md", s)
		}
		link := filepath.Join(dst, filepath.Base(abs))
		os.Remove(link)
		if err := os.Symlink(abs, link); err != nil {
			return err
		}
	}
	return nil
}

func quoteList(a []string) string {
	q := make([]string, len(a))
	for i, s := range a {
		q[i] = tomlQuote(s)
	}
	return strings.Join(q, ", ")
}

func sortedKeys[V any](m map[string]V) []string {
	k := make([]string, 0, len(m))
	for x := range m {
		k = append(k, x)
	}
	slices.Sort(k)
	return k
}

// tomlQuote writes s as a TOML basic string.
func tomlQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// takeArg removes flag and its value from a, returning the value ("" when absent).
func takeArg(a []string, flag string) ([]string, string) {
	i := slices.Index(a, flag)
	if i < 0 || i+1 >= len(a) {
		return a, ""
	}
	return slices.Delete(slices.Clone(a), i, i+2), a[i+1]
}

// PoolGetenv reads the pool account's variables (o.PoolEnv) before getenv, so a pooled run's
// kit links that account's login, not the default one.
func PoolGetenv(o Options, getenv func(string) string) func(string) string {
	return func(k string) string {
		for i := len(o.PoolEnv) - 1; i >= 0; i-- {
			if key, v, ok := strings.Cut(o.PoolEnv[i], "="); ok && key == k {
				return v
			}
		}
		return getenv(k)
	}
}

// mergeSettings lays the kit's settings JSON under smithy's own (smithy's keys win).
func mergeSettings(kit, own string) string {
	var k, o map[string]any
	if b, err := os.ReadFile(kit); err == nil {
		kit = string(b)
	}
	if json.Unmarshal([]byte(kit), &k) != nil || json.Unmarshal([]byte(own), &o) != nil {
		return own
	}
	for key, v := range o {
		k[key] = v
	}
	b, _ := json.Marshal(k)
	return string(b)
}

// KitMCP is the smithy MCP server command (smithy mcp) the harness build gives every driven CLI;
// nil in the skills build, which has no smithy mcp.
var KitMCP []string

// KitChoice is what the user chose to inject into every driven CLI (kit.mcp, kit.skills):
// whether smithy's MCP server goes in, and which installed skills are linked in by name.
func KitChoice() (mcp bool, skills []string) {
	mcp = config.Get("kit.mcp", "true") != "false"
	for _, s := range strings.Split(config.Get("kit.skills", ""), ",") {
		if s = strings.TrimSpace(s); s != "" && !slices.Contains(skills, s) {
			skills = append(skills, s)
		}
	}
	return mcp, skills
}

// KitGuard turns the smithy binary into the Claude Code settings that run the dangerous-command
// guard before every Bash call ("" when the guard is off); nil in the skills build.
var KitGuard func(exe string) string

// RunKit is the injection for one run (kit.mcp, kit.skills): KitMCP as the "smithy" MCP server, told the run's warden
// card (SMITHY_MCP_CARD) and, for a --readonly run, to expose read-only tools. nil when there is
// no kit or for a custom AGENT_CMD.
func RunKit(cli string, o Options, getenv func(string) string) (*Injection, error) {
	if len(KitMCP) == 0 || cli == "custom" {
		return nil, nil
	}
	useMCP, skillNames := KitChoice()
	var skills []string
	for _, n := range skillNames {
		if p, ok := FindSkill(n); ok {
			skills = append(skills, filepath.Dir(p))
		}
	}
	guard := ""
	if KitGuard != nil && Kind(cli) == "claude" {
		guard = KitGuard(KitMCP[0])
	}
	if !useMCP && len(skills) == 0 && guard == "" {
		return nil, nil
	}
	args := slices.Clone(KitMCP[1:])
	if o.ReadOnly {
		args = append(args, "--readonly")
	}
	env := map[string]string{}
	if batch, id := cardOf(o.Log, o.Cwd); batch != "" {
		env["SMITHY_MCP_CARD"] = batch + "/" + id
	}
	dir := ""
	if o.Log != "" {
		dir = o.Log
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(o.Cwd, dir)
		}
		dir += ".kit"
	} else {
		d, err := os.MkdirTemp("", "smithy-kit-")
		if err != nil {
			return nil, err
		}
		dir = d
	}
	k := Kit{Skills: skills}
	if useMCP {
		k.MCP = map[string]MCPServer{"smithy": {Command: KitMCP[0], Args: args, Env: env}}
	}
	k.Settings = guard
	in, err := Inject(cli, k, dir, PoolGetenv(o, getenv))
	if o.Log == "" {
		if err != nil {
			os.RemoveAll(dir)
			return nil, err
		}
		in.Temp = dir
	}
	if err != nil {
		return nil, err
	}
	return &in, nil
}
