// Package cleanup runs `smithy cleanup builds`: old smithy builds, stale sandbox images and
// stale worktrees, listed by default and deleted only with --yes.
package cleanup

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/agents"
	"github.com/Zovaris/smithy/internal/ghx"
)

const Usage = "usage: smithy cleanup builds [--yes]\n  lists old smithy builds in $SMITHY_HOME/bin (keeps the current one and one previous; dev builds\n  after 14 days), stale smithy-* container images (not auto.sandbox_image) and stale worktrees of\n  the repo it runs in (clean including ignored files, not locked, with no commits beyond the base branch, not the folder it runs in), with the\n  space each takes; --yes deletes them. Worktrees with uncommitted changes are never touched."

// Item is one thing cleanup can remove: a build folder, an image id or a worktree path.
type Item struct {
	Kind, Target, Size string
	Bytes              int64
}

var engine = func(args ...string) (string, error) {
	p := agents.HostProbe()
	e := agents.ContainerEngine(p)
	if e == "" {
		return "", fmt.Errorf("no container engine")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, e, args...)
	if set, _ := agents.DockerHost(e, p); set != "" {
		c.Env = append(os.Environ(), "DOCKER_HOST="+set)
	}
	out, err := c.CombinedOutput()
	return strings.TrimSpace(string(out)), err
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

// Builds lists the version folders in bin (install.sh's bin/<version>/smithy) other than the one
// bin/smithy points at and the newest other release, plus dev builds (bin/dev/<key>) untouched for
// devMaxAge. Other folders and the folder of the running binary are never listed.
func Builds(bin string) []Item {
	ents, err := os.ReadDir(bin)
	if err != nil {
		return nil
	}
	current := ""
	if t, err := filepath.EvalSymlinks(filepath.Join(bin, "smithy")); err == nil {
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
	if devs, err := os.ReadDir(filepath.Join(bin, "dev")); err == nil {
		for _, d := range devs {
			if i, err := d.Info(); err == nil && d.IsDir() && time.Since(i.ModTime()) > devMaxAge {
				dirs = append(dirs, filepath.Join(bin, "dev", d.Name()))
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

// Images picks smithy-* images from `image ls` lines (repo, tag, id, size; tab separated) that are
// not the configured sandbox image.
func Images(ls, keep string) []Item {
	keepID := ""
	var rows [][]string
	for _, l := range strings.Split(ls, "\n") {
		if f := strings.Split(l, "\t"); len(f) == 4 {
			rows = append(rows, f)
			if f[0]+":"+f[1] == keep {
				keepID = f[2]
			}
		}
	}
	var out []Item
	for _, f := range rows {
		if !strings.HasPrefix(f[0], "smithy-") || f[1] == "<none>" || f[2] == keepID || f[0]+":"+f[1] == keep {
			continue
		}
		out = append(out, Item{Kind: "image", Target: f[0] + ":" + f[1], Size: f[3]})
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
	items := Builds(filepath.Join(ghx.Home(), "bin"))
	if keep, err := agents.SandboxSetting("auto.sandbox_image", agents.DefaultSandboxImage); err != nil {
		fmt.Fprintln(os.Stderr, "smithy cleanup: skipped images:", err)
	} else if ls, err := engine("image", "ls", "--format", "{{.Repository}}\t{{.Tag}}\t{{.ID}}\t{{.Size}}"); err == nil {
		items = append(items, Images(ls, keep)...)
	}
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
		case "image":
			var out string
			if out, err = engine("rmi", it.Target); err != nil {
				err = fmt.Errorf("%v: %s", err, out)
			}
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
	fmt.Printf("%s %s on disk, plus the image sizes listed; %d item(s)\n", verb, human(total), len(items))
	if !yes {
		fmt.Println("run smithy cleanup builds --yes to delete them")
	}
	if failed > 0 {
		return 1
	}
	return 0
}
