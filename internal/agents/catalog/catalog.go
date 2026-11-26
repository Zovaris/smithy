// Package catalog is the one table of the agent CLIs smithy drives. It imports nothing from smithy,
// so every package (pool included) can read it; agents re-exports it as agents.KnownCLIs.
package catalog

// Agent is one agent CLI and what smithy knows about it.
type Agent struct {
	Name    string   // the CLI kind, as AGENT_CLI and settings name it
	Display string   // how the TUI and the web name it
	Bin     string   // the executable on PATH
	Install string   // the one command that installs it
	Effort  bool     // takes an effort flag; the rest only get $AGENT_EFFORT
	Skills  bool     // `smithy skills install` writes into its skills dir
	Consult bool     // a fallback CLI for consult when the harness's own is missing
	Tier    string   // the harness whose models.<harness>.<tier> settings it uses
	ACP     []string // its Agent Client Protocol command, when it speaks ACP
	Models  []string // the command that lists its models, when it has one
	Login   []string // the interactive command that signs a pool account in
	PoolEnv string   // the env var that points a pool account at its config dir; "" when no pool
}

// Known lists every agent CLI in pick order: the first installed one runs when AGENT_CLI and
// AGENT_CMD are unset.
var Known = []Agent{
	{Name: "claude", Display: "Claude Code", Bin: "claude", Install: "npm install -g @anthropic-ai/claude-code", Effort: true, Skills: true, Consult: true,
		Tier: "claude-cli", Login: []string{"claude", "/login"}, PoolEnv: "CLAUDE_CONFIG_DIR"},
	{Name: "codex", Display: "Codex", Bin: "codex", Install: "npm install -g @openai/codex", Effort: true, Skills: true, Consult: true,
		Tier: "codex", Login: []string{"codex", "login"}, PoolEnv: "CODEX_HOME"},
	{Name: "opencode", Display: "OpenCode", Bin: "opencode", Install: "npm install -g opencode-ai", Skills: true, Consult: true,
		Tier: "opencode", ACP: []string{"opencode", "acp"}, Models: []string{"opencode", "models"},
		Login: []string{"opencode", "auth", "login"}, PoolEnv: "XDG_DATA_HOME"},
	{Name: "agy", Display: "Antigravity", Bin: "agy", Install: "curl -fsSL https://antigravity.google/cli/install.sh | bash", Skills: true, Consult: true,
		Tier: "agy", ACP: []string{"agy", "acp"}},
	{Name: "copilot", Display: "Copilot", Bin: "copilot", Install: "npm install -g @github/copilot", Skills: true, Consult: true,
		Tier: "copilot", ACP: []string{"copilot", "--acp"}, Login: []string{"copilot"}, PoolEnv: "COPILOT_HOME"},
	{Name: "grok", Display: "Grok", Bin: "grok", Install: "curl -fsSL https://x.ai/cli/install.sh | bash", Skills: true, Consult: true,
		Tier: "grok", ACP: []string{"grok", "agent", "stdio"}, Login: []string{"grok"}, PoolEnv: "GROK_HOME"},
	{Name: "cursor", Display: "Cursor", Bin: "agent", Install: "curl https://cursor.com/install -fsS | bash", Skills: true,
		Tier: "cursor", Login: []string{"agent", "login"}, PoolEnv: "CURSOR_CONFIG_DIR"},
	{Name: "pi", Display: "Pi", Bin: "pi", Install: "npm install -g @mariozechner/pi-coding-agent", Skills: true, Consult: true,
		Tier: "pi", Login: []string{"pi"}, PoolEnv: "PI_CODING_AGENT_DIR"},
	{Name: "dsh", Display: "dsh", Bin: "dsh", Install: "see github.com/deepseek-ai/dsh-superagent-prototype", Skills: true, Consult: true,
		Tier: "dsh", Login: []string{"dsh"}, PoolEnv: "DSH_HOME"},
}

// Lookup is the Known entry named name.
func Lookup(name string) (Agent, bool) {
	for _, a := range Known {
		if a.Name == name {
			return a, true
		}
	}
	return Agent{}, false
}

// Names lists, in Known order, the names of the agents keep accepts (all when keep is nil).
func Names(keep func(Agent) bool) []string {
	var out []string
	for _, a := range Known {
		if keep == nil || keep(a) {
			out = append(out, a.Name)
		}
	}
	return out
}
