// Package cleanup runs `smithy cleanup builds`: old smithy builds,
// stale worktrees, listed by default and deleted only with --yes.
package cleanup

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/ghx"
	"github.com/Zovaris/smithy/internal/layout"
)

const Usage = "usage: smithy cleanup builds [--yes]\n  lists old smithy builds in ~/.local/share/smithy and the old ~/.smithy/bin (keeps the current one and one previous; dev builds\n  after 14 days) and stale worktrees of\n  the repo it runs in (clean including ignored files, not locked, with no commits beyond the base branch, not the folder it runs in), with the\n  space each takes; --yes deletes them. Worktrees with uncommitted changes are never touched."

// Item is one thing cleanup can remove: a build folder or a worktree path.
type Item struct {
	Kind, Target, Size string
	Bytes              int64
}

func semver(s string) ([3]int, bool) {
	var v [3]int
	core, _, _ := strings.Cut(strings.TrimPrefix(s, "v"), "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

func duSize(p string) int64 {
	var n int64
	_ = filepath.WalkDir(p, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if i, e := d.Info(); e == nil {
				n += i.Size()
			}
		}
		return nil
	})
	return n
}

// devMaxAge is how long a per-checkout dev build (bin/dev/<key>) stays before cleanup lists it.
const devMaxAge = 14 * 24 * time.Hour

// Builds lists old builds in a pre-0.21 ~/.smithy/bin: BuildsIn(bin, bin/dev, bin/smithy).
func Builds(bin string) []Item {
	return BuildsIn(bin, filepath.Join(bin, "dev"), filepath.Join(bin, "smithy"))
}

// BuildsIn lists the version folders in bin (install.sh's <versions>/<version>/smithy) other than
// the one link points at and the newest other release, plus dev builds (<dev>/<key>) untouched for
// devMaxAge. Other folders, links and the folder of the running binary are never listed.
func BuildsIn(bin, dev, link string) []Item {
	ents, err := os.ReadDir(bin)
	if err != nil {
		ents = nil
	}
	current := ""
	if t, err := filepath.EvalSymlinks(link); err == nil {
		if rb, err := filepath.EvalSymlinks(bin); err == nil {
			if rel, err := filepath.Rel(rb, t); err == nil && !strings.HasPrefix(rel, "..") {
				current = strings.Split(filepath.ToSlash(rel), "/")[0]
			}
		}
	}
	var rel []string
	for _, e := range ents {
		if _, ok := semver(e.Name()); ok && e.IsDir() && e.Name() != current {
			rel = append(rel, e.Name())
		}
	}
	slices.SortFunc(rel, func(a, b string) int {
		va, _ := semver(a)
		vb, _ := semver(b)
		return slices.Compare(va[:], vb[:])
	})
	keep := 1
	if current == "" {
		keep = 2
	}
	var dirs []string
	if len(rel) > keep {
		for _, n := range rel[:len(rel)-keep] {
			dirs = append(dirs, filepath.Join(bin, n))
		}
	}
	if devs, err := os.ReadDir(dev); err == nil {
		for _, d := range devs {
			if p := filepath.Join(dev, d.Name()); d.IsDir() && time.Since(newest(p)) > devMaxAge {
				dirs = append(dirs, p)
			}
		}
	}
	exe, _ := os.Executable()
	var out []Item
	for _, p := range dirs {
		if exe != "" && inside(exe, p) {
			continue
		}
		b := duSize(p)
		out = append(out, Item{Kind: "build", Target: p, Bytes: b, Size: human(b)})
	}
	return out
}

func gitIn(dir string, args ...string) (string, error) {
	return ghx.Run("git", append([]string{"-C", dir}, args...)...)
}

func inside(child, parent string) bool {
	r, err := filepath.Rel(real(parent), real(child))
	return err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
}

func real(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	a, _ := filepath.Abs(p)
	return a
}

// Worktrees lists repo's linked worktrees that are clean (untracked and ignored files count), not locked, not
// the folder cwd is in, and whose HEAD has no commits beyond base.
func Worktrees(repo, base, cwd string) []Item {
	list, err := gitIn(repo, "worktree", "list", "--porcelain")
	if err != nil {
		return nil
	}
	var out []Item
	for i, block := range strings.Split(list, "\n\n") {
		path, locked := "", false
		for _, l := range strings.Split(block, "\n") {
			if p, ok := strings.CutPrefix(l, "worktree "); ok {
				path = p
			} else if l == "locked" || strings.HasPrefix(l, "locked ") {
				locked = true
			}
		}
		if i == 0 || path == "" || locked || inside(cwd, path) {
			continue
		}
		if fi, err := os.Stat(path); err != nil || !fi.IsDir() {
			continue
		}
		if st, err := gitIn(path, "status", "--porcelain", "--ignored"); err != nil || st != "" {
			continue
		}
		if ahead, err := gitIn(path, "rev-list", "--count", "HEAD", "--not", base); err != nil || ahead != "0" {
			if cherry, _ := gitIn(path, "cherry", base, "HEAD"); cherry == "" || strings.Contains("\n"+cherry, "\n+") {
				continue
			}
		}
		b := duSize(path)
		out = append(out, Item{Kind: "worktree", Target: path, Bytes: b, Size: human(b)})
	}
	return out
}

func human(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1fGB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(b)/(1<<10))
	}
	return fmt.Sprintf("%dB", b)
}

// Main runs `smithy cleanup builds [--yes]`.
func Main(args []string) int {
	yes := false
	for _, a := range args {
		if a != "--yes" {
			fmt.Fprintln(os.Stderr, Usage)
			return 2
		}
		yes = true
	}
	items := append(BuildsIn(layout.VersionsDir(), layout.DevDir(), layout.Link()), Builds(layout.LegacyBin())...)
	cwd, _ := os.Getwd()
	if common, err := gitIn(cwd, "rev-parse", "--path-format=absolute", "--git-common-dir"); err == nil && filepath.Base(common) == ".git" {
		repo := filepath.Dir(common)
		base, _ := gitIn(repo, "symbolic-ref", "-q", "--short", "refs/remotes/origin/HEAD")
		if base == "" {
			base = "origin/main"
			if _, err := gitIn(repo, "rev-parse", "-q", "--verify", base); err != nil {
				base = "main"
			}
		}
		items = append(items, Worktrees(repo, base, cwd)...)
		if yes {
			defer gitIn(repo, "worktree", "prune")
		}
	}
	if len(items) == 0 {
		fmt.Println("nothing to clean")
		return 0
	}
	var total int64
	failed := 0
	for _, it := range items {
		total += it.Bytes
		if !yes {
			fmt.Printf("would remove %s %s (%s)\n", it.Kind, it.Target, it.Size)
			continue
		}
		var err error
		switch it.Kind {
		case "build":
			err = os.RemoveAll(it.Target)
		case "worktree":
			_, err = ghx.Run("git", "-C", it.Target, "worktree", "remove", it.Target)
		}
		if err != nil {
			failed++
			fmt.Printf("kept %s %s: %v\n", it.Kind, it.Target, err)
			continue
		}
		fmt.Printf("removed %s %s (%s)\n", it.Kind, it.Target, it.Size)
	}
	verb := "would free"
	if yes {
		verb = "freed"
	}
	fmt.Printf("%s %s on disk; %d item(s)\n", verb, human(total), len(items))
	if !yes {
		fmt.Println("run smithy cleanup builds --yes to delete them")
	}
	if failed > 0 {
		return 1
	}
	return 0
}

// newest is the latest modification time of dir and the files directly in it, so a dev build
// rebuilt in place (or its folder touched) counts as fresh.
func newest(dir string) time.Time {
	var t time.Time
	if fi, err := os.Stat(dir); err == nil {
		t = fi.ModTime()
	}
	es, _ := os.ReadDir(dir)
	for _, e := range es {
		if i, err := e.Info(); err == nil && i.ModTime().After(t) {
			t = i.ModTime()
		}
	}
	return t
}
