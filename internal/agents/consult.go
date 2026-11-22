package agents

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/harness"
)

const consultUsage = `One cheap, tool-less call to the user's strongest model to settle a decision without the user.
  smithy consult "<question>" "<options, verbatim and in neutral order>" [--context <file>]
Prints the answer (≤5 lines). It may start with NEED-INFO: <what> (send the question back with
that) or ESCALATE: <why> (the user decides). Callers log what they decided and why.
Model: $CONSULT_MODEL, else the judge tier (the smartest model you have, optional), else strong,
else the CLI default.
CLI: $CONSULT_CMD (prompt on stdin) if set; else the CLI of the harness you're in (smithy harness),
else the first installed one. Claude runs with no tools, MCP, settings or session (~1k tokens);
the others run read-only through smithy agent. Nothing here assumes a vendor.`

// ConsultPrompt is the prompt consult sends for a question, its options and optional context.
func ConsultPrompt(q, o, ctx string) string {
	var b strings.Builder
	b.WriteString("You are a senior engineer settling one decision so the work can continue. Answer briefly.\n")
	b.WriteString("Question: " + q + "\n")
	b.WriteString("Options (neutral order): " + o + "\n")
	if ctx != "" {
		b.WriteString("Context:\n" + ctx + "\n")
	}
	b.WriteString("Pick one option and give the reason in at most 5 lines. If none fits or you need more\n")
	b.WriteString("information, answer NEED-INFO: <what>. If a product owner should decide (customer-visible\n")
	b.WriteString("behaviour, money, data, security, public API, production, irreversible), answer ESCALATE: <why>.\n")
	b.WriteString("Do not use tools.\n")
	return b.String()
}

// ConsultCLI picks the CLI for a harness: its own CLI when installed, else the first installed one.
func ConsultCLI(h string, have func(string) bool) string {
	cli := ""
	switch {
	case strings.HasPrefix(h, "claude-"):
		cli = "claude"
	case h == "codex", h == "opencode", h == "gemini", h == "pi", h == "dsh", h == "cursor":
		cli = h
	}
	if cli != "" && !have(Bin(cli)) {
		cli = ""
	}
	if cli == "" {
		for _, c := range []string{"claude", "codex", "opencode", "gemini", "pi", "dsh"} {
			if have(c) {
				return c
			}
		}
	}
	return cli
}

func shell(command, stdinPath, dir string) int {
	in, err := os.Open(stdinPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer in.Close()
	cmd := exec.Command("bash", "-c", command)
	cmd.Dir, cmd.Stdin, cmd.Stdout, cmd.Stderr = dir, in, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return exitCode(err)
	}
	return 0
}

// ConsultMain runs `smithy consult` and returns the exit code.
func ConsultMain(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, consultUsage)
		return 2
	}
	ctx := ""
	if len(args) > 3 && args[2] == "--context" && isFile(args[3]) {
		if f, err := os.Open(args[3]); err == nil {
			b, _ := io.ReadAll(io.LimitReader(f, 6000))
			f.Close()
			ctx = strings.TrimRight(string(b), "\n")
		}
	}
	model := os.Getenv("CONSULT_MODEL")
	if model == "" {
		model, _ = config.Model("judge")
	}
	if model == "" {
		model, _ = config.Model("strong")
	}
	tmp, err := os.MkdirTemp("", "consult.")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(tmp)
	pf := filepath.Join(tmp, "prompt.md")
	if err := os.WriteFile(pf, []byte(ConsultPrompt(args[0], args[1], ctx)), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if c := os.Getenv("CONSULT_CMD"); c != "" {
		return shell(c, pf, "")
	}
	h, _ := harness.Detect()
	cli := ConsultCLI(h, installed)
	if cli == "claude" {
		a := []string{"-p"}
		if model != "" {
			a = append(a, "--model", model)
		}
		a = append(a, "--effort", DefaultEffort(), "--tools", "", "--strict-mcp-config", "--disable-slash-commands", "--setting-sources", "", "--no-session-persistence")
		in, err := os.Open(pf)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer in.Close()
		cmd := exec.Command("claude", a...)
		cmd.Dir, cmd.Stdin, cmd.Stdout, cmd.Stderr = tmp, in, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return exitCode(err)
		}
		return 0
	}
	os.Setenv("AGENT_CLI", cli)
	if model == "" {
		model = os.Getenv("AGENT_MODEL")
	}
	return Run(Options{Cwd: tmp, Model: model, Effort: DefaultEffort(), ReadOnly: true, PromptFile: pf})
}
