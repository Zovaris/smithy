// Package plan holds smelt's planning helpers: the repo's test tooling (stack), the changed
// source files (changed), the green-phase test guard (guard-tests) and where a plan's specs and
// tasks live (task-dir).
package plan

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Line is one `key: value` line of `smithy stack`; an empty Value prints as none.
type Line struct{ Key, Value string }

const stackUsage = `smelt: detects the repo's test tooling so agents don't guess. Read-only.
  smithy stack [repo-dir]
Prints ` + "`key: value`" + ` lines; a missing tool prints ` + "`key: none`" + `.`

type repo struct {
	dir string
	pkg map[string]any
}

func (r repo) file(name string) bool {
	st, err := os.Stat(filepath.Join(r.dir, name))
	return err == nil && st.Mode().IsRegular()
}

func (r repo) glob(pattern string) []string {
	m, _ := filepath.Glob(filepath.Join(r.dir, pattern))
	return m
}

func truthy(v any) bool {
	if v == nil {
		return false
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return true
}

func (r repo) script(name string) bool {
	scripts, ok := r.pkg["scripts"].(map[string]any)
	if !ok || !truthy(scripts[name]) {
		return false
	}
	s, isStr := scripts[name].(string)
	return !isStr || s != ""
}

func (r repo) dep(name string) bool {
	for _, k := range []string{"dependencies", "devDependencies"} {
		if m, ok := r.pkg[k].(map[string]any); ok && truthy(m[name]) {
			return true
		}
	}
	return false
}

func (r repo) pydep(name string) bool {
	re := regexp.MustCompile(`(?i)^[^#]*\b` + regexp.QuoteMeta(name) + `\b`)
	files := append([]string{filepath.Join(r.dir, "pyproject.toml")}, r.glob("requirements*.txt")...)
	files = append(files, filepath.Join(r.dir, "setup.cfg"))
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 64*1024), 1<<20)
		for sc.Scan() {
			if re.MatchString(sc.Text()) {
				fh.Close()
				return true
			}
		}
		fh.Close()
	}
	return false
}

func or(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}

func join(prev, sep, next string) string {
	if prev == "" {
		return next
	}
	return prev + sep + next
}

// Stack detects the test tooling of the repo at dir.
func Stack(dir string) []Line {
	r := repo{dir: dir}
	var lines []Line
	out := func(k, v string) { lines = append(lines, Line{k, v}) }

	pm := "npm"
	if r.file("pnpm-lock.yaml") {
		pm = "pnpm"
	}
	if r.file("yarn.lock") {
		pm = "yarn"
	}
	if r.file("bun.lockb") || r.file("bun.lock") {
		pm = "bun"
	}

	var langs []string
	hasPkg := r.file("package.json")
	if hasPkg {
		langs = append(langs, "js")
		if b, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
			_ = json.Unmarshal(b, &r.pkg)
		}
	}
	if r.file("tsconfig.json") {
		langs = append(langs, "ts")
	}
	python := r.file("pyproject.toml") || len(r.glob("requirements*.txt")) > 0 || r.file("setup.py")
	if python {
		langs = append(langs, "python")
	}
	cargo := r.file("Cargo.toml")
	if cargo {
		langs = append(langs, "rust")
	}
	gomod := r.file("go.mod")
	if gomod {
		langs = append(langs, "go")
	}
	out("lang", or(strings.Join(langs, " "), "unknown"))

	var test, single, typecheck, lint, e2e, mutation, pbt, cov, dev string
	if hasPkg {
		out("pkg-manager", pm)
		if r.script("test") {
			test = pm + " test"
			if pm == "bun" {
				test = "bun run test"
			}
		}
		if r.dep("vitest") {
			single, cov = "npx vitest run <file>", "npx vitest run --coverage --changed"
		} else if r.dep("jest") {
			single, cov = "npx jest <file>", "npx jest --coverage --changedSince=<base>"
		}
		if r.script("typecheck") {
			typecheck = pm + " run typecheck"
		} else if r.file("tsconfig.json") {
			typecheck = "npx tsc --noEmit"
		}
		if r.script("lint") {
			lint = pm + " run lint"
		}
		if r.script("dev") {
			dev = pm + " run dev"
		}
		if r.dep("@playwright/test") {
			e2e = "npx playwright test"
		}
		if r.dep("@stryker-mutator/core") {
			mutation = "npx stryker run --mutate <files> --incremental"
		}
		if r.dep("fast-check") {
			pbt = "fast-check"
		}
	}
	if python {
		run := ""
		if r.file("uv.lock") {
			run = "uv run "
		}
		if r.file("poetry.lock") {
			run = "poetry run "
		}
		test = or(test, run+"pytest -q")
		single = or(single, run+"pytest -q <file>")
		if r.pydep("mypy") {
			typecheck = join(typecheck, "; ", run+"mypy .")
		}
		if r.pydep("pyright") {
			typecheck = join(typecheck, "; ", run+"pyright")
		}
		if r.pydep("ruff") {
			lint = join(lint, "; ", run+"ruff check .")
		}
		if r.pydep("pytest-playwright") {
			e2e = or(e2e, run+"pytest -q <e2e dir>")
		}
		if r.pydep("mutmut") {
			mutation = or(mutation, run+"mutmut run --paths-to-mutate <files>")
		}
		if r.pydep("cosmic-ray") {
			mutation = or(mutation, run+"cosmic-ray (see its config)")
		}
		if r.pydep("hypothesis") {
			pbt = join(pbt, ", ", "hypothesis")
		}
		if r.pydep("diff-cover") {
			cov = or(cov, run+"pytest --cov --cov-report=xml && diff-cover coverage.xml --compare-branch=<base>")
		}
	}
	if cargo {
		test, single = or(test, "cargo test"), or(single, "cargo test <name>")
		typecheck, lint = or(typecheck, "cargo check"), or(lint, "cargo clippy")
		if _, err := exec.LookPath("cargo-mutants"); err == nil {
			mutation = or(mutation, "cargo mutants --in-diff <diff-file>")
		}
		if b, err := os.ReadFile(filepath.Join(dir, "Cargo.toml")); err == nil && strings.Contains(string(b), "proptest") {
			pbt = or(pbt, "proptest")
		}
	}
	if gomod {
		test, single = or(test, "go test -count=1 ./..."), or(single, "go test ./<pkg> -run <Name>")
		typecheck, pbt = or(typecheck, "go vet ./..."), or(pbt, "go fuzz (go test -fuzz)")
	}

	var over struct {
		Stack map[string]string `json:"stack"`
	}
	_ = json.Unmarshal(trustedConfig(dir), &over)
	set := func(k string, v *string) {
		if o := strings.TrimSpace(over.Stack[k]); o != "" {
			*v = o
		}
	}
	set("test", &test)
	set("single", &single)
	set("typecheck", &typecheck)
	set("lint", &lint)
	set("e2e", &e2e)
	set("mutation", &mutation)
	set("property", &pbt)
	set("coverage", &cov)
	set("dev", &dev)
	out("test", test)
	out("single", single)
	out("typecheck", typecheck)
	out("lint", lint)
	out("e2e", e2e)
	out("mutation", mutation)
	out("property", pbt)
	out("coverage", cov)
	out("dev", dev)
	b, _ := exec.Command("git", "-C", dir, "symbolic-ref", "-q", "--short", "refs/remotes/origin/HEAD").Output()
	base := strings.TrimPrefix(strings.TrimSpace(string(b)), "origin/")
	for _, c := range []string{"main", "master"} {
		if base != "" {
			break
		}
		if exec.Command("git", "-C", dir, "rev-parse", "-q", "--verify", "refs/remotes/origin/"+c).Run() == nil {
			base = c
		}
	}
	out("base", base)
	for _, f := range []string{"AGENTS.md", "CLAUDE.md", "docs/GLOSSARY.md", "docs/agents/STANDARDS.md"} {
		if r.file(f) {
			out("doc", f)
		}
	}
	if st, err := os.Stat(filepath.Join(dir, "docs", "adr")); err == nil && st.IsDir() {
		entries, _ := os.ReadDir(filepath.Join(dir, "docs", "adr"))
		n := 0
		for _, e := range entries {
			if !strings.HasPrefix(e.Name(), ".") && strings.HasSuffix(e.Name(), ".md") {
				n++
			}
		}
		out("adrs", fmt.Sprint(n))
	}
	return lines
}

// trustedConfig is the repo config as the default branch has it, or, when no branch tracks it,
// the main checkout's local file. A branch's own edits never set the commands smelt runs.
func trustedConfig(dir string) []byte {
	if b, err := exec.Command("git", "-C", dir, "show", "refs/remotes/origin/HEAD:.smithy/config.json").Output(); err == nil {
		return b
	}
	if exec.Command("git", "-C", dir, "ls-files", "--error-unmatch", ".smithy/config.json").Run() == nil {
		return nil
	}
	top := dir
	if c, err := exec.Command("git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-common-dir").Output(); err == nil && filepath.Base(strings.TrimSpace(string(c))) == ".git" {
		top = filepath.Dir(strings.TrimSpace(string(c)))
	}
	b, _ := os.ReadFile(filepath.Join(top, ".smithy", "config.json"))
	return b
}

// StackMain runs `smithy stack [repo-dir]` and returns the exit code.
func StackMain(args []string) int {
	dir := "."
	if len(args) > 0 {
		if args[0] == "-h" || args[0] == "--help" {
			fmt.Fprintln(os.Stderr, stackUsage)
			return 0
		}
		dir = args[0]
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		fmt.Fprintf(os.Stderr, "smithy stack: cd: %s: No such file or directory\n", dir)
		return 2
	}
	for _, l := range Stack(dir) {
		fmt.Printf("%-12s %s\n", l.Key+":", or(l.Value, "none"))
	}
	return 0
}
