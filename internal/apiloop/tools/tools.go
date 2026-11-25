// Package tools is the core tool set of smithy's API loop (`smithy agent --backend api`): read,
// write, edit (and apply_patch for OpenAI models), shell, glob, grep and fetch. Every call is
// first checked against the same allow and deny rules a claude worker gets under --profile auto
// (agents.AutoTools, AutoDenied, SecretDenied); deny rules always win, and a refused call becomes
// a decision through the loop's sink. The rules are a best effort, not a boundary: without a
// sandbox the shell can read anything the user can.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Zovaris/smithy/internal/apiloop"
	"github.com/Zovaris/smithy/internal/proc"
	"github.com/Zovaris/smithy/internal/proxy"
	"github.com/Zovaris/smithy/internal/search"
)

const (
	// MaxLines and MaxBytes cap one read, a shell's inline output and a fetched body.
	MaxLines = 2000
	MaxBytes = 50 << 10
	// MaxResults caps glob and grep.
	MaxResults = 100
	// DefaultTimeout and MaxTimeout bound one shell command.
	DefaultTimeout = 2 * time.Minute
	MaxTimeout     = 10 * time.Minute
)

// Set is the tool set for one run. Allow and Deny are Claude Code style rules ("Read",
// "Bash(git status *)", "Edit(~/.ssh/**)"); Hosts is the egress allow-list fetch may reach;
// Model picks apply_patch for OpenAI models. Home expands "~" in rules (default: the user's).
type Set struct {
	Cwd, Home, Model string
	Allow, Deny      []string
	Hosts            []string
	Client           *http.Client
	// ReadOnly offers only read, glob and grep, and Permit refuses every other tool.
	ReadOnly bool
	// Wrap, when set, turns a shell argv into one run under the OS sandbox (auto.sandbox native).
	Wrap func(argv []string) []string
	// Proxy is the egress proxy fetch dials through; without one fetch refuses private addresses.
	Proxy *url.URL

	mu  sync.Mutex
	tmp string
}

// Close removes the run's overflow files.
func (s *Set) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tmp == "" {
		return nil
	}
	err := os.RemoveAll(s.tmp)
	s.tmp = ""
	return err
}

func (s *Set) tempDir() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tmp == "" {
		d, err := os.MkdirTemp("", "smithy-run-")
		if err != nil {
			return "", err
		}
		s.tmp = d
	}
	return s.tmp, nil
}

var readOnlyTools = map[string]bool{"read": true, "glob": true, "grep": true}

func schema(props string, required ...string) json.RawMessage {
	r, _ := json.Marshal(required)
	return json.RawMessage(`{"type":"object","properties":{` + props + `},"required":` + string(r) + `}`)
}

// Specs lists the tools for the model.
func (s *Set) Specs() []apiloop.ToolSpec {
	sp := []apiloop.ToolSpec{
		{Name: "read", Description: fmt.Sprintf("Read a text file with numbered lines; at most %d lines or %d KB per call, page with offset (1-based) and limit.", MaxLines, MaxBytes>>10),
			Schema: schema(`"path":{"type":"string"},"offset":{"type":"integer"},"limit":{"type":"integer"}`, "path")},
		{Name: "write", Description: "Write a whole file, creating folders as needed.",
			Schema: schema(`"path":{"type":"string"},"content":{"type":"string"}`, "path", "content")},
		{Name: "edit", Description: "Replace the exact text old with new in a file; old must match once unless replace_all.",
			Schema: schema(`"path":{"type":"string"},"old":{"type":"string"},"new":{"type":"string"},"replace_all":{"type":"boolean"}`, "path", "old", "new")},
		{Name: "shell", Description: fmt.Sprintf("Run a shell command in the working folder; timeout in seconds (default %d, at most %d). Long output is cut and saved to a file.", int(DefaultTimeout.Seconds()), int(MaxTimeout.Seconds())),
			Schema: schema(`"command":{"type":"string"},"timeout":{"type":"integer"}`, "command")},
		{Name: "glob", Description: fmt.Sprintf("List files matching a glob (** spans folders), at most %d.", MaxResults),
			Schema: schema(`"pattern":{"type":"string"},"path":{"type":"string"}`, "pattern")},
		{Name: "grep", Description: fmt.Sprintf("Search file contents for a regular expression, at most %d matches as path:line:text.", MaxResults),
			Schema: schema(`"pattern":{"type":"string"},"path":{"type":"string"},"ignore_case":{"type":"boolean"},"fixed":{"type":"boolean"}`, "pattern")},
		{Name: "fetch", Description: "GET an https URL on the egress allow-list and return its body (cut at 50 KB).",
			Schema: schema(`"url":{"type":"string"}`, "url")},
	}
	if openAIModel(s.Model) {
		sp = append(sp, apiloop.ToolSpec{Name: "apply_patch", Description: "Apply a patch in the *** Begin Patch / *** Update File / *** Add File / *** Delete File / *** End Patch format.",
			Schema: schema(`"patch":{"type":"string"}`, "patch")})
	}
	if s.ReadOnly {
		ro := sp[:0]
		for _, t := range sp {
			if readOnlyTools[t.Name] {
				ro = append(ro, t)
			}
		}
		sp = ro
	}
	return sp
}

func openAIModel(m string) bool {
	m = strings.ToLower(m[strings.LastIndex(m, "/")+1:])
	for _, p := range []string{"gpt-", "o1", "o3", "o4", "codex"} {
		if strings.HasPrefix(m, p) {
			return true
		}
	}
	return false
}

type args struct {
	Path, Content, Old, New, Command, Pattern, URL, Patch string
	Offset, Limit, Timeout                                int
	ReplaceAll                                            bool `json:"replace_all"`
	IgnoreCase                                            bool `json:"ignore_case"`
	Fixed                                                 bool
}

func parse(c apiloop.ToolCall) (args, error) {
	var a args
	if len(c.Args) > 0 {
		if err := json.Unmarshal(c.Args, &a); err != nil {
			return a, fmt.Errorf("bad arguments: %v", err)
		}
	}
	return a, nil
}

// Permit checks c against the deny rules, then the allow rules.
func (s *Set) Permit(c apiloop.ToolCall) (bool, string) {
	a, err := parse(c)
	if err != nil {
		return false, err.Error()
	}
	if s.ReadOnly && !readOnlyTools[c.Name] {
		return false, c.Name + " is not allowed in a read-only run"
	}
	switch c.Name {
	case "read":
		return s.permitPath("Read", a.Path)
	case "write", "edit":
		return s.permitPath("Edit", a.Path)
	case "apply_patch":
		ops, err := parsePatch(a.Patch)
		if err != nil {
			return false, err.Error()
		}
		for _, op := range ops {
			if ok, why := s.permitPath("Edit", op.path); !ok {
				return false, why
			}
		}
		return true, ""
	case "glob", "grep":
		kind := map[string]string{"glob": "Glob", "grep": "Grep"}[c.Name]
		if !s.bare(kind) {
			return false, kind + " is not in the auto allow-list"
		}
		return s.permitPath("Read", a.Path)
	case "shell":
		return s.permitShell(a.Command)
	case "fetch":
		u, err := url.Parse(a.URL)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return false, "fetch takes an https URL"
		}
		if !proxy.Allowed(s.Hosts, u.Hostname()) {
			return false, u.Hostname() + " is not on the egress allow-list (auto.sandbox_domains)"
		}
		return true, ""
	}
	return false, "unknown tool " + c.Name
}

func (s *Set) bare(kind string) bool {
	for _, r := range s.Allow {
		if r == kind {
			return true
		}
	}
	return false
}

func (s *Set) homes() []string {
	h := s.Home
	if h == "" {
		h, _ = os.UserHomeDir()
	}
	out := []string{h}
	if r, err := filepath.EvalSymlinks(h); err == nil && r != h {
		out = append(out, r)
	}
	return out
}

func (s *Set) abs(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		p = filepath.Join(s.homes()[0], p[1:])
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(s.Cwd, p)
	}
	return filepath.Clean(p)
}

// resolved follows symlinks in p, or in its nearest existing parent for a new file.
func resolved(p string) string {
	rest := ""
	for q := p; ; {
		if r, err := filepath.EvalSymlinks(q); err == nil {
			return filepath.Join(r, rest)
		}
		parent := filepath.Dir(q)
		if parent == q {
			return p
		}
		rest = filepath.Join(filepath.Base(q), rest)
		q = parent
	}
}

func (s *Set) permitPath(kind, p string) (bool, string) {
	if p == "" {
		p = "."
	}
	full := s.abs(p)
	if kind == "Edit" {
		if fi, err := os.Lstat(full); err == nil && fi.Mode()&fs.ModeSymlink != 0 {
			if _, err := os.Stat(full); err != nil {
				return false, "writing through a dangling symlink is not allowed"
			}
		}
	}
	r := resolved(full)
	for _, cand := range []string{full, r, full + "/x", r + "/x"} {
		for _, r := range s.Deny {
			if pat, ok := ruleArg(r, kind); ok && s.pathMatch(pat, cand) {
				return false, "denied by " + r
			}
		}
	}
	if !s.bare(kind) {
		return false, kind + " is not in the auto allow-list"
	}
	return true, ""
}

func ruleArg(rule, kind string) (string, bool) {
	if a, ok := strings.CutPrefix(rule, kind+"("); ok && strings.HasSuffix(a, ")") {
		return a[:len(a)-1], true
	}
	return "", false
}

func (s *Set) pathMatch(pat, p string) bool {
	p = filepath.ToSlash(p)
	for _, h := range s.homes() {
		q := pat
		if q == "~" || strings.HasPrefix(q, "~/") {
			q = filepath.ToSlash(h) + q[1:]
		}
		if !strings.HasPrefix(q, "/") && !strings.HasPrefix(q, "**") {
			q = "**/" + q
		}
		if globRe(q, true).MatchString(p) || foldCase && globRe("(?i)"+q, true).MatchString(p) {
			return true
		}
	}
	return false
}

// globRe compiles a glob: ** spans folders, * stays inside one (or spans anything when !paths).
func globRe(g string, paths bool) *regexp.Regexp {
	var b strings.Builder
	flags := "(?s)"
	if f, ok := strings.CutPrefix(g, "(?i)"); ok {
		g, flags = f, "(?si)"
	}
	b.WriteString("^")
	for i := 0; i < len(g); i++ {
		switch {
		case strings.HasPrefix(g[i:], "**/"):
			b.WriteString("(?:.*/)?")
			i += 2
		case strings.HasPrefix(g[i:], "**"):
			b.WriteString(".*")
			i++
		case g[i] == '*' && paths:
			b.WriteString("[^/]*")
		case g[i] == '*':
			b.WriteString(".*")
		case g[i] == '?' && paths:
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(g[i : i+1]))
		}
	}
	b.WriteString("$")
	return regexp.MustCompile(flags + b.String())
}

// foldCase: the default file systems on macOS and Windows ignore case, so deny rules must too.
var foldCase = runtime.GOOS == "darwin" || runtime.GOOS == "windows"

var shellSplit = regexp.MustCompile(`&&|\|\||[;|&\n\r]`)

// permitShell allows a command only when every part of a pipeline or list is allowed and no
// deny rule matches the whole or a part. Substitutions are refused outright: they would hide a
// command from the rules.
func (s *Set) permitShell(cmd string) (bool, string) {
	if strings.TrimSpace(cmd) == "" {
		return false, "empty command"
	}
	for _, bad := range []string{"$(", "`", "<(", ">("} {
		if strings.Contains(cmd, bad) {
			return false, "command substitution (" + bad + ") is not allowed"
		}
	}
	parts := []string{cmd}
	for _, p := range shellSplit.Split(cmd, -1) {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	for _, p := range parts {
		for _, r := range s.Deny {
			if pat, ok := ruleArg(r, "Bash"); ok && globRe(pat, false).MatchString(p) {
				return false, "denied by " + r
			}
		}
	}
	for _, p := range parts[1:] {
		if !s.shellAllowed(p) {
			return false, "Bash(" + p + ") is not in the auto allow-list"
		}
	}
	return true, ""
}

func (s *Set) shellAllowed(p string) bool {
	for _, r := range s.Allow {
		if r == "Bash" {
			return true
		}
		if pat, ok := ruleArg(r, "Bash"); ok && globRe(pat, false).MatchString(p) {
			return true
		}
	}
	return false
}

// Call runs c; Permit must have allowed it.
func (s *Set) Call(ctx context.Context, c apiloop.ToolCall) (string, error) {
	a, err := parse(c)
	if err != nil {
		return "", err
	}
	switch c.Name {
	case "read":
		return s.read(a)
	case "write":
		p := s.abs(a.Path)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(p, []byte(a.Content), 0o644); err != nil {
			return "", err
		}
		return fmt.Sprintf("wrote %d bytes to %s", len(a.Content), a.Path), nil
	case "edit":
		return s.edit(a)
	case "apply_patch":
		return s.applyPatch(a.Patch)
	case "shell":
		return s.shell(ctx, a)
	case "glob":
		return s.glob(a)
	case "grep":
		return s.grep(a)
	case "fetch":
		return s.fetch(ctx, a.URL)
	}
	return "", errors.New("unknown tool " + c.Name)
}

// maxReadFile caps how much of one file read takes in, so a huge file can't exhaust memory.
const maxReadFile = 64 << 20

func (s *Set) read(a args) (string, error) {
	p := s.abs(a.Path)
	fi, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	if !fi.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", a.Path)
	}
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxReadFile))
	if err != nil {
		return "", err
	}
	lines := strings.SplitAfter(string(b), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	from := max(a.Offset, 1)
	limit := a.Limit
	if limit <= 0 || limit > MaxLines {
		limit = MaxLines
	}
	if from > len(lines) && len(lines) > 0 {
		return "", fmt.Errorf("offset %d is past the end (%d lines)", from, len(lines))
	}
	var out strings.Builder
	last := from - 1
	for i := from - 1; i < len(lines) && i < from-1+limit; i++ {
		l := fmt.Sprintf("%6d\t%s", i+1, strings.TrimSuffix(lines[i], "\n")) + "\n"
		if out.Len()+len(l) > MaxBytes {
			if i == from-1 {
				out.WriteString(l[:MaxBytes])
				last = i + 1
			}
			break
		}
		out.WriteString(l)
		last = i + 1
	}
	if last < len(lines) {
		fmt.Fprintf(&out, "[showing lines %d-%d of %d; continue with offset %d]\n", from, last, len(lines), last+1)
	}
	return out.String(), nil
}

func (s *Set) edit(a args) (string, error) {
	p := s.abs(a.Path)
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	if a.Old == "" {
		return "", errors.New("old is empty")
	}
	n := strings.Count(string(b), a.Old)
	switch {
	case n == 0:
		return "", fmt.Errorf("old text not found in %s", a.Path)
	case n > 1 && !a.ReplaceAll:
		return "", fmt.Errorf("old text matches %d times in %s; add context or set replace_all", n, a.Path)
	}
	out := strings.Replace(string(b), a.Old, a.New, map[bool]int{true: -1, false: 1}[a.ReplaceAll])
	if err := writeKeepMode(p, out); err != nil {
		return "", err
	}
	return fmt.Sprintf("replaced %d occurrence(s) in %s", map[bool]int{true: n, false: 1}[a.ReplaceAll], a.Path), nil
}

func writeKeepMode(p, content string) error {
	mode := fs.FileMode(0o644)
	if fi, err := os.Stat(p); err == nil {
		mode = fi.Mode().Perm()
	}
	return os.WriteFile(p, []byte(content), mode)
}

func (s *Set) shell(ctx context.Context, a args) (string, error) {
	d := DefaultTimeout
	if a.Timeout > 0 {
		d = min(time.Duration(a.Timeout)*time.Second, MaxTimeout)
	}
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	argv := []string{"sh", "-c", a.Command}
	if runtime.GOOS == "windows" {
		argv = []string{"cmd", "/C", a.Command}
	}
	if s.Wrap != nil {
		argv = s.Wrap(argv)
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = s.Cwd
	cmd.Env = scrubEnv(os.Environ())
	cmd.WaitDelay = 2 * time.Second
	proc.Tree(cmd)
	out, err := cmd.CombinedOutput()
	text, oerr := s.overflow(out)
	if oerr != nil {
		return "", oerr
	}
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("%stimed out after %s", text, d)
	}
	if err != nil {
		return "", fmt.Errorf("%s[%v]", text, err)
	}
	return text, nil
}

var secretEnv = regexp.MustCompile(`(?i)(TOKEN|KEY|SECRET|PASSWORD|PASSWD|CREDENTIAL|AUTH|COOKIE|SESSION)`)

// scrubEnv drops variables whose names look like they hold a secret.
func scrubEnv(env []string) []string {
	out := env[:0:0]
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if !secretEnv.MatchString(name) {
			out = append(out, kv)
		}
	}
	return out
}

// overflow keeps output under MaxBytes inline, saving the whole of longer output to a file in
// the run's temp dir (removed by Close).
func (s *Set) overflow(out []byte) (string, error) {
	if len(out) <= MaxBytes {
		return string(out), nil
	}
	dir, err := s.tempDir()
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, "shell-*.log")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Write(out); err != nil {
		return "", err
	}
	half := MaxBytes / 2
	return fmt.Sprintf("%s\n…\n%s\n[output cut: %d bytes; full output in %s]\n", out[:half], out[len(out)-half:], len(out), f.Name()), nil
}

func (s *Set) glob(a args) (string, error) {
	root := s.abs(a.Path)
	re := globRe(filepath.ToSlash(a.Pattern), true)
	var out strings.Builder
	n := 0
	errStop := errors.New("stop")
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if !re.MatchString(rel) {
			return nil
		}
		if ok, _ := s.permitPath("Read", p); !ok {
			return nil
		}
		if n == MaxResults {
			return errStop
		}
		n++
		out.WriteString(rel + "\n")
		return nil
	})
	if errors.Is(err, errStop) {
		fmt.Fprintf(&out, "[truncated at %d results]\n", MaxResults)
	} else if err != nil {
		return "", err
	}
	return out.String(), nil
}

func (s *Set) grep(a args) (string, error) {
	exe, _ := os.Executable()
	res, err := search.Run(search.Options{Root: s.abs(a.Path), Pattern: a.Pattern, Fixed: a.Fixed, IgnoreCase: a.IgnoreCase,
		MaxMatches: MaxResults, MaxBytes: MaxBytes, Rg: search.FindRg(filepath.Dir(exe))})
	if err != nil {
		return "", err
	}
	var out strings.Builder
	root := s.abs(a.Path)
	for _, m := range res.Matches {
		if ok, _ := s.permitPath("Read", filepath.Join(root, filepath.FromSlash(m.Path))); !ok {
			continue
		}
		out.WriteString(m.String() + "\n")
	}
	if res.Truncated {
		fmt.Fprintf(&out, "[truncated at %d results]\n", len(res.Matches))
	}
	return out.String(), nil
}

// transport dials through s.Proxy when set; otherwise it checks every address it connects to,
// after DNS, so an allowed name can't resolve to a loopback, private or link-local address.
func (s *Set) transport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	if s.Proxy != nil {
		t.Proxy = http.ProxyURL(s.Proxy)
		return t
	}
	t.Proxy = nil
	d := &net.Dialer{Timeout: 30 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		if ip, err := netip.ParseAddr(host); err != nil || !publicIP(ip) {
			return fmt.Errorf("fetch refuses %s: not a public address", host)
		}
		return nil
	}}
	t.DialContext = d.DialContext
	return t
}

func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsValid() && !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() &&
		!ip.IsUnspecified() && !ip.IsMulticast() && !ip.IsInterfaceLocalMulticast() &&
		!netip.MustParsePrefix("100.64.0.0/10").Contains(ip)
}

func (s *Set) fetch(ctx context.Context, raw string) (string, error) {
	c := http.Client{Timeout: 30 * time.Second, Transport: s.transport()}
	if s.Client != nil {
		c = *s.Client
	}
	c.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if r.URL.Scheme != "https" || !proxy.Allowed(s.Hosts, r.URL.Hostname()) {
			return fmt.Errorf("redirect to %s is not on the egress allow-list", r.URL.Host)
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, MaxBytes+1))
	if err != nil {
		return "", err
	}
	out := string(b)
	if len(b) > MaxBytes {
		out = string(b[:MaxBytes]) + "\n[body cut at 50 KB]\n"
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d\n%s", resp.StatusCode, out)
	}
	return out, nil
}
