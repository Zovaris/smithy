// Command smithy is the one binary behind the smithy skills. Each command group registers
// itself from its own cmd_*.go file, so groups are added without touching this one.
package main

import (
	"fmt"
	"github.com/Zovaris/smithy/internal/warden"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Zovaris/smithy/internal/config"
)

// version is set at build time with -ldflags "-X main.version=<v>".
var version = "dev"

type command struct {
	help  string
	usage string
	flags map[string]bool
	run   func(args []string) int
	// subs, when set, holds the flags of each subcommand (the first arg) instead of flags.
	subs map[string]map[string]bool
}

var commands = map[string]command{}

// register adds a top-level command. help is one line for `smithy --help`; usage is what
// `smithy <name> --help` prints; flags lists the options the command accepts, a trailing
// "=" marking one that takes a value. A bare "--" in flags means the command reads `--` itself;
// for every other command entry drops the first `--` so only clean positionals reach it.
func register(name, help, usage, flags string, run func(args []string) int) {
	commands[name] = command{help: help, usage: usage, flags: parseFlags(flags), run: run}
}

// registerSub sets the flags one subcommand of a registered command accepts.
func registerSub(name, sub, flags string) {
	c := commands[name]
	if c.subs == nil {
		c.subs = map[string]map[string]bool{}
	}
	c.subs[sub] = parseFlags(flags)
	commands[name] = c
}

func parseFlags(flags string) map[string]bool {
	f := map[string]bool{}
	for _, x := range strings.Fields(flags) {
		f[strings.TrimSuffix(x, "=")] = strings.HasSuffix(x, "=") || x == "--"
	}
	return f
}

// atMost wraps run so more than n positionals exit 2 instead of being ignored.
func atMost(name string, n int, run func(args []string) int) func(args []string) int {
	return func(args []string) int {
		if len(args) > n {
			return usageError(name, "unexpected argument "+args[n], "")
		}
		return run(args)
	}
}

func init() {
	register("version", "print the version", "usage: smithy version [--verbose]\nPrints the version (also smithy --version).\n--verbose adds the binary path and whether it is a dev build.", "--verbose",
		func(args []string) int {
			verbose := false
			for _, a := range args {
				if a != "--verbose" {
					return usageError("version", "unexpected argument "+a, "")
				}
				verbose = true
			}
			fmt.Println(version)
			if !verbose {
				return 0
			}
			fmt.Println("binary:", binaryPath())
			if version == "dev" {
				fmt.Println("build: dev (not a release build; version set at build time is missing)")
			} else {
				fmt.Println("build: release")
			}
			return 0
		})
	register("engine-probe", "print smithy-engine and the version", "usage: smithy engine-probe\nPrints smithy-engine and the version.", "",
		atMost("engine-probe", 0, func([]string) int { fmt.Println("smithy-engine", version); return 0 }))
}

// binaryPath is the resolved path of the running binary, or argv[0] when the OS cannot say.
func binaryPath() string {
	p, err := os.Executable()
	if err != nil {
		return os.Args[0]
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// entry applies the convention every command shares, before anything runs: -h/--help anywhere
// (up to a bare --) prints the usage on stdout, exit 0; an unknown option exits 2. It returns
// the args the command runs with.
func entry(name string, c command, args []string) ([]string, int, bool) {
	for _, a := range args {
		if a == "--" {
			break
		}
		if a == "-h" || a == "--help" {
			commandHelp(os.Stdout, name, args[:1])
			return nil, 0, true
		}
	}
	sub := ""
	if len(args) > 0 {
		sub = args[0]
		if f, ok := c.subs[args[0]]; ok {
			c.flags = f
		}
	}
	var out []string
	at, drop := map[string]int{}, map[int]bool{}
	pair := func(k, v string) {
		if p, ok := at[k]; ok {
			drop[p], drop[p+1] = true, true
		}
		at[k] = len(out)
		out = append(out, k, v)
	}
	rest := []string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			if c.flags["--"] {
				rest = append(rest, a)
			}
			rest = append(rest, args[i+1:]...)
			break
		}
		if len(a) < 2 || a[0] != '-' || isNumber(a) {
			out = append(out, a)
			continue
		}
		if k, v, eq := strings.Cut(a, "="); eq && k != "--" && c.flags[k] {
			pair(k, v)
			continue
		}
		takesValue, ok := c.flags[a]
		if !ok {
			msg := "unknown option " + a
			var known []string
			for f := range c.flags {
				if f != "--" {
					known = append(known, f)
				}
			}
			if s := suggest(a, known); s != "" {
				msg += " (did you mean " + s + "?)"
			}
			return nil, usageError(name, msg, sub), true
		}
		if takesValue {
			if _, isFlag := c.flags[next(args, i)]; i+1 >= len(args) || isFlag || args[i+1] == "--" {
				return nil, usageError(name, a+" needs a value", sub), true
			}
			i++
			pair(a, args[i])
			continue
		}
		out = append(out, a)
	}
	// A repeated value flag: the last one wins, the earlier pairs are dropped.
	var kept []string
	for i, a := range out {
		if !drop[i] {
			kept = append(kept, a)
		}
	}
	return append(kept, rest...), 0, false
}

// subUsage is the part of usage about subcommand sub: each line that starts it (an indented
// "  sub …" line, or a "smithy <name> … | sub …" line) plus its more indented continuation
// lines; "" when usage has none.
func subUsage(name, usage, sub string) string {
	indent := func(l string) int { return len(l) - len(strings.TrimLeft(l, " ")) }
	starts := func(l string) bool {
		t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "usage:"))
		parts := []string{t}
		if r, ok := strings.CutPrefix(t, "smithy "+name+" "); ok {
			parts = strings.Split(r, "|")
		}
		for _, p := range parts {
			if f := strings.Fields(p); len(f) > 0 && f[0] == sub {
				return true
			}
		}
		return false
	}
	lines := strings.Split(usage, "\n")
	var out []string
	for i := 0; i < len(lines); i++ {
		if !starts(lines[i]) {
			continue
		}
		out = append(out, lines[i])
		in := indent(lines[i])
		for i+1 < len(lines) && strings.TrimSpace(lines[i+1]) != "" && indent(lines[i+1]) > in {
			i++
			out = append(out, lines[i])
		}
	}
	return strings.Join(out, "\n")
}

// next is the arg after i, or "" at the end.
func next(args []string, i int) string {
	if i+1 < len(args) {
		return args[i+1]
	}
	return ""
}

func isNumber(s string) bool {
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

func usage(w io.Writer) { topHelp(w, false) }

// noArgs runs for bare `smithy`; the harness build opens the chat, the skills build prints usage.
var noArgs func() int

// bare runs `smithy` with no arguments: the chat when the build has one and both ends are a
// terminal, else usage with exit 2 (a script never gets a fullscreen UI).
func bare(open func() int, tty bool) int {
	if open != nil && tty {
		return open()
	}
	usage(os.Stderr)
	return 2
}

func terminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func main() {
	warden.BinaryVersion = version
	if len(os.Args) < 2 {
		os.Exit(bare(noArgs, terminal(os.Stdin) && terminal(os.Stdout)))
	}
	if os.Args[1] == "--standalone" {
		os.Args = append([]string{os.Args[0], "chat"}, os.Args[1:]...)
	}
	switch os.Args[1] {
	case "help", "--help", "-h":
		os.Exit(helpMain(os.Args[2:]))
	}
	name := os.Args[1]
	if name == "--version" || name == "-v" {
		name = "version"
	}
	if name == "tui" {
		fmt.Fprintln(os.Stderr, "smithy tui is renamed to smithy stats")
		name = "stats"
	}
	c, ok := commands[name]
	if !ok {
		code := unknownCommand(os.Stderr, name)
		if hint := warden.VersionHint(); hint != "" {
			fmt.Fprintln(os.Stderr, hint)
		}
		os.Exit(code)
	}
	args, code, done := entry(name, c, os.Args[2:])
	if done {
		os.Exit(code)
	}
	config.AutoMigrate()
	os.Exit(c.run(args))
}
