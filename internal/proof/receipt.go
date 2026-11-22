// Package proof records fresh evidence for claims (receipts) and checks a diff for secrets
// before it leaves the machine (scan).
package proof

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/Zovaris/smithy/internal/ghx"
)

const receiptUsage = `usage:
  smithy receipt run [--claim "<claim>"] -- <command> [args...]
      runs the command in the repo root, streams its output and records a receipt:
      claim, command, exit code, output digest, commit, dirty tree, duration, platform.
      Exits with the command's exit code.
  smithy receipt list [--md] [--all]
      receipts for the current commit (--all: every commit); --md prints a table for a PR body.
      A receipt taken on another commit is marked stale (unless only task, spec or changelog
      files changed since); with uncommitted changes, dirty.
  smithy receipt clear
Receipts live in <repo>/.smelt/receipts.jsonl; .smelt/ ignores itself (its own .gitignore).`

// Receipt is one recorded run of a command, the evidence behind a claim.
type Receipt struct {
	Claim    string   `json:"claim"`
	Command  []string `json:"command"`
	Exit     int      `json:"exit"`
	Digest   string   `json:"digest"`
	Commit   string   `json:"commit"`
	Dirty    bool     `json:"dirty"`
	At       string   `json:"at"`
	Millis   int64    `json:"ms"`
	Platform string   `json:"platform"`
	// sameCode: taken on an earlier commit, but only Markdown changed since.
	sameCode bool
}

func receiptsPath(root string) string { return filepath.Join(root, ".smelt", "receipts.jsonl") }

func head(root string) (string, bool) {
	sha, _ := ghx.Run("git", "-C", root, "rev-parse", "HEAD")
	st, _ := ghx.Run("git", "-C", root, "status", "--porcelain", "--", ".", ":(exclude,glob)docs/specs/**/*.md", ":(exclude,glob).docs/**/*.md", ":(exclude)CHANGELOG.md", ":(exclude).smelt", ":(exclude).smithy/config.json", ":(exclude,glob)**/zz_*_test.*", ":(exclude,glob)**/zz_*_test", ":(exclude,glob)**/zz_*.test.*", ":(exclude,glob)**/zz_*.spec.*")
	return sha, st != ""
}

// Run executes argv in root, tees its output to out and returns the receipt.
func Run(root, claim string, argv []string, out io.Writer) (Receipt, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	if len(argv) == 1 && strings.ContainsAny(argv[0], " \t") {
		// One quoted command line (zsh doesn't word-split "$cmd"): let the shell split it.
		if runtime.GOOS == "windows" {
			cmd = exec.Command("cmd", "/c", argv[0])
		} else {
			cmd = exec.Command("sh", "-c", argv[0])
		}
	}
	cmd.Dir = root
	h := sha256.New()
	w := io.MultiWriter(out, h)
	cmd.Stdout, cmd.Stderr = w, w
	ownGroup(cmd)
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigs)
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return Receipt{}, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var got os.Signal
	var err error
wait:
	for {
		select {
		case s := <-sigs:
			got = s
			forward(cmd, s)
		case err = <-done:
			break wait
		}
	}
	if got != nil {
		code := 130
		if n, ok := got.(syscall.Signal); ok {
			code = 128 + int(n)
		}
		return Receipt{}, &Interrupted{Code: code}
	}
	exit := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			return Receipt{}, err
		}
		exit = exitCode(ee)
	}
	sha, dirty := head(root)
	if claim == "" {
		claim = strings.Join(argv, " ")
	}
	return Receipt{
		Claim: claim, Command: argv, Exit: exit,
		Digest: hex.EncodeToString(h.Sum(nil))[:16], Commit: sha, Dirty: dirty,
		At: start.UTC().Format(time.RFC3339), Millis: time.Since(start).Milliseconds(),
		Platform: runtime.GOOS + "/" + runtime.GOARCH,
	}, nil
}

// Interrupted is Run's error when smithy itself got SIGINT/SIGTERM: the signal went to the
// command, it was waited for, and no receipt was made. Code is 128+signal.
type Interrupted struct{ Code int }

func (e *Interrupted) Error() string { return fmt.Sprintf("interrupted (exit %d)", e.Code) }

// exitCode is the command's exit code, or 128+signal for a command killed by a signal.
func exitCode(ee *exec.ExitError) int {
	if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return ee.ExitCode()
}

// Append adds r to the repo's receipt log.
func Append(root string, r Receipt) error {
	p := receiptsPath(root)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	ign := filepath.Join(filepath.Dir(p), ".gitignore")
	if _, err := os.Stat(ign); os.IsNotExist(err) {
		if err := os.WriteFile(ign, []byte("*\n"), 0o644); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, _ := json.Marshal(r)
	if st, err := f.Stat(); err == nil && st.Size() > 0 {
		last := make([]byte, 1)
		if _, err := f.ReadAt(last, st.Size()-1); err == nil && last[0] != '\n' {
			b = append([]byte{'\n'}, b...)
		}
	}
	_, err = f.Write(append(b, '\n'))
	return err
}

// Load reads every receipt in the repo's log; a missing log is empty.
func Load(root string) ([]Receipt, error) {
	b, err := os.ReadFile(receiptsPath(root))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rs []Receipt
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var r Receipt
		if json.Unmarshal(sc.Bytes(), &r) == nil {
			rs = append(rs, r)
		}
	}
	return rs, nil
}

// Markdown renders receipts as a PR-body table, newest claim last.
func Markdown(rs []Receipt, head string, dirtyNow bool) string {
	var b strings.Builder
	b.WriteString("| Claim | Command | Exit | Commit | Output sha256 |\n|---|---|---|---|---|\n")
	for _, r := range rs {
		c := short(r.Commit)
		if s := r.State(head, dirtyNow); s != "fresh" {
			c += " (" + s + ")"
		}
		fmt.Fprintf(&b, "| %s | `%s` | %d | %s | %s |\n",
			cell(r.Claim), cell(Redact(Shown(r.Command))), r.Exit, c, r.Digest)
	}
	return b.String()
}

// State is "fresh", "dirty" (taken on head with uncommitted changes, or the tree changed since)
// or "stale" (another commit). dirtyNow is whether the working tree has uncommitted changes.
func (r Receipt) State(head string, dirtyNow bool) string {
	switch {
	case r.Commit != head && !r.sameCode:
		return "stale"
	case r.Dirty || dirtyNow:
		return "dirty"
	}
	return "fresh"
}

// Shown is a receipt's command as typed: one command line that came in as a single string
// prints bare, since the shell split it; anything else is quoted like Quote.
func Shown(argv []string) string {
	if len(argv) == 1 && strings.ContainsAny(argv[0], " \t") {
		return argv[0]
	}
	return Quote(argv)
}

// Quote joins argv the way a shell would read it back.
func Quote(argv []string) string {
	out := make([]string, len(argv))
	for i, a := range argv {
		if a == "" || strings.ContainsAny(a, " \t\n'\"\\$`*?;&|<>()#~![]{}") {
			a = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
		out[i] = a
	}
	return strings.Join(out, " ")
}

// Redact masks anything in s that the scan rules take for a secret.
func Redact(s string) string {
	for _, r := range rules {
		s = r.re.ReplaceAllStringFunc(s, mask)
	}
	return secretArg.ReplaceAllStringFunc(s, func(m string) string {
		g := secretArg.FindStringSubmatch(m)
		return g[1] + mask(g[2])
	})
}

var secretArg = regexp.MustCompile(`(?i)((?:--?(?:token|password|passwd|secret|api[-_]?key|auth)[= ]+|bearer\s+))([^\s'"]+)`)

// planDoc is a task or spec file (docs/specs, .docs) or the changelog: marking a task done there
// can't change what a command proved.
func planDoc(f string) bool {
	return f == "CHANGELOG.md" || strings.HasSuffix(f, ".md") && (strings.HasPrefix(f, "docs/specs/") || strings.HasPrefix(f, ".docs/"))
}

// docsOnlySince reports whether every file changed from commit a to b is a planDoc.
func docsOnlySince(root, a, b string) bool {
	if a == "" || a == b {
		return a == b
	}
	out, err := ghx.Run("git", "-C", root, "diff", "--name-only", a, b)
	if err != nil {
		return false
	}
	for _, f := range strings.Fields(out) {
		if !planDoc(f) {
			return false
		}
	}
	return true
}

func short(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

func cell(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "|", `\|`), "\n", " ") }

// ReceiptMain runs `smithy receipt`.
func ReceiptMain(args []string) int {
	cwd, _ := os.Getwd()
	root := ghx.RepoRoot(cwd)
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, receiptUsage)
		return 2
	}
	if root == "" {
		fmt.Fprintln(os.Stderr, "receipt: not inside a git checkout")
		return 2
	}
	switch args[0] {
	case "run":
		claim, rest := "", args[1:]
		for len(rest) > 0 && rest[0] != "--" {
			if rest[0] == "--claim" && len(rest) > 1 {
				claim, rest = rest[1], rest[2:]
				continue
			}
			fmt.Fprintln(os.Stderr, receiptUsage)
			return 2
		}
		if len(rest) < 2 {
			fmt.Fprintln(os.Stderr, receiptUsage)
			return 2
		}
		r, err := Run(root, claim, rest[1:], os.Stdout)
		var in *Interrupted
		if errors.As(err, &in) {
			fmt.Fprintln(os.Stderr, "receipt: interrupted, nothing recorded")
			return in.Code
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "receipt:", err)
			return 127
		}
		if err := Append(root, r); err != nil {
			fmt.Fprintln(os.Stderr, "receipt: not recorded:", err)
			if r.Exit == 0 {
				return 1
			}
			return r.Exit
		}
		state := "fresh"
		if r.Dirty {
			state = "dirty tree: commit, then run it again for a fresh receipt"
		}
		fmt.Fprintf(os.Stderr, "receipt: %q exit %d at %s (%s)\n", r.Claim, r.Exit, short(r.Commit), state)
		return r.Exit
	case "list":
		md, all := false, false
		for _, a := range args[1:] {
			switch a {
			case "--md":
				md = true
			case "--all":
				all = true
			default:
				fmt.Fprintln(os.Stderr, receiptUsage)
				return 2
			}
		}
		rs, err := Load(root)
		if err != nil {
			fmt.Fprintln(os.Stderr, "receipt:", err)
			return 1
		}
		sha, dirtyNow := head(root)
		if !all {
			kept := rs[:0]
			for _, r := range rs {
				if r.Commit == sha || docsOnlySince(root, r.Commit, sha) {
					r.sameCode = r.Commit != sha
					kept = append(kept, r)
				}
			}
			if hidden := len(rs) - len(kept); hidden > 0 {
				fmt.Fprintf(os.Stderr, "receipt: %d receipt(s) from other commits hidden; --all shows them\n", hidden)
			}
			rs = kept
		}
		if md {
			fmt.Print(Markdown(rs, sha, dirtyNow))
			return 0
		}
		for _, r := range rs {
			fmt.Printf("%s\texit %d\t%s\t%s\t%s\n", short(r.Commit), r.Exit, r.State(sha, dirtyNow), r.Claim, Redact(Shown(r.Command)))
		}
		return 0
	case "clear":
		if err := os.Remove(receiptsPath(root)); err != nil && !os.IsNotExist(err) {
			fmt.Fprintln(os.Stderr, "receipt:", err)
			return 1
		}
		return 0
	}
	fmt.Fprintln(os.Stderr, receiptUsage)
	return 2
}
