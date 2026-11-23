package warden

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func init() {
	missingInHarness = func(string) ([]string, string) {
		return strings.Fields(os.Getenv("WARDEN_TEST_UNLINKED")), "/h/skills"
	}
	findSkill = func(name string) (string, bool) {
		if root := os.Getenv("WARDEN_TEST_SKILL_ROOT"); root != "" {
			p := filepath.Join(root, name, "SKILL.md")
			_, err := os.Stat(p)
			return p, err == nil
		}
		for _, s := range strings.Fields(os.Getenv("WARDEN_TEST_SKILLS")) {
			if s == name {
				return "/skills/" + s, true
			}
		}
		return "", false
	}
}

func capture(t *testing.T, f func() int) (string, string, int) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	ro, wo, _ := os.Pipe()
	re, we, _ := os.Pipe()
	os.Stdout, os.Stderr = wo, we
	code := f()
	os.Stdout, os.Stderr = oldOut, oldErr
	wo.Close()
	we.Close()
	o, _ := io.ReadAll(ro)
	e, _ := io.ReadAll(re)
	return string(o), string(e), code
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

type batch struct{ dir string }

func newBatch(t *testing.T, plan string) batch {
	t.Helper()
	root := t.TempDir()
	t.Setenv("WARDEN_HOME", filepath.Join(root, "warden"))
	t.Setenv("SMITHY_HOME", filepath.Join(root, "smithy"))
	t.Setenv("HOME", filepath.Join(root, "home"))
	b := batch{filepath.Join(root, "warden", "b1")}
	writeFile(t, filepath.Join(b.dir, "batch.json"), plan)
	return b
}

func (b batch) card(t *testing.T, id, state string) {
	t.Helper()
	writeFile(t, filepath.Join(b.dir, id+".json"), `{"state":"`+state+`"}`)
}

func (b batch) read(t *testing.T, id string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(b.dir, id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestWaitTurn(t *testing.T) {
	const items = `"items":[{"id":"a"},{"id":"b","prereqs":["a"]},{"id":"c"},{"id":"d","prereqs":["a"]}]`
	cases := []struct {
		name   string
		plan   string
		cards  map[string]string
		id     string
		want   int
		out    string
		state  string
		phase  string
		noteIn string
	}{
		{"go when prereq merged", `{` + items + `}`, map[string]string{"a": "merged"}, "b", 0, "go b", "working", "start", ""},
		{"no prereqs and free slot", `{` + items + `}`, nil, "c", 0, "go c", "working", "start", ""},
		{"waits while prereq working", `{` + items + `}`, map[string]string{"a": "working"}, "b", 4, "waiting b", "waiting", "waiting", ""},
		{"prereq pr_open without stack", `{` + items + `}`, map[string]string{"a": "pr_open"}, "b", 4, "waiting b", "waiting", "waiting", ""},
		{"stack accepts pr_open", `{"stack":true,` + items + `}`, map[string]string{"a": "pr_open"}, "b", 0, "go b", "working", "start", ""},
		{"stack accepts ready", `{"stack":true,` + items + `}`, map[string]string{"a": "ready"}, "b", 0, "go b", "working", "start", ""},
		{"stack still waits on working", `{"stack":true,` + items + `}`, map[string]string{"a": "working"}, "b", 4, "waiting b", "waiting", "waiting", ""},
		{"dropped prereq blocks", `{` + items + `}`, map[string]string{"a": "dropped"}, "d", 5, "", "blocked", "waiting", "prereq a was dropped"},
		{"slot limit by max", `{"max":1,` + items + `}`, map[string]string{"a": "merged", "c": "working"}, "b", 4, "waiting b", "waiting", "waiting", ""},
		{"slot free under max", `{"max":2,` + items + `}`, map[string]string{"a": "merged", "c": "working"}, "b", 0, "go b", "working", "start", ""},
		{"max zero never starts", `{"max":0,` + items + `}`, nil, "c", 4, "waiting c", "waiting", "waiting", ""},
		{"default max is five", `{` + items + `}`, map[string]string{"a": "working", "x": "working", "y": "working", "z": "working", "w": "working"}, "c", 4, "waiting c", "waiting", "waiting", ""},
		{"non-working cards free the slot", `{"max":1,` + items + `}`, map[string]string{"a": "merged", "x": "pr_open", "y": "waiting"}, "b", 0, "go b", "working", "start", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := newBatch(t, c.plan)
			for id, st := range c.cards {
				b.card(t, id, st)
			}
			o, _, code := capture(t, func() int { return WaitTurn([]string{"b1", c.id, "0"}) })
			if code != c.want || strings.TrimSpace(o) != c.out {
				t.Errorf("code %d out %q want %d %q", code, o, c.want, c.out)
			}
			card := b.read(t, c.id)
			if card["state"] != c.state || card["phase"] != c.phase {
				t.Errorf("card %v", card)
			}
			if c.noteIn != "" && card["note"] != c.noteIn {
				t.Errorf("note %v", card["note"])
			}
			if _, err := os.Stat(filepath.Join(b.dir, ".slot.lock")); err == nil {
				t.Error("slot lock left behind")
			}
		})
	}
}

func TestWaitTurnLockHeld(t *testing.T) {
	b := newBatch(t, `{"items":[{"id":"c"}]}`)
	if err := os.Mkdir(filepath.Join(b.dir, ".slot.lock"), 0o755); err != nil {
		t.Fatal(err)
	}
	o, _, code := capture(t, func() int { return WaitTurn([]string{"b1", "c", "0"}) })
	if code != 4 || strings.TrimSpace(o) != "waiting c" {
		t.Errorf("code %d out %q", code, o)
	}
}

func TestWaitTurnUsageErrors(t *testing.T) {
	b := newBatch(t, `{"items":[{"id":"a"}]}`)
	if _, e, c := capture(t, func() int { return WaitTurn([]string{"b1"}) }); c != 2 || !strings.Contains(e, "usage") {
		t.Errorf("short args: %d %q", c, e)
	}
	if _, e, c := capture(t, func() int { return WaitTurn([]string{"nope", "a", "0"}) }); c != 2 || !strings.Contains(e, "no batch nope") {
		t.Errorf("unknown batch: %d %q", c, e)
	}
	if _, e, c := capture(t, func() int { return WaitTurn([]string{"b1", "zz", "0"}) }); c != 2 || !strings.Contains(e, "no item zz") {
		t.Errorf("unknown item: %d %q", c, e)
	}
	if _, err := os.Stat(filepath.Join(b.dir, "zz.json")); err == nil {
		t.Error("unknown item must not get a card")
	}
	writeFile(t, filepath.Join(b.dir, "batch.json"), "not json")
	if _, _, c := capture(t, func() int { return WaitTurn([]string{"b1", "a", "0"}) }); c != 2 {
		t.Errorf("bad plan: %d", c)
	}
}

type doctorEnv struct {
	root, bin, home, work string
}

func newDoctor(t *testing.T, tools ...string) doctorEnv {
	t.Helper()
	root := t.TempDir()
	d := doctorEnv{root, filepath.Join(root, "bin"), filepath.Join(root, "home"), filepath.Join(root, "work", "sub")}
	for _, dir := range []string{d.bin, d.home, d.work} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, tool := range tools {
		writeFile(t, filepath.Join(d.bin, tool), "#!/bin/sh\nif [ \"$1 $2\" = \"auth status\" ] && [ -e \""+root+"/signedout\" ]; then exit 1; fi\nexit 0\n")
		if err := os.Chmod(filepath.Join(d.bin, tool), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(filepath.Join(root, "work"))
	t.Setenv("PATH", d.bin)
	t.Setenv("HOME", d.home)
	t.Setenv("SMITHY_HOME", filepath.Join(root, "smithy"))
	t.Setenv("SMITHY_CONFIG", filepath.Join(root, "config.json"))
	t.Setenv("WARDEN_HOME", filepath.Join(root, "warden"))
	t.Setenv("WARDEN_WORKSPACE", d.work)
	t.Setenv("WARDEN_HARNESS", "codex")
	t.Setenv("WARDEN_TEST_SKILLS", "smelt hush scout tend use-smithy")
	t.Setenv("AGENT_CMD", "my-agent")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	writeFile(t, filepath.Join(root, "work", "AGENTS.md"), "x")
	writeFile(t, filepath.Join(root, "config.json"), `{"models":{"codex":{"cheap":"mini","strong":"big"}}}`)
	return d
}

func doctor(t *testing.T, args ...string) (string, int) {
	t.Helper()
	o, _, c := capture(t, func() int { return Doctor(args) })
	return o, c
}

func TestDoctorAllGood(t *testing.T) {
	d := newDoctor(t, "git", "gh")
	o, c := doctor(t)
	if c != 0 {
		t.Fatalf("code %d\n%s", c, o)
	}
	for _, want := range []string{
		"warden doctor\n",
		"  ok    git found",
		"  ok    gh found",
		"  ok    harness: codex (adapter: any; override with WARDEN_HARNESS)",
		"  ok    agent CLI: AGENT_CMD",
		"  ok    gh signed in",
		"  ok    github: gh (the GitHub CLI)",
		"  ok    smelt skill: /skills/smelt",
		"  ok    use-smithy skill: /skills/use-smithy",
		"  ok    model cheap: mini",
		"  ok    model strong: big",
		"  ok    writable " + filepath.Join(d.root, "warden"),
		"  ok    agent instructions for workers: ",
		"AGENTS.md",
		"ready\n",
	} {
		if !strings.Contains(o, want) {
			t.Errorf("missing %q in:\n%s", want, o)
		}
	}
	if strings.Contains(o, "warn") || strings.Contains(o, "FAIL") {
		t.Errorf("unexpected warn or FAIL:\n%s", o)
	}
}

func TestDoctorQuiet(t *testing.T) {
	newDoctor(t, "git", "gh")
	o, c := doctor(t, "--quiet")
	if c != 0 || o != "ready\n" {
		t.Errorf("quiet clean: %d %q", c, o)
	}
	t.Setenv("WARDEN_TEST_SKILLS", "smelt")
	o, c = doctor(t, "--quiet")
	if c != 0 || strings.Contains(o, "  ok") || strings.Contains(o, "warden doctor") || !strings.Contains(o, "  warn  hush skill not installed\n        fix: npx skills add Zovaris/smithy -g --skill hush") {
		t.Errorf("quiet with warns: %d\n%s", c, o)
	}
}

func TestDoctorFailures(t *testing.T) {
	t.Run("git and gh missing", func(t *testing.T) {
		newDoctor(t)
		o, c := doctor(t)
		if c != 1 || !strings.Contains(o, "  FAIL  git missing\n        fix: install git (brew install git)") || !strings.Contains(o, "  FAIL  github: none (no gh, no GH_TOKEN)") || !strings.HasSuffix(o, "not ready\n") {
			t.Errorf("%d\n%s", c, o)
		}
		if strings.Contains(o, "gh signed in") || strings.Contains(o, "gh not signed in") {
			t.Errorf("auth checked without gh:\n%s", o)
		}
	})
	t.Run("no gh but a token", func(t *testing.T) {
		newDoctor(t, "git")
		t.Setenv("GITHUB_TOKEN", "secret-value")
		o, c := doctor(t)
		if c != 0 || !strings.Contains(o, "  ok    github: token (GITHUB_TOKEN, the GitHub API; gh not used)") || strings.Contains(o, "secret-value") {
			t.Errorf("%d\n%s", c, o)
		}
	})
	t.Run("gh signed out", func(t *testing.T) {
		d := newDoctor(t, "git", "gh")
		writeFile(t, filepath.Join(d.root, "signedout"), "")
		o, c := doctor(t)
		if c != 1 || !strings.Contains(o, "  FAIL  gh not signed in\n        fix: gh auth login (or set GH_TOKEN)") {
			t.Errorf("%d\n%s", c, o)
		}
	})
	t.Run("no agent CLI on the any adapter", func(t *testing.T) {
		newDoctor(t, "git", "gh")
		t.Setenv("AGENT_CMD", "")
		o, c := doctor(t)
		if c != 1 || !strings.Contains(o, "  FAIL  no headless agent CLI on PATH (needed to spawn workers here)") {
			t.Errorf("%d\n%s", c, o)
		}
	})
	t.Run("agent CLI found on PATH", func(t *testing.T) {
		newDoctor(t, "git", "gh", "gemini")
		t.Setenv("AGENT_CMD", "")
		o, c := doctor(t)
		if c != 0 || !strings.Contains(o, "  ok    agent CLI: gemini") {
			t.Errorf("%d\n%s", c, o)
		}
	})
	t.Run("unwritable warden home", func(t *testing.T) {
		d := newDoctor(t, "git", "gh")
		writeFile(t, filepath.Join(d.root, "file"), "x")
		t.Setenv("WARDEN_HOME", filepath.Join(d.root, "file", "sub"))
		o, c := doctor(t)
		if c != 1 || !strings.Contains(o, "  FAIL  cannot write "+filepath.Join(d.root, "file", "sub")) {
			t.Errorf("%d\n%s", c, o)
		}
	})
}

func TestDoctorWarnings(t *testing.T) {
	t.Run("skills and models missing", func(t *testing.T) {
		newDoctor(t, "git", "gh")
		t.Setenv("WARDEN_TEST_SKILLS", "")
		os.Remove(os.Getenv("SMITHY_CONFIG"))
		o, c := doctor(t)
		if c != 0 {
			t.Fatalf("warnings must not fail: %d\n%s", c, o)
		}
		for _, want := range []string{
			"  warn  smelt skill not installed\n        fix: npx skills add Zovaris/smithy -g --skill smelt",
			"  warn  tend skill not installed",
			"  warn  no cheap model set for codex\n        fix: ask the user, then: smithy config set-model cheap <name>",
			"  warn  no strong model set for codex",
		} {
			if !strings.Contains(o, want) {
				t.Errorf("missing %q in:\n%s", want, o)
			}
		}
	})
	t.Run("no CLI on the cli adapter is a warning", func(t *testing.T) {
		d := newDoctor(t, "git", "gh")
		t.Setenv("AGENT_CMD", "")
		t.Setenv("WARDEN_HARNESS", "claude-cli")
		o, c := doctor(t)
		if c != 0 || !strings.Contains(o, "  warn  no headless agent CLI on PATH (only consult needs it here)") {
			t.Errorf("%d\n%s", c, o)
		}
		if !strings.Contains(o, "  warn  gh pr merge not in the allow list") || strings.Contains(o, "auto-approve hook") {
			t.Errorf("claude-cli checks:\n%s", o)
		}
		_ = d
	})
	t.Run("no agent instructions", func(t *testing.T) {
		d := newDoctor(t, "git", "gh")
		os.Remove(filepath.Join(d.root, "work", "AGENTS.md"))
		o, c := doctor(t)
		if c != 0 || !strings.Contains(o, "  warn  no AGENTS.md / CLAUDE.md here or in a parent folder") {
			t.Errorf("%d\n%s", c, o)
		}
	})
}

func TestDoctorAgentsLookup(t *testing.T) {
	d := newDoctor(t, "git", "gh")
	os.Remove(filepath.Join(d.root, "work", "AGENTS.md"))
	writeFile(t, filepath.Join(d.root, "work", "CLAUDE.md"), "x")
	o, _ := doctor(t)
	if !strings.Contains(o, "agent instructions for workers: "+filepath.Join(d.root, "work", "CLAUDE.md")) {
		t.Errorf("CLAUDE.md not found walking up:\n%s", o)
	}
	writeFile(t, filepath.Join(d.work, "AGENTS.md"), "x")
	o, _ = doctor(t)
	if !strings.Contains(o, filepath.Join(d.work, "AGENTS.md")) {
		t.Errorf("nearest file wins:\n%s", o)
	}
	t.Setenv("WARDEN_WORKSPACE", "")
	o, _ = doctor(t)
	if !strings.Contains(o, "agent instructions for workers: ") || !strings.Contains(o, "work") {
		t.Errorf("falls back to cwd:\n%s", o)
	}
}

func TestDoctorClaudeSettings(t *testing.T) {
	t.Run("desktop with merge allow", func(t *testing.T) {
		d := newDoctor(t, "git", "gh")
		t.Setenv("WARDEN_HARNESS", "claude-desktop")
		settings := filepath.Join(d.home, ".claude", "settings.json")
		writeFile(t, settings, `{"permissions":{"allow":["Bash(gh pr merge:*)"]}}`)
		o, c := doctor(t)
		if c != 0 || !strings.Contains(o, "  ok    gh pr merge allowed ("+settings+")") || strings.Contains(o, "auto-approve") {
			t.Errorf("%d\n%s", c, o)
		}
	})
	t.Run("smithy pr merge allow rule", func(t *testing.T) {
		for _, rule := range []string{"Bash(gh pr merge*)", "Bash(smithy pr merge:*)", "Bash(smithy:*)", "Bash(/opt/x/scripts/smithy pr merge:*)", "Bash(~/.claude/skills/use-smithy/scripts/smithy:*)"} {
			d := newDoctor(t, "git", "gh")
			t.Setenv("WARDEN_HARNESS", "claude-desktop")
			writeFile(t, filepath.Join(d.home, ".claude", "settings.json"), `{"permissions":{"allow":["`+rule+`"]}}`)
			o, _ := doctor(t)
			if strings.Contains(o, "not in the allow list") || !strings.Contains(o, "  ok    gh pr merge allowed (") {
				t.Errorf("%s:\n%s", rule, o)
			}
		}
	})
	t.Run("non-Bash or unrelated rules do not count", func(t *testing.T) {
		for _, rule := range []string{"Read(~/smithy-notes/**)", "Bash(ls smithy:*)", "Edit(smithy:*)", "Bash(*smithy*)"} {
			d := newDoctor(t, "git", "gh")
			t.Setenv("WARDEN_HARNESS", "claude-desktop")
			writeFile(t, filepath.Join(d.home, ".claude", "settings.json"), `{"permissions":{"allow":["`+rule+`"]}}`)
			o, _ := doctor(t)
			if !strings.Contains(o, "not in the allow list") {
				t.Errorf("%s counted as a merge allow:\n%s", rule, o)
			}
		}
	})
	t.Run("warning names smithy pr merge", func(t *testing.T) {
		newDoctor(t, "git", "gh")
		t.Setenv("WARDEN_HARNESS", "claude-cli")
		if o, _ := doctor(t); !strings.Contains(o, "smithy pr merge") {
			t.Errorf("fix text:\n%s", o)
		}
	})
	t.Run("project local settings", func(t *testing.T) {
		d := newDoctor(t, "git", "gh")
		t.Setenv("WARDEN_HARNESS", "claude-cli")
		writeFile(t, filepath.Join(d.root, "work", ".claude", "settings.local.json"), `{"permissions":{"allow":["Bash(ls)","Bash(gh pr merge:*)"]}}`)
		o, _ := doctor(t)
		if !strings.Contains(o, "  ok    gh pr merge allowed (.claude/settings.local.json)") {
			t.Errorf("local settings:\n%s", o)
		}
	})
	t.Run("invalid settings ignored", func(t *testing.T) {
		d := newDoctor(t, "git", "gh")
		t.Setenv("WARDEN_HARNESS", "claude-cli")
		writeFile(t, filepath.Join(d.home, ".claude", "settings.json"), "{broken")
		o, c := doctor(t)
		if c != 0 || !strings.Contains(o, "  warn  gh pr merge not in the allow list") {
			t.Errorf("%d\n%s", c, o)
		}
	})
}

func TestDoctorSkillNotInHarness(t *testing.T) {
	newDoctor(t, "git", "gh")
	t.Setenv("WARDEN_TEST_UNLINKED", "tend hush")
	o, c := doctor(t, "--quiet")
	if c != 0 || !strings.Contains(o, "  warn  codex can't load tend, hush (installed elsewhere, not in its skills folder)\n        fix: link each into /h/skills") {
		t.Errorf("%d\n%s", c, o)
	}
}

func TestWaitTurnMaxFlag(t *testing.T) {
	newBatch(t, `{"max":0,"items":[{"id":"c"}]}`)
	old := waitStep
	waitStep = 50 * time.Millisecond
	defer func() { waitStep = old }()
	for _, args := range [][]string{{"b1", "c", "--max", "0.01"}, {"b1", "c", "--max=0.01"}, {"b1", "c", "0.01"}} {
		start := time.Now()
		o, _, code := capture(t, func() int { return WaitTurn(args) })
		if el := time.Since(start); code != 4 || strings.TrimSpace(o) != "waiting c" || el < 500*time.Millisecond || el > 3*time.Second {
			t.Errorf("%v: code %d out %q after %v", args, code, o, el)
		}
	}
	for _, bad := range []string{"x", "NaN", "Inf", "-1"} {
		if _, e, c := capture(t, func() int { return WaitTurn([]string{"b1", "c", "--max", bad}) }); c != 2 || !strings.Contains(e, "usage") {
			t.Errorf("bad --max %s: %d %q", bad, c, e)
		}
	}
}

func TestVersionHint(t *testing.T) {
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "VERSION"), []byte("0.14.1\n"), 0o644)
	t.Setenv("SMITHY_SKILL_DIR", d)
	old := BinaryVersion
	defer func() { BinaryVersion = old }()
	BinaryVersion = "0.14.0"
	if h := VersionHint(); !strings.Contains(h, "binary v0.14.0 but skills v0.14.1") || !strings.Contains(h, "sh -s 0.14.1") {
		t.Fatalf("hint: %q", h)
	}
	BinaryVersion = "0.14.1"
	if h := VersionHint(); h != "" {
		t.Fatalf("match should be silent: %q", h)
	}
	BinaryVersion = "dev"
	if h := VersionHint(); h != "" {
		t.Fatalf("dev should be silent: %q", h)
	}
}
