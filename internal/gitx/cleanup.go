package gitx

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Zovaris/smithy/internal/ghx"
)

const cleanupUsage = "usage: smithy cleanup <repo> <branch> [<worktree>] [--force]\n  removes <worktree> only when it is passed, and never the folder it runs in; a branch checked\n  out in another worktree is kept"

// WorktreeOf finds the worktree path that has branch checked out in `git worktree list --porcelain` output.
func WorktreeOf(porcelain, branch string) string {
	w := ""
	for _, l := range strings.Split(porcelain, "\n") {
		if p, ok := strings.CutPrefix(l, "worktree "); ok {
			w = p
		} else if l == "branch refs/heads/"+branch {
			return w
		}
	}
	return ""
}

// CleanupMain runs `smithy cleanup`: removes a finished task's worktree and local branch, and
// prints `cleaned <branch>` or `kept <branch>: <why>` (exit 1 when something was kept).
func CleanupMain(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, cleanupUsage)
		return 2
	}
	repo, branch, wt, force := args[0], args[1], "", false
	for _, a := range args[2:] {
		if a == "--force" {
			force = true
		} else {
			wt = a
		}
	}
	caller, _ := os.Getwd()
	if err := os.Chdir(repo); err != nil {
		fmt.Fprintln(os.Stderr, "smithy cleanup:", err)
		return 2
	}
	_ = quiet("git", "fetch", "-q", "--prune", "origin")
	if wt == "" {
		if other := WorktreeOf(git("worktree", "list", "--porcelain"), branch); other != "" && !samePath(other, repo) {
			fmt.Printf("kept %s: checked out in %s; pass that worktree to remove it\n", branch, other)
			return 1
		}
	} else if inside(caller, wt) {
		fmt.Printf("kept %s: %s is the folder this runs in\n", branch, wt)
		return 1
	}
	removed := false
	if fi, err := os.Stat(wt); wt != "" && err == nil && fi.IsDir() {
		if !force && git("-C", wt, "status", "--porcelain") != "" {
			fmt.Printf("kept %s: uncommitted changes in %s\n", branch, wt)
			return 1
		}
		ra := []string{"worktree", "remove"}
		if force {
			ra = append(ra, "--force")
		}
		if quiet("git", append(ra, wt)...) != nil {
			fmt.Printf("kept %s: could not remove %s\n", branch, wt)
			return 1
		}
		removed = true
	}
	attached("git", "worktree", "prune")
	base := git("symbolic-ref", "-q", "--short", "refs/remotes/origin/HEAD")
	if base == "" {
		base = "origin/main"
	}
	if quiet("git", "show-ref", "-q", "--verify", "refs/heads/"+branch) != nil {
		if !removed {
			fmt.Fprintln(os.Stderr, "smithy cleanup: no branch", branch)
			return 1
		}
		dropRemote(branch, base)
		fmt.Println("cleaned", branch)
		return 0
	}
	if git("branch", "--show-current") == branch {
		fmt.Printf("kept %s: it is checked out here\n", branch)
		return 1
	}
	unpushed, rlErr := ghx.Run("git", "rev-list", branch, "--not", "--remotes", "--exclude=refs/heads/"+branch, "--branches")
	if rlErr != nil {
		unpushed = "unknown"
	}
	merged := quiet("git", "merge-base", "--is-ancestor", branch, base) == nil || landed(branch, base)
	if force || merged || unpushed == "" {
		if stderrOnly("git", "branch", "-D", branch) == nil {
			dropRemote(branch, base)
			fmt.Println("cleaned", branch)
			return 0
		}
	}
	if removed {
		fmt.Printf("removed %s; kept branch %s: it has commits on no remote and no other branch; push it or pass --force\n", wt, branch)
		return 1
	}
	fmt.Printf("kept %s: has commits on no remote and no other branch; push it or pass --force\n", branch)
	return 1
}

// dropRemote deletes origin's copy of branch once it has landed in base (merged, or rebased and
// squashed in), so no feat/* branch lingers after a merge. It reports a failed push.
func dropRemote(branch, base string) {
	ref := "refs/remotes/origin/" + branch
	if quiet("git", "show-ref", "-q", "--verify", ref) != nil {
		return
	}
	if quiet("git", "merge-base", "--is-ancestor", ref, base) != nil && !landed(ref, base) {
		return
	}
	if quiet("git", "push", "-q", "origin", "--delete", branch) != nil {
		fmt.Printf("kept remote %s: could not delete it; delete it on the remote\n", branch)
	}
}

// landed reports whether every commit of branch not in base has an equivalent change in base,
// as after a rebase merge rewrote its hashes.
func landed(branch, base string) bool {
	out := git("cherry", base, branch)
	return out != "" && !strings.Contains("\n"+out, "\n+")
}

func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	a, _ := filepath.Abs(p)
	return a
}

func samePath(a, b string) bool { return realPath(a) == realPath(b) }

// inside reports whether dir is wt or a folder under it.
func inside(dir, wt string) bool {
	d, w := realPath(dir), realPath(wt)
	return d == w || strings.HasPrefix(d, w+string(filepath.Separator))
}
