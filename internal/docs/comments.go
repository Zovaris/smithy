package docs

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const commentsUsage = `smithy comments: checks the comment policy and prints ` + "`file:line: reason`" + ` per forbidden comment.
  smithy comments [paths...]         files or folders (default: the repo)
  smithy comments --diff <base>      only lines added against <base> (and staged ones)
Allowed: public API doc comments, license/SPDX headers, shebangs, a shell script's header block,
tool directives with a reason, smithy:allow markers, go:build/generate, Rust /// //! and SAFETY. Everything else is
reported. Heuristic: languages ts tsx js jsx mjs cjs py go rs java kt sh. Exit 1 on findings.`

var (
	slashExts = map[string]bool{".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true, ".cjs": true,
		".go": true, ".rs": true, ".java": true, ".kt": true, ".kts": true}
	hashExts  = map[string]bool{".py": true, ".sh": true, ".bash": true}
	jsdocExts = map[string]bool{".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true, ".cjs": true, ".java": true, ".kt": true, ".kts": true}
	taskRx    = regexp.MustCompile(`\b(TODO|FIXME|HACK|XXX)\b`)
	headerRx  = regexp.MustCompile(`(?i)(SPDX-License-Identifier|Copyright|Licensed under|License)`)
	// codeishRx matches comment text shaped like code, not prose that happens to hold a
	// keyword or "O(1)": a line ending in ; { or }, a lone call, an assignment, an import,
	// a short return, or a block header ending in ( { or :.
	codeishRx  = regexp.MustCompile(`(;\s*$|[{}]\s*$|^\s*[\w.$]+\(.*\)\s*;?\s*$|\S\s*(:=|[-+*/]?=)\s*\S|^\s*(import|from|package)\s+[\w./"'@-]+(\s+import\s+.*)?\s*$|^\s*return(\s+\S+){0,2}\s*;?\s*$|^\s*(if|for|while|def|func|class|elif|else)\b.*[({:]\s*$)`)
	goDeclRx   = regexp.MustCompile(`^(func(\s*\([^)]*\))?|type|var|const)\s+[A-Z]`)
	goPkgRx    = regexp.MustCompile(`^package\s`)
	goFieldRx  = regexp.MustCompile(`^\s+[A-Z]\w*(\s|$|\()`)
	eslintRx   = regexp.MustCompile(`^(eslint-disable|eslint-enable)`)
	goDirectRx = regexp.MustCompile(`^(go:|export\s|line\s)`)
	codingRx   = regexp.MustCompile(`^#.*coding[:=]`)
	noqaRx     = regexp.MustCompile(`^noqa\b`)
	noqaRuleRx = regexp.MustCompile(`^noqa:\s*[A-Z]+\d+`)
	ignoreRx   = regexp.MustCompile(`^type:\s*ignore`)
	ignoreCode = regexp.MustCompile(`^type:\s*ignore\[[\w-]+`)
	pyPragmaRx = regexp.MustCompile(`^(pragma: no cover|fmt: (on|off|skip)|isort: |pyright: |mypy: )`)
	shellckRx  = regexp.MustCompile(`^shellcheck\s`)
)

const (
	msgTask   = "TODO/FIXME in a comment; open an issue instead"
	msgBlock  = "block comment; move it to docs or a doc comment"
	msgReason = "directive without a reason"
)

// Finding is one forbidden comment: its 1-based line and the reason.
type Finding struct {
	Line int
	Why  string
}

func stripStrings(line []rune) []rune {
	var out []rune
	var quote rune
	for i := 0; i < len(line); {
		c := line[i]
		if quote != 0 {
			if c == '\\' {
				i += 2
				continue
			}
			if c == quote {
				quote = 0
			}
			out = append(out, ' ')
		} else if c == '"' || c == '\'' || c == '`' {
			quote = c
			out = append(out, ' ')
		} else if c == '/' && regexStart(line, i) {
			j := regexEnd(line, i)
			for ; i < j; i++ {
				out = append(out, ' ')
			}
			continue
		} else {
			out = append(out, c)
		}
		i++
	}
	return out
}

// regexStart reports whether the / at i opens a JS regex literal: not a comment, and after an
// operator or bracket rather than a value.
func regexStart(line []rune, i int) bool {
	if i+1 >= len(line) || line[i+1] == '/' || line[i+1] == '*' {
		return false
	}
	if i > 0 && line[i-1] == '<' {
		return false
	}
	j := i - 1
	for j >= 0 && (line[j] == ' ' || line[j] == '\t') {
		j--
	}
	if j < 0 {
		return true
	}
	if strings.ContainsRune("(,=:[!&|?{};+-*%<>~^", line[j]) {
		return true
	}
	k := j
	for k >= 0 && (line[k] >= 'a' && line[k] <= 'z') {
		k--
	}
	switch string(line[k+1 : j+1]) {
	case "return", "typeof", "case", "in", "of", "await", "yield", "throw", "void", "delete":
		return true
	}
	return false
}

// regexEnd is the index just past the regex literal that opens at i, or i+1 if it never closes.
func regexEnd(line []rune, i int) int {
	class := false
	for j := i + 1; j < len(line); j++ {
		switch c := line[j]; {
		case c == '\\':
			j++
		case c == '[':
			class = true
		case c == ']':
			class = false
		case c == '/' && !class:
			return j + 1
		}
	}
	return i + 1
}

func indexRunes(s []rune, sub string) int {
	i := strings.Index(string(s), sub)
	if i < 0 {
		return -1
	}
	return utf8.RuneCountInString(string(s)[:i])
}

func kind(text string) string {
	if codeishRx.MatchString(text) {
		return "commented-out code"
	}
	return "explanatory comment"
}

func goExportedDecl(lines []string, i int) bool {
	j := i + 1
	for j < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[j]), "//") {
		j++
	}
	if j >= len(lines) {
		return false
	}
	d := lines[j]
	return goDeclRx.MatchString(d) || goPkgRx.MatchString(d) || goFieldRx.MatchString(d)
}

func slashDirective(body, ext string) string {
	b := strings.TrimSpace(body)
	switch {
	case eslintRx.MatchString(b):
		if strings.Contains(b, "--") {
			return "ok"
		}
		return msgReason
	case strings.HasPrefix(b, "@ts-ignore") || strings.HasPrefix(b, "@ts-nocheck"):
		return "@ts-ignore is not allowed; use @ts-expect-error with a reason"
	case strings.HasPrefix(b, "@ts-expect-error"):
		if utf8.RuneCountInString(b) > len("@ts-expect-error")+2 {
			return "ok"
		}
		return msgReason
	case ext == ".go" && strings.HasPrefix(b, "nolint"):
		if strings.Contains(b, "//") {
			return "ok"
		}
		return msgReason
	case ext == ".go" && goDirectRx.MatchString(b):
		return "ok"
	case strings.HasPrefix(b, "smithy:allow"):
		return "ok"
	case strings.HasPrefix(b, "prettier-ignore") || strings.HasPrefix(b, "biome-ignore") ||
		strings.HasPrefix(b, "istanbul ignore") || strings.HasPrefix(b, "c8 ignore"):
		return "ok"
	}
	return ""
}

// CheckSlash applies the policy to a language with // and /* */ comments (ts, js, go, rs, java, kt).
func CheckSlash(lines []string, ext string) []Finding {
	var out []Finding
	inBlock := false
	for i, line := range lines {
		n := i + 1
		if inBlock {
			if taskRx.MatchString(line) {
				out = append(out, Finding{n, msgTask})
			}
			if strings.Contains(line, "*/") {
				inBlock = false
			}
			continue
		}
		raw := []rune(line)
		code := stripStrings(raw)
		s := strings.TrimLeftFunc(string(code), isPySpace)
		if strings.HasPrefix(s, "/*") {
			doc := strings.HasPrefix(s, "/**") && !strings.HasPrefix(s, "/**/")
			header := i < 5 && headerRx.MatchString(line)
			if taskRx.MatchString(line) {
				out = append(out, Finding{n, msgTask})
			} else if !(doc && jsdocExts[ext]) && !header {
				out = append(out, Finding{n, msgBlock})
			}
			if !strings.Contains(s[2:], "*/") {
				inBlock = true
			}
			continue
		}
		idx := indexRunes(code, "//")
		if idx < 0 {
			continue
		}
		before := string(code[:idx])
		body := ""
		if idx+2 < len(raw) {
			body = string(raw[idx+2:])
		}
		if taskRx.MatchString(body) {
			out = append(out, Finding{n, msgTask})
			continue
		}
		if i < 5 && headerRx.MatchString(body) {
			continue
		}
		d := slashDirective(body, ext)
		if d == "ok" {
			continue
		}
		if d != "" {
			out = append(out, Finding{n, d})
			continue
		}
		if strings.TrimFunc(before, isPySpace) != "" {
			out = append(out, Finding{n, "trailing " + kind(body)})
			continue
		}
		if ext == ".rs" && (strings.HasPrefix(body, "/") || strings.HasPrefix(body, "!") || strings.HasPrefix(strings.TrimFunc(body, isPySpace), "SAFETY:")) {
			continue
		}
		if ext == ".go" && goExportedDecl(lines, i) {
			continue
		}
		out = append(out, Finding{n, kind(body)})
	}
	return out
}

// CheckHash applies the policy to a language with # comments (py, sh, bash).
func CheckHash(lines []string, ext string) []Finding {
	var out []Finding
	header := true
	shell := ext == ".sh" || ext == ".bash"
	for i, line := range lines {
		n := i + 1
		if i == 0 && strings.HasPrefix(line, "#!") {
			continue
		}
		raw := []rune(line)
		code := stripStrings(raw)
		s := strings.TrimLeftFunc(string(code), isPySpace)
		if ext == ".py" && i < 2 && codingRx.MatchString(s) {
			continue
		}
		idx := indexRunes(code, "#")
		if idx < 0 {
			if s != "" {
				header = false
			}
			continue
		}
		before := string(code[:idx])
		body := ""
		if idx+1 < len(raw) {
			body = string(raw[idx+1:])
		}
		if taskRx.MatchString(body) {
			out = append(out, Finding{n, msgTask})
			continue
		}
		if shell && before != "" && !strings.HasSuffix(before, " ") && !strings.HasSuffix(before, "\t") {
			continue
		}
		b := strings.TrimFunc(body, isPySpace)
		if shell && header && strings.TrimFunc(before, isPySpace) == "" {
			continue
		}
		if i < 5 && headerRx.MatchString(b) {
			continue
		}
		if ext == ".py" {
			if noqaRx.MatchString(b) {
				if !noqaRuleRx.MatchString(b) {
					out = append(out, Finding{n, "bare noqa; name the rule"})
				}
				continue
			}
			if ignoreRx.MatchString(b) {
				if !ignoreCode.MatchString(b) {
					out = append(out, Finding{n, "bare type: ignore; name the error code"})
				}
				continue
			}
			if pyPragmaRx.MatchString(b) {
				continue
			}
		}
		if shell && shellckRx.MatchString(b) || strings.HasPrefix(strings.TrimSpace(b), "smithy:allow") {
			continue
		}
		if strings.TrimFunc(before, isPySpace) != "" {
			out = append(out, Finding{n, "trailing " + kind(b)})
		} else {
			header = false
			out = append(out, Finding{n, kind(b)})
		}
	}
	return out
}

func isPySpace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '\v', '\f', '\x1c', '\x1d', '\x1e', '\x1f', '\u0085':
		return true
	}
	return r > 0x7f && (r == 0xa0 || r == 0x1680 || (r >= 0x2000 && r <= 0x200a) || r == 0x2028 || r == 0x2029 || r == 0x202f || r == 0x205f || r == 0x3000)
}

// CheckFile returns the findings for one source text; ok is false for languages it does not check.
func CheckFile(name, text string) ([]Finding, bool) {
	_, ext := splitExt(name)
	switch {
	case slashExts[ext]:
		return CheckSlash(readLines(text), ext), true
	case hashExts[ext]:
		return CheckHash(readLines(text), ext), true
	}
	return nil, false
}

func commentFiles(args []string) []string {
	if len(args) > 0 && args[0] == "--diff" {
		base := "HEAD"
		if len(args) > 1 {
			base = args[1]
		}
		names := gitNames("diff", "--name-only", "--diff-filter=ACMR", base, "--")
		names = append(names, gitNames("diff", "--name-only", "--diff-filter=ACMR", "--cached", "--")...)
		set := map[string]bool{}
		for _, f := range names {
			if isFile(f) {
				set[f] = true
			}
		}
		out := make([]string, 0, len(set))
		for f := range set {
			out = append(out, f)
		}
		sort.Strings(out)
		return out
	}
	if len(args) == 0 {
		args = []string{"."}
	}
	var found []string
	for _, p := range args {
		if isFile(p) {
			found = append(found, p)
			continue
		}
		walkFiles(p, skipDirs, func(dir, name string) { found = append(found, joinPath(dir, name)) })
	}
	sort.Strings(found)
	return found
}

var hunkRe = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))?`)

// diffHeader tracks the file of a unified diff: file headers count only between
// `diff --git` and the first hunk, so an added line starting with "++ " is never one.
// It reports whether l was a header line.
func diffHeader(l string, file *string, header *bool) bool {
	switch {
	case strings.HasPrefix(l, "diff --git "):
		*header, *file = true, ""
		return true
	case *header && strings.HasPrefix(l, "@@"):
		*header = false
		return false
	case *header && strings.HasPrefix(l, "+++ "):
		*file = diffName(strings.TrimPrefix(l, "+++ "))
		return true
	}
	return *header
}

// diffName turns a `+++` path into a repo path: git's C-style quoting is undone and the
// b/ prefix dropped; /dev/null is "".
func diffName(s string) string {
	s = strings.TrimSuffix(s, "\t")
	if strings.HasPrefix(s, "\"") {
		if u, err := strconv.Unquote(s); err == nil {
			s = u
		}
	}
	if s == "/dev/null" {
		return ""
	}
	return strings.TrimPrefix(s, "b/")
}

// AddedLines maps each file in a -U0 unified diff to the line numbers it adds.
func AddedLines(diff string) map[string]map[int]bool {
	out := map[string]map[int]bool{}
	file, header := "", false
	for _, l := range strings.Split(diff, "\n") {
		if diffHeader(l, &file, &header) {
			continue
		}
		m := hunkRe.FindStringSubmatch(l)
		if m == nil || file == "" {
			continue
		}
		start, _ := strconv.Atoi(m[1])
		n := 1
		if m[2] != "" {
			n, _ = strconv.Atoi(m[2])
		}
		if out[file] == nil {
			out[file] = map[int]bool{}
		}
		for i := start; i < start+n; i++ {
			out[file][i] = true
		}
	}
	return out
}

// Comments runs `smithy comments [paths...]` or `smithy comments --diff <base>`.
func Comments(args []string) int {
	if wantsHelp(args) {
		fmt.Fprintln(os.Stderr, commentsUsage)
		return 2
	}
	findings := 0
	var added map[string]map[int]bool
	if len(args) > 0 && args[0] == "--diff" {
		base := "HEAD"
		if len(args) > 1 {
			base = args[1]
		}
		if msg := checkBase(base); msg != "" {
			fmt.Fprintln(os.Stderr, "smithy comments:", msg)
			return 2
		}
		q := []string{"-c", "core.quotepath=false", "diff", "-U0", "--no-color", "--no-ext-diff"}
		added = AddedLines(git(append(q, base, "--")...) + "\n" + git(append(q, "--cached", "--")...))
	}
	for _, f := range commentFiles(args) {
		_, ext := splitExt(f)
		if !slashExts[ext] && !hashExts[ext] {
			continue
		}
		text, ok := readText(f)
		if !ok {
			continue
		}
		res, _ := CheckFile(f, text)
		for _, r := range res {
			if added != nil && !added[f][r.Line] {
				continue
			}
			fmt.Printf("%s:%d: %s\n", f, r.Line, r.Why)
			findings++
		}
	}
	if findings > 0 {
		fmt.Fprintf(os.Stderr, "%d comment(s) break the policy: move the why to the module README or an ADR, or delete them\n", findings)
		return 1
	}
	return 0
}
