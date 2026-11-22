package agents

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/ghx"
	"github.com/Zovaris/smithy/internal/harness"
)

const setupUsage = `use-smithy setup: what is installed, model settings, and the agent instructions block.
  smithy setup status                 installed skills, models for this harness, block presence
  smithy setup block status [file]    is the block in <file> (default: this harness's user file)?
  smithy setup block add [file]       add or refresh the block (between use-smithy:start/end markers)
  smithy setup block remove [file]    remove it
  smithy setup path [status|add]      is smithy on PATH; add ~/.smithy/bin to the shell's startup file
Default file per harness: claude-* → ~/.claude/CLAUDE.md, codex → $CODEX_HOME/AGENTS.md,
opencode → $XDG_CONFIG_HOME/opencode/AGENTS.md, gemini → ~/.gemini/GEMINI.md,
pi → $PI_CODING_AGENT_DIR/AGENTS.md, dsh → $DSH_HOME/AGENTS.md, else ./AGENTS.md (Cursor has
no global file; it reads the project's AGENTS.md).`

const (
	blockStart = "<!-- use-smithy:start -->"
	blockEnd   = "<!-- use-smithy:end -->"
)

// DefaultFile is the agent instructions file for harness h.
func DefaultFile(h, home, cwd string, getenv func(string) string) string {
	switch {
	case strings.HasPrefix(h, "claude-"):
		return filepath.Join(home, ".claude", "CLAUDE.md")
	case h == "codex":
		return filepath.Join(envOr(getenv, "CODEX_HOME", filepath.Join(home, ".codex")), "AGENTS.md")
	case h == "opencode":
		return filepath.Join(envOr(getenv, "XDG_CONFIG_HOME", filepath.Join(home, ".config")), "opencode", "AGENTS.md")
	case h == "gemini":
		return filepath.Join(home, ".gemini", "GEMINI.md")
	case h == "pi":
		return filepath.Join(envOr(getenv, "PI_CODING_AGENT_DIR", filepath.Join(home, ".pi", "agent")), "AGENTS.md")
	case h == "dsh":
		return filepath.Join(envOr(getenv, "DSH_HOME", filepath.Join(home, ".dsh")), "AGENTS.md")
	}
	return filepath.Join(cwd, "AGENTS.md")
}

// HasBlock reports whether text holds the use-smithy block.
func HasBlock(text string) bool { return strings.Contains(text, blockStart) }

// ExtractBlock returns the first use-smithy block in text, markers included, ending in a newline;
// empty when text has none.
func ExtractBlock(text string) string {
	i := strings.Index(text, blockStart)
	if i < 0 {
		return ""
	}
	j := strings.Index(text[i:], blockEnd)
	if j < 0 {
		return ""
	}
	return text[i:i+j+len(blockEnd)] + "\n"
}

// StripBlock removes every use-smithy block, markers included, and the blank lines it leaves at
// the end; each kept line ends in a newline.
func StripBlock(text string) string {
	var b strings.Builder
	skip := false
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if strings.Contains(line, blockStart) {
			skip = true
		}
		if !skip {
			b.WriteString(line + "\n")
		}
		if strings.Contains(line, blockEnd) {
			skip = false
		}
	}
	out := strings.TrimRight(b.String(), "\n")
	if out == "" {
		return ""
	}
	return out + "\n"
}

// AddBlock returns text with the block replaced by (or appended as) block.
func AddBlock(text, block string) string {
	rest := StripBlock(text)
	if rest != "" {
		rest += "\n"
	}
	return rest + block
}

func blockFile() (string, error) {
	if d := ghx.SkillDir(); d != "" {
		if f := filepath.Join(d, "..", "references", "SETUP.md"); isFile(f) {
			return f, nil
		}
	}
	if p, ok := FindSkill("use-smithy"); ok {
		if f := filepath.Join(filepath.Dir(p), "references", "SETUP.md"); isFile(f) {
			return f, nil
		}
	}
	return "", errors.New("smithy setup: use-smithy/references/SETUP.md not found")
}

func defaultFile() string {
	h, _ := harness.Detect()
	home := os.Getenv("HOME")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	cwd, _ := os.Getwd()
	return DefaultFile(h, home, cwd, os.Getenv)
}

func fileHasBlock(f string) bool {
	b, err := os.ReadFile(f)
	return err == nil && HasBlock(string(b))
}

func block(args []string) int {
	f := ""
	if len(args) > 1 {
		f = args[1]
	}
	if f == "" {
		f = defaultFile()
	}
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "status":
		if !fileHasBlock(f) {
			fmt.Println("missing in", f)
			return 1
		}
		fmt.Println("present in", f)
	case "add":
		bf, err := blockFile()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		blk, err := os.ReadFile(bf)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		blk = []byte(ExtractBlock(string(blk)))
		if len(blk) == 0 {
			fmt.Fprintln(os.Stderr, "smithy setup: no use-smithy block in", bf)
			return 1
		}
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		cur, _ := os.ReadFile(f)
		if err := os.WriteFile(f, []byte(AddBlock(string(cur), string(blk))), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("added to", f)
	case "remove":
		cur, err := os.ReadFile(f)
		if err != nil || !HasBlock(string(cur)) {
			fmt.Println("not in", f)
			return 0
		}
		if err := os.WriteFile(f, []byte(StripBlock(string(cur))), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("removed from", f)
	default:
		fmt.Fprintln(os.Stderr, "usage: smithy setup block status|add|remove [file]")
		return 2
	}
	return 0
}

func status() int {
	h, _ := harness.Detect()
	fmt.Println("harness:", h)
	fmt.Println("installed:")
	for _, s := range Known {
		if p, ok := FindSkill(s); ok {
			fmt.Printf("  %-9s %s\n", s, p)
		}
	}
	fmt.Println("settings (repo, then user, then default):")
	if exe, err := os.Executable(); err == nil {
		out, _ := exec.Command(exe, "config", "list").Output()
		for _, l := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
			if l != "" {
				fmt.Println("  " + l)
			}
		}
	}
	for _, t := range []string{"cheap", "mid", "strong"} {
		if _, err := config.Model(t); err != nil {
			fmt.Printf("models for %s: ask the user to name each unset tier, then smithy config set-model <tier> <name>\n", h)
			break
		}
	}
	pathCmd(nil)
	f := defaultFile()
	if fileHasBlock(f) {
		fmt.Println("block: present in", f)
	} else {
		fmt.Printf("block: missing in %s (smithy setup block add)\n", f)
	}
	return 0
}

// SetupMain runs `smithy setup` and returns the exit code.
func SetupMain(args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "status":
			return status()
		case "block":
			return block(args[1:])
		case "path":
			return pathCmd(args[1:])
		}
	}
	fmt.Println(setupUsage)
	return 2
}
