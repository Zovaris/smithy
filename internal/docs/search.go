package docs

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Zovaris/smithy/internal/search"
	"github.com/Zovaris/smithy/internal/text"
)

const searchUsage = `smithy search: searches project docs and decisions, best sources first.
  smithy search "<query>" [--adr|--glossary|--all|--code] [--regex] [--max N]
Order: ADRs (docs/adr) > glossary > module READMEs > other docs (docs/**, README, AGENTS.md,
CLAUDE.md). Case-insensitive fixed-string match. Prints ` + "`file:line: text`" + `, at most N lines
(default 40). Exit 1 when nothing matches.
--code searches every file of the repo instead (ignored, hidden and binary files skipped) with the
ripgrep shipped next to smithy, rg on PATH, git ls-files or a plain walk, fastest first;
SMITHY_RG=<path> or off overrides the rg it picks. --regex reads the query as a regular
expression. Output is capped at N matches, 220 characters a line and 64 KB.`

var searchPrune = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true, "build": true, "target": true, ".venv": true,
}

func findMD(root string, keep func(p, name string) bool) []string {
	var out []string
	var walk func(dir string)
	walk = func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			p := dir + "/" + e.Name()
			if keep(p, e.Name()) {
				out = append(out, p)
			}
			if e.IsDir() {
				walk(p)
			}
		}
	}
	walk(root)
	sort.Strings(out)
	return out
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// SearchADRs lists docs/adr/**/*.md, sorted.
func SearchADRs() []string {
	if !isDir("docs/adr") {
		return nil
	}
	return findMD("docs/adr", func(_, name string) bool { return strings.HasSuffix(name, ".md") })
}

// SearchGlossary lists the glossary files that exist.
func SearchGlossary() []string {
	var out []string
	for _, f := range []string{"docs/GLOSSARY.md", "GLOSSARY.md"} {
		if isFile(f) {
			out = append(out, f)
		}
	}
	return out
}

// SearchModules lists README.md files below the root, outside docs/ and the pruned folders.
func SearchModules() []string {
	var out []string
	var walk func(dir string)
	walk = func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			p := dir + "/" + e.Name()
			if e.IsDir() && searchPrune[e.Name()] && dir == "." {
				continue
			}
			if e.Name() == "README.md" {
				out = append(out, strings.TrimPrefix(p, "./"))
			}
			if e.IsDir() {
				walk(p)
			}
		}
	}
	walk(".")
	var keep []string
	for _, p := range out {
		if p != "README.md" && !strings.HasPrefix(p, "docs/") {
			keep = append(keep, p)
		}
	}
	sort.Strings(keep)
	return keep
}

// SearchOthers lists the remaining docs: docs/**/*.md outside docs/adr and glossaries, plus the
// root README.md, AGENTS.md and CLAUDE.md.
func SearchOthers() []string {
	set := map[string]bool{}
	if isDir("docs") {
		for _, p := range findMD("docs", func(p, name string) bool {
			return strings.HasSuffix(name, ".md") && !strings.HasPrefix(p, "docs/adr/") && name != "GLOSSARY.md"
		}) {
			set[p] = true
		}
	}
	for _, f := range []string{"README.md", "AGENTS.md", "CLAUDE.md"} {
		if isFile(f) {
			set[f] = true
		}
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// SearchHits returns `file:line:text` for every line of files that contains q, ignoring case.
func SearchHits(q string, files []string) []string {
	rx := regexp.MustCompile("(?i)" + regexp.QuoteMeta(q))
	var out []string
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil || isBinary(b) {
			continue
		}
		text := string(b)
		text = strings.TrimSuffix(text, "\n")
		if len(b) == 0 {
			continue
		}
		for i, line := range strings.Split(text, "\n") {
			if rx.MatchString(line) {
				out = append(out, f+":"+strconv.Itoa(i+1)+":"+line)
			}
		}
	}
	return out
}

func isBinary(b []byte) bool {
	n := len(b)
	if n > 8000 {
		n = 8000
	}
	for _, c := range b[:n] {
		if c == 0 {
			return true
		}
	}
	return false
}

// Search runs `smithy search "<query>" [--adr|--glossary|--all] [--max N]`.
func Search(args []string) int {
	if wantsHelp(args) {
		fmt.Fprintln(os.Stderr, searchUsage)
		return 2
	}
	q, scope, max, regex := "", "all", 40, false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--":
			if i+1 < len(args) {
				q = strings.Join(args[i+1:], " ")
			}
			i = len(args)
		case "--adr":
			scope = "adr"
		case "--glossary":
			scope = "glossary"
		case "--all":
			scope = "all"
		case "--code":
			scope = "code"
		case "--regex":
			regex = true
		case "--max":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, searchUsage)
				return 2
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil || n < 0 {
				fmt.Fprintln(os.Stderr, searchUsage)
				return 2
			}
			max = n
			i++
		default:
			q = args[i]
		}
	}
	if q == "" {
		fmt.Fprintln(os.Stderr, searchUsage)
		return 2
	}
	if root := strings.TrimSpace(git("rev-parse", "--show-toplevel")); root != "" {
		_ = os.Chdir(root)
	}
	if scope == "code" {
		return searchCode(q, regex, max)
	}
	var out []string
	switch scope {
	case "adr":
		out = SearchHits(q, SearchADRs())
	case "glossary":
		out = SearchHits(q, SearchGlossary())
	default:
		for _, group := range [][]string{SearchADRs(), SearchGlossary(), SearchModules(), SearchOthers()} {
			out = append(out, SearchHits(q, group)...)
		}
	}
	if len(out) == 0 {
		fmt.Fprintf(os.Stderr, "no match for \"%s\"\n", q)
		return 1
	}
	for i, line := range out {
		if i >= max {
			break
		}
		if r := []rune(line); len(r) > 220 {
			line = string(r[:220])
		}
		fmt.Println(line)
	}
	if len(out) > max {
		fmt.Printf("… %d more (narrow the query or use --adr/--glossary)\n", len(out)-max)
	}
	return 0
}

func searchCode(q string, regex bool, max int) int {
	binDir := ""
	if exe, err := os.Executable(); err == nil {
		if r, err := filepath.EvalSymlinks(exe); err == nil {
			exe = r
		}
		binDir = filepath.Dir(exe)
	}
	if max == 0 {
		max = 40
	}
	r, err := search.Run(search.Options{Root: ".", Pattern: q, Fixed: !regex, IgnoreCase: !regex,
		MaxMatches: max, MaxLine: 220, Rg: search.FindRg(binDir)})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if len(r.Matches) == 0 {
		fmt.Fprintf(os.Stderr, "no match for \"%s\"\n", text.Clean(q))
		return 1
	}
	for _, m := range r.Matches {
		fmt.Println(m)
	}
	if r.Truncated {
		fmt.Printf("… capped at %d matches or %d bytes (narrow the query)\n", max, search.DefaultMaxBytes)
	}
	return 0
}
