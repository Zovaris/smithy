package agents

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Zovaris/smithy/internal/apiloop"
	"github.com/Zovaris/smithy/internal/auth"
)

// Ready is one agent smithy can drive: an installed CLI or an API provider with a stored key.
type Ready struct {
	Name   string `json:"name"` // the session agent: a CLI name or api:<provider>
	Kind   string `json:"kind"` // cli or api
	Ready  bool   `json:"ready"`
	Detail string `json:"detail"` // where it was found, or why it is missing
	Fix    string `json:"fix,omitempty"`
}

// ReadyDeps is what DetectReady looks at; DefaultReadyDeps reads this machine.
type ReadyDeps struct {
	LookPath func(string) (string, error)
	Getenv   func(string) string
	Stored   func(string) string
}

// DefaultReadyDeps reads PATH, the environment and smithy auth.
func DefaultReadyDeps() ReadyDeps {
	return ReadyDeps{LookPath: exec.LookPath, Getenv: os.Getenv, Stored: auth.Stored}
}

// InstallHint is the one command that installs each CLI.
var InstallHint = map[string]string{
	"claude":   "npm install -g @anthropic-ai/claude-code",
	"codex":    "npm install -g @openai/codex",
	"opencode": "npm install -g opencode-ai",
	"gemini":   "npm install -g @google/gemini-cli",
	"cursor":   "curl https://cursor.com/install -fsS | bash",
	"pi":       "npm install -g @mariozechner/pi-coding-agent",
	"dsh":      "see github.com/deepseek-ai/dsh-superagent-prototype",
}

var apiKeyEnv = map[string]string{
	"anthropic":  "ANTHROPIC_API_KEY",
	"openai":     "OPENAI_API_KEY",
	"gemini":     "GEMINI_API_KEY",
	"openrouter": "OPENROUTER_API_KEY",
	"deepseek":   "DEEPSEEK_API_KEY",
}

// cliFolders are the install folders a daemon started outside a login shell may not have on PATH.
func cliFolders(cli, home string) []string {
	d := []string{
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, ".npm-global", "bin"),
		filepath.Join(home, ".bun", "bin"),
		"/opt/homebrew/bin",
		"/usr/local/bin",
	}
	if cli == "claude" {
		d = append([]string{filepath.Join(home, ".claude", "local")}, d...)
	}
	return d
}

// FindCLI is the path of cli's binary: PATH first, then the usual install folders.
func FindCLI(cli string, look func(string) (string, error), getenv func(string) string) (string, bool) {
	bin := Bin(cli)
	if p, err := look(bin); err == nil {
		return p, true
	}
	home := getenv("HOME")
	if home == "" {
		return "", false
	}
	for _, d := range cliFolders(cli, home) {
		p := filepath.Join(d, bin)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return p, true
		}
	}
	return "", false
}

// DetectReady lists every CLI in CLIs order (Claude Code first), then every API provider, each
// ready or missing with the one action that fixes it.
func DetectReady(d ReadyDeps) []Ready {
	var out []Ready
	for _, c := range CLIs {
		if p, ok := FindCLI(c, d.LookPath, d.Getenv); ok {
			out = append(out, Ready{Name: c, Kind: "cli", Ready: true, Detail: p})
			continue
		}
		out = append(out, Ready{Name: c, Kind: "cli", Detail: Bin(c) + " not found", Fix: InstallHint[c]})
	}
	ps := make([]string, 0, len(apiloop.Providers))
	for p := range apiloop.Providers {
		ps = append(ps, p)
	}
	slices.Sort(ps)
	for _, p := range ps {
		r := Ready{Name: "api:" + p, Kind: "api"}
		svc := apiloop.Providers[p].Service
		switch {
		case svc == "":
			if path, err := d.LookPath("ollama"); err == nil {
				r.Ready, r.Detail = true, path
			} else {
				r.Detail, r.Fix = "ollama not found", "see ollama.com/download"
			}
		case strings.TrimSpace(d.Stored(svc)) != "":
			r.Ready, r.Detail = true, "smithy auth"
		default:
			r.Detail, r.Fix = "no key", "smithy auth set "+svc
			if env := apiKeyEnv[p]; env != "" && d.Getenv(env) != "" {
				r.Detail = env + " is set; smithy's API loop reads only smithy auth"
			}
		}
		out = append(out, r)
	}
	return out
}

// CLIPath is path with the folder of every CLI found off it appended, so a daemon started
// without the login shell's PATH still runs a CLI installed in a usual folder.
func CLIPath(path string, getenv func(string) string) string {
	look := func(bin string) (string, error) { return lookIn(path, bin) }
	parts := filepath.SplitList(path)
	for _, c := range CLIs {
		if _, err := look(Bin(c)); err == nil {
			continue
		}
		if p, ok := FindCLI(c, look, getenv); ok && !slices.Contains(parts, filepath.Dir(p)) {
			parts = append(parts, filepath.Dir(p))
		}
	}
	return strings.Join(parts, string(os.PathListSeparator))
}

func lookIn(path, bin string) (string, error) {
	for _, d := range filepath.SplitList(path) {
		p := filepath.Join(d, bin)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", exec.ErrNotFound
}
