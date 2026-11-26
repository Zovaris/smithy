package agents

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Zovaris/smithy/internal/ghx"
)

const skillsUsage = `Finds installed skills, so a skill or a worker can read another skill's SKILL.md directly.
User-invoked skills can't be called by an agent as a slash command, but any agent in any
harness can read the file.
  smithy skills <name>     absolute path of <name>/SKILL.md; exit 1 when not installed
  smithy skills --list     ` + "`<name> <path>`" + ` for every skill of this repo that is installed
  smithy skills install [--harness <name>...]
                           download this release's skills (checksum-checked) into each detected
                           harness's skills folder, or only the named ones; no Node needed.
                           Harnesses: claude, codex, opencode, agy, copilot, grok, cursor, pi, dsh`

// Known are the skills of this repo, in the order --list prints them.
var Known = []string{"use-smithy", "smelt", "warden", "ledger", "scout", "tend", "hush", "simple"}

func envOr(getenv func(string) string, key, def string) string {
	if v := getenv(key); v != "" {
		return v
	}
	return def
}

// SkillCandidates lists where a skill's SKILL.md may be, in lookup order: next to the running
// skill (skillDir is its scripts folder), the repo at top, then each harness's user folder.
func SkillCandidates(name, skillDir, top, home string, getenv func(string) string) []string {
	var c []string
	if skillDir != "" {
		c = append(c, filepath.Join(skillDir, "..", "..", name, "SKILL.md"))
	}
	if top != "" {
		c = append(c, filepath.Join(top, ".agents", "skills", name, "SKILL.md"), filepath.Join(top, ".claude", "skills", name, "SKILL.md"))
	}
	for _, d := range []string{
		filepath.Join(home, ".agents", "skills"),
		filepath.Join(envOr(getenv, "SMITHY_HOME", filepath.Join(home, ".smithy")), "skills"),
		filepath.Join(home, ".claude", "skills"),
		filepath.Join(home, ".codex", "skills"),
		filepath.Join(envOr(getenv, "XDG_CONFIG_HOME", filepath.Join(home, ".config")), "opencode", "skills"),
		filepath.Join(home, ".gemini", "skills"),
		filepath.Join(envOr(getenv, "COPILOT_HOME", filepath.Join(home, ".copilot")), "skills"),
		filepath.Join(envOr(getenv, "GROK_HOME", filepath.Join(home, ".grok")), "skills"),
		filepath.Join(home, ".cursor", "skills"),
		filepath.Join(envOr(getenv, "PI_CODING_AGENT_DIR", filepath.Join(home, ".pi", "agent")), "skills"),
		filepath.Join(envOr(getenv, "DSH_HOME", filepath.Join(home, ".dsh")), "skills"),
	} {
		c = append(c, filepath.Join(d, name, "SKILL.md"))
	}
	return c
}

// UserSkills are the skills of your own in ~/.agents/skills and ~/.smithy/skills, name to the
// absolute SKILL.md path; the first folder wins a name both have.
func UserSkills(home string) map[string]string {
	out := map[string]string{}
	for _, d := range []string{filepath.Join(home, ".agents", "skills"), filepath.Join(ghx.Home(), "skills")} {
		es, _ := os.ReadDir(d)
		for _, e := range es {
			f := filepath.Join(d, e.Name(), "SKILL.md")
			if _, dup := out[e.Name()]; dup || strings.HasPrefix(e.Name(), ".") || !isFile(f) {
				continue
			}
			if a, err := filepath.Abs(f); err == nil {
				out[e.Name()] = a
			}
		}
	}
	return out
}

// FindSkill returns the absolute, symlink-free path of an installed skill's SKILL.md.
func FindSkill(name string) (string, bool) {
	cwd, _ := os.Getwd()
	home := os.Getenv("HOME")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	for _, f := range SkillCandidates(name, ghx.SkillDir(), ghx.RepoRoot(cwd), home, os.Getenv) {
		if !isFile(f) {
			continue
		}
		d, err := filepath.EvalSymlinks(filepath.Dir(f))
		if err != nil {
			continue
		}
		if d, err = filepath.Abs(d); err == nil {
			return filepath.Join(d, "SKILL.md"), true
		}
	}
	return "", false
}

// SkillsMain runs `smithy skills` and returns the exit code.
func SkillsMain(args []string) int {
	a := ""
	if len(args) > 0 {
		a = args[0]
	}
	switch a {
	case "install":
		return skillsInstallMain(args[1:])
	case "--list":
		for _, s := range Known {
			if p, ok := FindSkill(s); ok {
				fmt.Println(s, p)
			}
		}
		return 0
	case "", "-h", "--help":
		fmt.Println(skillsUsage)
		return 2
	}
	p, ok := FindSkill(a)
	if !ok {
		fmt.Fprintf(os.Stderr, "smithy skills: %s is not installed (smithy skills --list)\n", a)
		return 1
	}
	fmt.Println(p)
	return 0
}

// HarnessSkillDirs lists the folders the harness h loads skills from: its user folders first,
// then the repo at top (each harness reads its own repo folders, not every one); nil when the
// harness is unknown.
func HarnessSkillDirs(h, top, home string, getenv func(string) string) []string {
	agents := filepath.Join(home, ".agents", "skills")
	var user, repo []string
	switch h {
	case "claude-cli", "claude-desktop":
		user, repo = []string{filepath.Join(home, ".claude", "skills")}, []string{".claude"}
	case "codex":
		user, repo = []string{agents, filepath.Join(home, ".codex", "skills")}, []string{".agents"}
	case "opencode":
		user = []string{filepath.Join(envOr(getenv, "XDG_CONFIG_HOME", filepath.Join(home, ".config")), "opencode", "skills"), filepath.Join(home, ".claude", "skills"), agents}
		repo = []string{".opencode", ".claude", ".agents"}
	case "agy":
		user, repo = []string{filepath.Join(home, ".gemini", "skills"), filepath.Join(home, ".gemini", "antigravity-cli", "skills"), agents}, []string{".agents"}
	case "copilot":
		user, repo = []string{filepath.Join(envOr(getenv, "COPILOT_HOME", filepath.Join(home, ".copilot")), "skills"), agents}, []string{".github", ".agents", ".claude"}
	case "grok":
		user, repo = []string{filepath.Join(envOr(getenv, "GROK_HOME", filepath.Join(home, ".grok")), "skills"), agents}, []string{".grok", ".agents"}
	case "cursor":
		user, repo = []string{filepath.Join(home, ".cursor", "skills"), agents}, []string{".cursor", ".agents"}
	case "pi":
		user, repo = []string{filepath.Join(envOr(getenv, "PI_CODING_AGENT_DIR", filepath.Join(home, ".pi", "agent")), "skills"), agents}, []string{".pi", ".agents"}
	case "dsh":
		user, repo = []string{filepath.Join(envOr(getenv, "DSH_HOME", filepath.Join(home, ".dsh")), "skills"), agents}, []string{".agents"}
	default:
		return nil
	}
	if top != "" {
		for _, r := range repo {
			user = append(user, filepath.Join(top, r, "skills"))
		}
	}
	return user
}

// MissingInHarness returns the skills installed somewhere (FindSkill) that the harness h can't
// load, with the folder to link them into; dir is "" when h is unknown.
func MissingInHarness(h string) (missing []string, dir string) {
	cwd, _ := os.Getwd()
	home := os.Getenv("HOME")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	dirs := HarnessSkillDirs(h, ghx.RepoRoot(cwd), home, os.Getenv)
	if dirs == nil {
		return nil, ""
	}
	for _, s := range Known {
		if _, ok := FindSkill(s); !ok {
			continue
		}
		seen := false
		for _, d := range dirs {
			seen = seen || isFile(filepath.Join(d, s, "SKILL.md"))
		}
		if !seen {
			missing = append(missing, s)
		}
	}
	return missing, dirs[0]
}
