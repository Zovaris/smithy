// Package search finds lines matching a pattern under a folder for `smithy search` (and the
// MCP search tool): ripgrep when it is installed next to the binary or on PATH, else
// `git ls-files` in a repo, else a stdlib walk. Every engine returns the same capped result.
package search

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/Zovaris/smithy/internal/text"
)

const (
	EngineRg   = "rg"
	EngineGit  = "git"
	EngineWalk = "walk"

	DefaultMaxMatches = 200
	DefaultMaxLine    = 300
	DefaultMaxBytes   = 64 << 10
	maxFileSize       = 8 << 20
)

var exeSuffix = map[bool]string{true: ".exe"}[runtime.GOOS == "windows"]

// Options says what to search. Zero caps take the defaults; Engine "" picks the fastest one
// available; Rg is the ripgrep binary to use ("" means none).
type Options struct {
	Root, Pattern                 string
	Fixed, IgnoreCase             bool
	MaxMatches, MaxLine, MaxBytes int
	Engine, Rg                    string
}

// Match is one matching line; Path is slash-separated and relative to Root.
type Match struct {
	Path string
	Line int
	Text string
}

func (m Match) String() string { return m.Path + ":" + strconv.Itoa(m.Line) + ":" + m.Text }

// Result holds the matches in path and line order; Truncated is set when a cap cut it short.
type Result struct {
	Engine    string
	Matches   []Match
	Truncated bool
}

// Bytes is the size of the result printed one match per line.
func (r Result) Bytes() int {
	n := 0
	for _, m := range r.Matches {
		n += len(m.String()) + 1
	}
	return n
}

// FindRg returns the ripgrep to use: $SMITHY_RG ("off" disables it), the rg shipped next to
// the smithy binary in binDir, or rg on PATH; "" when there is none.
func FindRg(binDir string) string {
	switch env := os.Getenv("SMITHY_RG"); env {
	case "off":
		return ""
	case "":
	default:
		return env
	}
	if binDir != "" {
		p := filepath.Join(binDir, "rg"+exeSuffix)
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			return p
		}
	}
	p, _ := exec.LookPath("rg")
	return p
}

type collector struct {
	opts Options
	res  Result
	size int
}

func (c *collector) add(m Match) bool {
	m.Text = capLine(text.StripEscapes(strings.TrimRight(m.Text, "\r"), true), c.opts.MaxLine)
	m.Path = text.StripEscapes(m.Path, false)
	n := len(m.String()) + 1
	if len(c.res.Matches) >= c.opts.MaxMatches || c.size+n > c.opts.MaxBytes {
		c.res.Truncated = true
		return false
	}
	c.size += n
	c.res.Matches = append(c.res.Matches, m)
	return true
}

func capLine(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// Run searches opts.Root for opts.Pattern.
func Run(opts Options) (Result, error) {
	if opts.Pattern == "" {
		return Result{}, errors.New("search: empty pattern")
	}
	if opts.Root == "" {
		opts.Root = "."
	}
	if fi, err := os.Stat(opts.Root); err != nil || !fi.IsDir() {
		return Result{}, fmt.Errorf("search: %s is not a folder", opts.Root)
	}
	if opts.MaxMatches <= 0 {
		opts.MaxMatches = DefaultMaxMatches
	}
	if opts.MaxLine <= 0 {
		opts.MaxLine = DefaultMaxLine
	}
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = DefaultMaxBytes
	}
	pat := opts.Pattern
	if opts.Fixed {
		pat = regexp.QuoteMeta(pat)
	}
	if opts.IgnoreCase {
		pat = "(?i)" + pat
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return Result{}, fmt.Errorf("search: bad pattern: %v", err)
	}
	engine := opts.Engine
	if engine == "" {
		switch {
		case opts.Rg != "":
			engine = EngineRg
		case inRepo(opts.Root):
			engine = EngineGit
		default:
			engine = EngineWalk
		}
	}
	c := &collector{opts: opts, res: Result{Engine: engine}}
	switch engine {
	case EngineRg:
		if opts.Rg == "" {
			return Result{}, errors.New("search: ripgrep not found")
		}
		err = runRg(c)
	case EngineGit:
		var files []string
		if files, err = gitFiles(opts.Root); err == nil {
			scanFiles(c, re, files)
		}
	case EngineWalk:
		scanFiles(c, re, walkFiles(opts.Root))
	default:
		return Result{}, fmt.Errorf("search: unknown engine %q (rg, git or walk)", engine)
	}
	if err != nil {
		return Result{}, err
	}
	sort.SliceStable(c.res.Matches, func(i, j int) bool {
		a, b := c.res.Matches[i], c.res.Matches[j]
		return a.Path < b.Path || a.Path == b.Path && a.Line < b.Line
	})
	return c.res, nil
}

func inRepo(root string) bool {
	out, err := exec.Command("git", "-C", root, "rev-parse", "--is-inside-work-tree").Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

func gitFiles(root string) ([]string, error) {
	out, err := exec.Command("git", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard").Output()
	if err != nil {
		return nil, fmt.Errorf("search: git ls-files: %v", err)
	}
	var files []string
	for _, f := range strings.Split(string(out), "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	sort.Strings(files)
	return files, nil
}

func walkFiles(root string) []string {
	var files []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() {
			if rel, err := filepath.Rel(root, p); err == nil {
				files = append(files, filepath.ToSlash(rel))
			}
		}
		return nil
	})
	return files
}

func scanFiles(c *collector, re *regexp.Regexp, files []string) {
	for _, rel := range files {
		p := filepath.Join(c.opts.Root, filepath.FromSlash(rel))
		// a tracked symlink can point outside the root: only regular files are read
		if li, err := os.Lstat(p); err != nil || !li.Mode().IsRegular() {
			continue
		}
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		fi, err := f.Stat()
		if err != nil || !fi.Mode().IsRegular() || fi.Size() > maxFileSize {
			f.Close()
			continue
		}
		br := bufio.NewReader(f)
		if head, _ := br.Peek(8000); bytes.IndexByte(head, 0) >= 0 {
			f.Close()
			continue
		}
		sc := bufio.NewScanner(br)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for n := 1; sc.Scan(); n++ {
			if re.Match(sc.Bytes()) && !c.add(Match{Path: rel, Line: n, Text: sc.Text()}) {
				f.Close()
				return
			}
		}
		f.Close()
	}
}

func runRg(c *collector) error {
	args := []string{"--no-config", "--no-heading", "--with-filename", "--line-number", "--null",
		"--color=never", "--no-messages", "--max-filesize=8M"}
	if c.opts.Fixed {
		args = append(args, "--fixed-strings")
	}
	if c.opts.IgnoreCase {
		args = append(args, "--ignore-case")
	}
	args = append(args, "--regexp", c.opts.Pattern, "--", ".")
	cmd := exec.Command(c.opts.Rg, args...)
	cmd.Dir = c.opts.Root
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("search: run rg: %v", err)
	}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	stopped := false
	for sc.Scan() {
		path, rest, ok := strings.Cut(sc.Text(), "\x00")
		num, line, ok2 := strings.Cut(rest, ":")
		n, err := strconv.Atoi(num)
		if !ok || !ok2 || err != nil {
			continue
		}
		path = filepath.ToSlash(strings.TrimPrefix(path, "."+string(filepath.Separator)))
		if !c.add(Match{Path: strings.TrimPrefix(path, "./"), Line: n, Text: line}) {
			stopped = true
			_ = cmd.Process.Kill()
			break
		}
	}
	err = cmd.Wait()
	if stopped {
		return nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return nil
	}
	if err != nil {
		return fmt.Errorf("search: rg: %v %s", err, text.Clean(stderr.String()))
	}
	return nil
}
