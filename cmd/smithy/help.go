package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

const width = 80

const exitCodes = "Exit codes: 0 success, 1 failure, 2 usage error (3 where a command's help says)."

// style colors names and descriptions on a terminal; plain when w is not one or NO_COLOR is set.
type style struct{ on bool }

func styleFor(w io.Writer) style {
	f, ok := w.(*os.File)
	return style{on: ok && terminal(f) && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"}
}

func (s style) strong(t string) string { return s.wrap("1", t) }
func (s style) dim(t string) string    { return s.wrap("2", t) }
func (s style) wrap(code, t string) string {
	if !s.on || t == "" {
		return t
	}
	return "\x1b[" + code + "m" + t + "\x1b[0m"
}

func summary(name string, c command) string {
	if c.help != "" {
		return c.help
	}
	return harnessSummary[name]
}

func harnessOnlyHere(name string) bool {
	_, ok := harnessSummary[name]
	return ok && !harnessBuild
}

// topHelp is `smithy help`: what smithy is, then the commands people run by group; all adds
// the commands the skills call.
func topHelp(w io.Writer, all bool) {
	s := styleFor(w)
	build := "the skills build (this one)"
	other := "the harness build"
	if harnessBuild {
		build, other = "the harness build (this one)", "the skills build"
	}
	intro := fmt.Sprintf("smithy %s turns your coding agent into a small engineering team. "+
		"The skills call it for evidence, waits and batches; you run it to set up, check and watch the work. "+
		"Two builds: %s and %s; the harness build adds the daemon, chat, web UI and board.", version, build, other)
	for _, l := range wrapText(intro, 0) {
		fmt.Fprintln(w, l)
	}
	byGroup := map[string][]string{}
	for n, c := range commands {
		g := helpDocs[n].group
		if _, ok := helpDocs[n]; !ok {
			g = groupSkills
		}
		if g == groupHidden || summary(n, c) == "" {
			continue
		}
		byGroup[g] = append(byGroup[g], n)
	}
	marked := false
	for _, g := range groupOrder {
		if g == groupSkills && !all {
			continue
		}
		names := byGroup[g]
		if len(names) == 0 {
			continue
		}
		sort.Strings(names)
		fmt.Fprintln(w)
		fmt.Fprintln(w, s.strong(g))
		for _, n := range names {
			text := summary(n, commands[n])
			if harnessOnlyHere(n) {
				text += " (harness build)"
				marked = true
			}
			lines := wrapText(text, 0, width-16)
			fmt.Fprintf(w, "  %s%s%s\n", s.strong(n), strings.Repeat(" ", max(1, 14-len(n))), s.dim(lines[0]))
			for _, l := range lines[1:] {
				fmt.Fprintf(w, "%16s%s\n", "", s.dim(l))
			}
		}
	}
	fmt.Fprintln(w)
	if marked {
		for _, l := range wrapText("(harness build): get smithy-harness-<os>-<arch> from "+
			"https://github.com/Zovaris/smithy/releases, or go build -tags harness ./cmd/smithy.", 0) {
			fmt.Fprintln(w, l)
		}
	}
	more := "Run smithy help <command> for usage and examples."
	if !all {
		more += " smithy help --all also lists the commands the skills call."
	}
	for _, l := range wrapText(more+" Not on PATH? smithy setup path.", 0) {
		fmt.Fprintln(w, l)
	}
	fmt.Fprintln(w, exitCodes)
}

// commandHelp is `smithy help <name> [sub]`: summary, usage and description, examples, see also.
func commandHelp(w io.Writer, name string, rest []string) {
	s := styleFor(w)
	c := commands[name]
	d := helpDocs[name]
	title, text, examples := "smithy "+name, c.usage, d.examples
	see := make([]string, 0, len(d.see)+1)
	for _, x := range d.see {
		see = append(see, "smithy help "+x)
	}
	head := summary(name, c)
	if len(rest) > 0 {
		if u := subUsage(name, c.usage, rest[0]); u != "" {
			title += " " + rest[0]
			text = u
			head = "subcommand of smithy " + name
			var sub []string
			for _, e := range examples {
				if strings.HasPrefix(e, title+" ") || e == title {
					sub = append(sub, e)
				}
			}
			if len(sub) > 0 {
				examples = sub
			}
			see = append([]string{"smithy help " + name}, see...)
		}
	}
	if harnessOnlyHere(name) {
		head += " (harness build)"
	}
	for i, l := range wrapText(title+": "+head, 0) {
		if i == 0 {
			l = s.strong(title) + ":" + strings.TrimPrefix(l, title+":")
		}
		fmt.Fprintln(w, l)
	}
	fmt.Fprintln(w)
	for _, l := range reflow(strings.Split(strings.TrimRight(text, "\n"), "\n")) {
		fmt.Fprintln(w, l)
	}
	if len(examples) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, s.strong("Examples:"))
		for _, e := range examples {
			for _, l := range wrapLine("  " + e) {
				fmt.Fprintln(w, l)
			}
		}
	}
	if len(see) > 0 {
		fmt.Fprintln(w)
		for _, l := range wrapText("See also: "+strings.Join(see, ", "), 0) {
			fmt.Fprintln(w, l)
		}
	}
	fmt.Fprintln(w, exitCodes)
}

// reflow fits usage text to the width: a run of plain prose lines with one too long is
// rewrapped as a paragraph; command and indented lines wrap on their own.
func reflow(lines []string) []string {
	prose := func(l string) bool {
		t := strings.TrimSpace(l)
		return t != "" && l == t && !strings.HasPrefix(t, "usage:") && !strings.HasPrefix(t, "smithy ")
	}
	var out []string
	for i := 0; i < len(lines); i++ {
		if !prose(lines[i]) {
			out = append(out, wrapLine(lines[i])...)
			continue
		}
		j, long := i, false
		for j < len(lines) && prose(lines[j]) {
			long = long || len([]rune(lines[j])) > width
			j++
		}
		if long {
			out = append(out, wrapText(strings.Join(lines[i:j], " "), 0)...)
		} else {
			out = append(out, lines[i:j]...)
		}
		i = j - 1
	}
	return out
}

// wrapLine fits one usage line to the width; a command line's continuations hang 4 deeper.
func wrapLine(l string) []string {
	if len([]rune(l)) <= width {
		return []string{l}
	}
	in := len(l) - len(strings.TrimLeft(l, " "))
	t := strings.TrimSpace(l)
	hang := 0
	if strings.HasPrefix(t, "usage:") || strings.HasPrefix(t, "smithy ") {
		hang = 4
	}
	lines := wrapText(t, in, width-in-hang)
	for i := 1; i < len(lines); i++ {
		lines[i] = strings.Repeat(" ", hang) + lines[i]
	}
	return lines
}

// wrapText breaks text into lines of at most w runes (default the width), each indented by in.
func wrapText(text string, in int, w ...int) []string {
	max := width - in
	if len(w) > 0 {
		max = w[0]
	}
	pad := strings.Repeat(" ", in)
	var lines []string
	cur := ""
	for _, word := range strings.Fields(text) {
		for len([]rune(word)) > max {
			if cur != "" {
				lines = append(lines, pad+cur)
				cur = ""
			}
			r := []rune(word)
			lines = append(lines, pad+string(r[:max]))
			word = string(r[max:])
		}
		switch {
		case cur == "":
			cur = word
		case len([]rune(cur))+1+len([]rune(word)) <= max:
			cur += " " + word
		default:
			lines = append(lines, pad+cur)
			cur = word
		}
	}
	if cur != "" || len(lines) == 0 {
		lines = append(lines, pad+cur)
	}
	return lines
}

// usageLine is the first usage line of a command (or of its subcommand sub), for error output.
func usageLine(name string, c command, sub string) string {
	u := c.usage
	if sub != "" {
		if s := subUsage(name, u, sub); s != "" {
			u = s
		}
	}
	first := ""
	for _, l := range strings.Split(u, "\n") {
		t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "usage:"))
		if t == "" {
			continue
		}
		if first == "" {
			first = t
		}
		if strings.HasPrefix(t, "smithy "+name) {
			return "usage: " + t
		}
	}
	if !strings.HasPrefix(first, "smithy ") {
		return "usage: smithy " + name + " " + first
	}
	return "usage: " + first
}

// usageError prints what went wrong, the usage line and where to read more; exit 2.
func usageError(name, msg, sub string) int {
	c := commands[name]
	fmt.Fprintf(os.Stderr, "smithy %s: %s\n", name, msg)
	for _, l := range wrapLine(usageLine(name, c, sub)) {
		fmt.Fprintln(os.Stderr, l)
	}
	fmt.Fprintf(os.Stderr, "Run smithy help %s for usage and examples.\n", name)
	return 2
}

// unknownCommand says the name is not a command and suggests the closest one; exit 2.
func unknownCommand(w io.Writer, name string) int {
	fmt.Fprintf(w, "smithy: unknown command %q\n", name)
	var names []string
	for n, c := range commands {
		if summary(n, c) != "" {
			names = append(names, n)
		}
	}
	if s := suggest(name, names); s != "" {
		fmt.Fprintf(w, "Did you mean smithy %s?\n", s)
	}
	fmt.Fprintln(w, "Run smithy help to see the commands.")
	return 2
}

// suggest is the candidate closest to in by edit distance, or "" when none is close.
func suggest(in string, candidates []string) string {
	best, bestD := "", 3
	sort.Strings(candidates)
	for _, c := range candidates {
		if d := distance(in, c); d < bestD && d <= len([]rune(in))/2 {
			best, bestD = c, d
		}
	}
	return best
}

func distance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}

// helpMain is `smithy help [--all | <command> [sub]]`.
func helpMain(args []string) int {
	switch {
	case len(args) == 0:
		topHelp(os.Stdout, false)
		return 0
	case args[0] == "--all":
		topHelp(os.Stdout, true)
		return 0
	}
	if _, ok := commands[args[0]]; !ok {
		return unknownCommand(os.Stderr, args[0])
	}
	commandHelp(os.Stdout, args[0], args[1:])
	return 0
}
