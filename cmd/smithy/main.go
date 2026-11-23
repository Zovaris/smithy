// Command smithy is the one binary behind the smithy skills. Each command group registers
// itself from its own cmd_*.go file, so groups are added without touching this one.
package main

import (
	"fmt"
	"github.com/Zovaris/smithy/internal/warden"
	"io"
	"os"
	"path/filepath"
	"sort"
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
			fmt.Fprintf(os.Stderr, "smithy %s: unexpected argument %s\n", name, args[n])
			fmt.Fprintln(os.Stderr, commands[name].usage)
			return 2
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
					fmt.Fprintf(os.Stderr, "smithy version: unexpected argument %s\n", a)
					fmt.Fprintln(os.Stderr, commands["version"].usage)
					return 2
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
	register("engine-probe", "", "usage: smithy engine-probe\nPrints smithy-engine and the version.", "",
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
			fmt.Println(c.usage)
			return nil, 0, true
		}
	}
	if len(args) > 0 {
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
			fmt.Fprintf(os.Stderr, "smithy %s: unknown option %s\n", name, a)
			fmt.Fprintln(os.Stderr, c.usage)
			return nil, 2, true
		}
		if takesValue {
			if _, isFlag := c.flags[next(args, i)]; i+1 >= len(args) || isFlag || args[i+1] == "--" {
				fmt.Fprintf(os.Stderr, "smithy %s: %s needs a value\n", name, a)
				return nil, 2, true
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

func usage(w io.Writer) {
	fmt.Fprintf(w, "smithy %s: the binary behind the smithy skills\n", version)
	names := make([]string, 0, len(commands))
	for n, c := range commands {
		if c.help != "" {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(w, "  smithy %-12s %s\n", n, commands[n].help)
	}
	fmt.Fprintln(w, "Run any command with --help for its usage. Not on PATH? smithy setup path.")
}

func main() {
	warden.BinaryVersion = version
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "help", "--help", "-h":
		usage(os.Stdout)
		os.Exit(0)
	}
	name := os.Args[1]
	if name == "--version" || name == "-v" {
		name = "version"
	}
	c, ok := commands[name]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown command %s\n", name)
		if hint := warden.VersionHint(); hint != "" {
			fmt.Fprintln(os.Stderr, hint)
		}
		usage(os.Stderr)
		os.Exit(2)
	}
	args, code, done := entry(name, c, os.Args[2:])
	if done {
		os.Exit(code)
	}
	config.AutoMigrate()
	os.Exit(c.run(args))
}
