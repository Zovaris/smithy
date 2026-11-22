package docs

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Zovaris/smithy/internal/gitx"
)

const driftUsage = `smithy drift: deterministic doc-drift report against a base (default: origin's default branch).
  smithy drift [base]
A branch name diffs against its merge-base with origin/<branch> when that ref exists (as smithy
scan does); a revision expression is used as given. Without an origin default branch or upstream
the report covers the whole history.
Sections: changed code and the docs that mention it (by path or exported symbol); paths and
` + "`name()`" + ` symbols named in docs that no longer exist; public API added or changed without any
doc change in the diff; the size of the always-loaded files (STANDARDS.md flagged over its rule budget). Report only: exits 0, or 2 for a base git can't resolve. Judgement comes after (references/DRIFT.md).`

var (
	srcExts = map[string]bool{".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true, ".cjs": true,
		".py": true, ".go": true, ".rs": true, ".java": true, ".kt": true, ".kts": true, ".sh": true}
	exportRx = []*regexp.Regexp{
		regexp.MustCompile(`^\s*export\s+(?:default\s+)?(?:async\s+)?(?:function\*?|class|const|let|var|interface|type|enum)\s+([A-Za-z_]\w*)`),
		regexp.MustCompile(`^(?:async\s+)?def\s+([A-Za-z]\w*)\s*\(`),
		regexp.MustCompile(`^class\s+([A-Za-z]\w*)`),
		regexp.MustCompile(`^func\s+(?:\([^)]*\)\s*)?([A-Z]\w*)`),
		regexp.MustCompile(`^type\s+([A-Z]\w*)`),
		regexp.MustCompile(`^\s*pub\s+(?:async\s+)?(?:fn|struct|enum|trait|type|const|mod)\s+([A-Za-z_]\w*)`),
		regexp.MustCompile(`^\s*public\s+(?:static\s+|final\s+|abstract\s+)*(?:class|interface|enum|record|[\w<>\[\],\s]+?)\s+([A-Za-z_]\w*)\s*[({<]`),
		regexp.MustCompile(`^\s*(?:fun|class|object|interface)\s+([A-Za-z_]\w*)`),
	}
	backtickRx = regexp.MustCompile("`([^`\\s]+)`")
	pathExtRx  = regexp.MustCompile(`\.[A-Za-z]{1,5}$`)
	identRx    = regexp.MustCompile(`^[A-Za-z_]\w*$`)
)

// ExportedName is the public symbol a source line declares, or "" when it declares none.
func ExportedName(line string) string {
	for _, rx := range exportRx {
		if m := rx.FindStringSubmatch(line); m != nil {
			return m[1]
		}
	}
	return ""
}

// Symbols is the set of public symbols declared in a source text.
func Symbols(text string) map[string]bool {
	found := map[string]bool{}
	for _, line := range splitLines(text) {
		if s := ExportedName(line); s != "" {
			found[s] = true
		}
	}
	return found
}

// DocFiles lists the Markdown docs drift reads: everything under docs/ plus any README.md,
// AGENTS.md or CLAUDE.md, relative to the current directory and sorted.
func DocFiles() []string {
	var out []string
	walkFiles(".", skipDirs, func(dir, name string) {
		p := strings.TrimPrefix(joinPath(dir, name), "./")
		if strings.HasSuffix(name, ".md") && (strings.HasPrefix(p, "docs/") || name == "README.md" || name == "AGENTS.md" || name == "CLAUDE.md") {
			out = append(out, p)
		}
	})
	sort.Strings(out)
	return out
}

func readOrEmpty(p string) string {
	t, _ := readText(p)
	return t
}

func wordRx(s string) *regexp.Regexp {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(s) + `\b`)
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

type doc struct{ path, text string }

// MentionReasons names why doc text mentions changed file f: its path, base name or stem, or one of
// its exported symbols (names of four characters or more), sorted, at most four.
func MentionReasons(f string, syms map[string]bool, text string) []string {
	base := path.Base(f)
	stem, _ := splitExt(base)
	why := map[string]bool{}
	for _, n := range []string{f, base, stem} {
		if utf8.RuneCountInString(n) > 3 && strings.Contains(text, n) {
			why[n] = true
		}
	}
	for s := range syms {
		if utf8.RuneCountInString(s) > 3 && wordRx(s).MatchString(text) {
			why[s] = true
		}
	}
	out := sortedKeys(why)
	if len(out) > 4 {
		out = out[:4]
	}
	return out
}

// MissingRefs lists the backticked paths and `name()` symbols in one doc that no longer exist;
// corpus is called once, on the first symbol, for the text of the tracked source files.
func MissingRefs(p, text string, corpus func() string) []string {
	var missing []string
	set := map[string]bool{}
	for _, m := range backtickRx.FindAllStringSubmatch(text, -1) {
		set[m[1]] = true
	}
	for _, tok := range sortedKeys(set) {
		t := strings.TrimRight(strings.TrimRight(tok, "()"), ",.:;")
		if strings.Contains(t, "/") && pathExtRx.MatchString(t) && !hasAnyPrefix(t, "http", "~", "$", "<") && !strings.Contains(t, "*") {
			dir := ""
			if i := strings.LastIndexByte(p, '/'); i >= 0 {
				dir = p[:i]
			}
			if !exists(t) && !exists(joinPath(dir, t)) {
				missing = append(missing, fmt.Sprintf("%s: path `%s`", p, tok))
			}
		} else if strings.HasSuffix(tok, "()") && identRx.MatchString(t) {
			if !wordRx(t).MatchString(corpus()) {
				missing = append(missing, fmt.Sprintf("%s: symbol `%s`", p, tok))
			}
		}
	}
	return missing
}

const standardsBudget = 30

var alwaysLoaded = []string{"AGENTS.md", "CLAUDE.md", "docs/agents/STANDARDS.md", "scripts/SCRIPTS.md"}

// SizeLine reports one always-loaded file as "name: N lines, M bytes"; STANDARDS.md also counts its
// "- " rule lines and flags them past the budget in references/STANDARDS-FORMAT.md.
func SizeLine(name, text string) string {
	n := strings.Count(text, "\n")
	if text != "" && !strings.HasSuffix(text, "\n") {
		n++
	}
	out := fmt.Sprintf("%s: %d lines, %d bytes", name, n, len(text))
	if strings.HasSuffix(name, "STANDARDS.md") {
		rules := 0
		for _, l := range splitLines(text) {
			if strings.HasPrefix(l, "- ") {
				rules++
			}
		}
		out += fmt.Sprintf(", %d rules", rules)
		if rules > standardsBudget {
			out += fmt.Sprintf(", over budget (%d)", standardsBudget)
		}
	}
	return out
}

var codeSymRx = regexp.MustCompile(`^[A-Za-z_]\w*(?:\.[A-Za-z_]\w*)?$`)

// fileExtRx matches a short lower-case extension: `README.md` is a file, not Type.Method.
var fileExtRx = regexp.MustCompile(`\.[a-z]{1,4}$`)

// codeShaped reports whether a backticked token looks like a code symbol rather than a word:
// Ident or Type.Method with an upper-case letter, an underscore or a dot.
func codeShaped(tok string) bool {
	if len(tok) < 3 || !codeSymRx.MatchString(tok) || fileExtRx.MatchString(tok) {
		return false
	}
	return strings.ContainsAny(tok, "ABCDEFGHIJKLMNOPQRSTUVWXYZ_.")
}

// GoneSymbols lists the backticked code symbols (Ident or Type.Method) in one doc that were in
// the code at base and are gone now; inBase and inNow report whether a word is in that code.
func GoneSymbols(p, text string, inBase, inNow func(string) bool) []string {
	var gone []string
	set := map[string]bool{}
	for _, m := range backtickRx.FindAllStringSubmatch(text, -1) {
		if codeShaped(m[1]) {
			set[m[1]] = true
		}
	}
	for _, tok := range sortedKeys(set) {
		parts := strings.Split(tok, ".")
		last := parts[len(parts)-1]
		if inNow(last) {
			continue
		}
		existed := true
		for _, w := range parts {
			existed = existed && inBase(w)
		}
		if existed {
			gone = append(gone, fmt.Sprintf("%s: symbol `%s` (in the code at base, gone now)", p, tok))
		}
	}
	return gone
}

// srcSpecs are git pathspecs for the source files drift reads.
func srcSpecs() []string {
	var specs []string
	for _, ext := range sortedKeys(srcExts) {
		specs = append(specs, "*"+ext)
	}
	return specs
}

// codeHas reports whether word is in the source files of rev (the work tree when rev is "").
func codeHas(rev, word string) bool {
	args := []string{"grep", "-q", "-w", "-F", "-e", word}
	if rev != "" {
		args = append(args, rev)
	} else {
		args = append(args, "--untracked")
	}
	args = append(append(args, "--"), srcSpecs()...)
	return exec.Command("git", args...).Run() == nil
}

func hasAnyPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// AddedAPI scans a -U0 diff for added lines that declare a public symbol, as "file: name".
func AddedAPI(diff string) []string {
	var flagged []string
	current, header := "", false
	for _, line := range splitLines(diff) {
		if diffHeader(line, &current, &header) {
			continue
		}
		if strings.HasPrefix(line, "+") {
			if s := ExportedName(line[1:]); s != "" {
				flagged = append(flagged, current+": "+s)
			}
		}
	}
	return flagged
}

// Drift runs `smithy drift [base]`; it exits 2 for a base git cannot resolve, else 0 inside a repo.
func Drift(args []string) int {
	if wantsHelp(args) {
		fmt.Fprintln(os.Stderr, driftUsage)
		return 2
	}
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		fmt.Fprintln(os.Stderr, "smithy drift:", err)
		return 1
	}
	if err := os.Chdir(strings.TrimSpace(string(out))); err != nil {
		fmt.Fprintln(os.Stderr, "smithy drift:", err)
		return 1
	}
	arg := ""
	if len(args) > 0 {
		arg = args[0]
	}
	if arg != "" {
		if msg := checkBase(arg); msg != "" {
			fmt.Fprintln(os.Stderr, "smithy drift:", msg)
			return 2
		}
	}
	base, label := gitx.ResolveBase(".", arg)
	changedSet := map[string]bool{}
	for _, f := range gitNames("diff", "--name-only", base, "--") {
		changedSet[f] = true
	}
	changed := sortedKeys(changedSet)
	var changedSrc, changedDocs []string
	for _, f := range changed {
		if _, ext := splitExt(f); srcExts[ext] {
			changedSrc = append(changedSrc, f)
		}
		if strings.HasSuffix(f, ".md") {
			changedDocs = append(changedDocs, f)
		}
	}
	var docs []doc
	for _, p := range DocFiles() {
		docs = append(docs, doc{p, readOrEmpty(p)})
	}
	fmt.Printf("base: %s\n", label)

	fmt.Println("\n## Changed code and the docs that mention it")
	anyPair := false
	related := map[string]bool{}
	for _, f := range changedDocs {
		related[f] = true
	}
	for _, f := range changedSrc {
		syms := map[string]bool{}
		if exists(f) {
			syms = Symbols(readOrEmpty(f))
		}
		var hits []string
		for _, d := range docs {
			if why := MentionReasons(f, syms, d.text); len(why) > 0 {
				related[d.path] = true
				hits = append(hits, fmt.Sprintf("%s (%s)", d.path, strings.Join(why, ", ")))
			}
		}
		if len(hits) > 0 {
			anyPair = true
			fmt.Printf("- %s\n  %s\n", f, strings.Join(hits, "\n  "))
		}
	}
	if !anyPair {
		fmt.Println("- none")
	}

	fmt.Println("\n## Missing references (named in docs, not found in the repo)")
	corpusText, loaded := "", false
	corpus := func() string {
		if !loaded {
			var parts []string
			for _, f := range gitNames("ls-files") {
				if _, ext := splitExt(f); srcExts[ext] {
					parts = append(parts, readOrEmpty(f))
				}
			}
			corpusText, loaded = strings.Join(parts, "\n"), true
		}
		return corpusText
	}
	var missing []string
	for _, d := range docs {
		missing = append(missing, MissingRefs(d.path, d.text, corpus)...)
		if related[d.path] {
			missing = append(missing, GoneSymbols(d.path, d.text,
				func(w string) bool { return codeHas(base, w) },
				func(w string) bool { return codeHas("", w) })...)
		}
	}
	if len(missing) == 0 {
		fmt.Println("- none")
	} else {
		for _, m := range missing {
			fmt.Println("- " + m)
		}
	}

	fmt.Println("\n## Public API changed without a doc change")
	var flagged []string
	if len(changedSrc) > 0 {
		flagged = AddedAPI(git(append([]string{"-c", "core.quotepath=false", "diff", "-U0", "--no-ext-diff", base, "--"}, changedSrc...)...))
	}
	switch {
	case len(flagged) > 0 && len(changedDocs) == 0:
		for _, x := range flagged {
			fmt.Println("- " + x)
		}
	case len(flagged) == 0:
		fmt.Println("- none")
	default:
		fmt.Printf("- docs changed in this diff (%s); re-read them against: %s\n", strings.Join(changedDocs, ", "), strings.Join(flagged, ", "))
	}

	fmt.Println("\n## Always-loaded files")
	anySize := false
	for _, p := range alwaysLoaded {
		if exists(p) {
			anySize = true
			fmt.Println("- " + SizeLine(p, readOrEmpty(p)))
		}
	}
	if !anySize {
		fmt.Println("- none")
	}
	return 0
}
