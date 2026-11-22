package plan

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/ghx"
	"github.com/Zovaris/smithy/internal/tasks"
)

const taskDirUsage = `usage: smithy task-dir <slug>
Resolves (and creates) the task dir for a new plan from the user's settings, so smelt and
warden put specs and tasks in the same place.
Prints the absolute dir. Exit 3 with ` + "`ask <key>: <question>`" + ` when a setting it needs is unset:
ask the user (the line ends with the default it would take), save it (` + "`smithy config set --repo <key> <value>`" + `), run it again.
  tasks.location  repo (default) | local | board
  tasks.commit    true: docs/specs/<slug> (committed) · false: .docs/specs/<slug> (kept out of git)
  tasks.ignore    with commit false — gitignore: add ` + "`.docs/`" + ` to .gitignore (commit that line
                  yourself) · exclude: add it to .git/info/exclude (local only, nothing committed)
Board: prints ` + "`board <dir>`" + ` (the spec still goes to the repo or local dir chosen by tasks.commit).`

// AskError means a setting task-dir needs is unset: ask the user Question, save Key, rerun.
type AskError struct{ Key, Question, Default string }

func (e *AskError) Error() string {
	line := "ask " + e.Key + ": " + e.Question
	if e.Default != "" {
		line += " (default: " + e.Default + ")"
	}
	return line
}

// ErrNotRepo means task-dir ran outside a git repo.
var ErrNotRepo = errors.New("not a git repo")

const (
	askCommit = "commit the specs and tasks to git (docs/specs), or keep them out of git (.docs/specs)?"
	askIgnore = "keep .docs/ out of git with a committed .gitignore line (gitignore), or only on this machine via .git/info/exclude (exclude)?"
)

// TaskDir resolves and creates the task dir for slug from the settings of the repo at the
// working directory; board reports tasks.location board. An *AskError names an unset setting.
func TaskDir(slug string) (dir string, board bool, err error) {
	top, err := ghx.Run("git", "rev-parse", "--show-toplevel")
	if err != nil || top == "" {
		return "", false, ErrNotRepo
	}
	excludeRepoConfig(top)
	loc := config.Get("tasks.location", "repo")
	if loc == "local" {
		dir = filepath.Join(tasks.PlansHome(), filepath.Base(top), slug)
		return dir, false, os.MkdirAll(dir, 0o755)
	}
	commit, _, cerr := config.Effective("tasks.commit")
	if cerr != nil {
		return "", false, &AskError{"tasks.commit", askCommit, "true → " + filepath.Join(top, "docs", "specs", slug)}
	}
	if commit == "true" {
		dir = filepath.Join(top, "docs", "specs", slug)
	} else {
		ignore, _, ierr := config.Effective("tasks.ignore")
		if ierr != nil {
			return "", false, &AskError{"tasks.ignore", askIgnore, "exclude → " + filepath.Join(top, ".docs", "specs", slug)}
		}
		f := filepath.Join(top, ".gitignore")
		if ignore != "gitignore" {
			if f, err = ghx.Run("git", "rev-parse", "--git-path", "info/exclude"); err != nil {
				return "", false, err
			}
			if !filepath.IsAbs(f) {
				cwd, _ := os.Getwd()
				f = filepath.Join(cwd, f)
			}
			if err = os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
				return "", false, err
			}
		}
		if err = ignoreLine(f, ".docs/"); err != nil {
			return "", false, err
		}
		dir = filepath.Join(top, ".docs", "specs", slug)
	}
	return dir, loc == "board", os.MkdirAll(dir, 0o755)
}

// excludeRepoConfig keeps an untracked .smithy/config.json out of git via the local
// .git/info/exclude, before the settings this command asks for can write it.
func excludeRepoConfig(top string) {
	if _, err := ghx.Run("git", "-C", top, "ls-files", "--error-unmatch", ".smithy/config.json"); err == nil {
		return
	}
	f, err := ghx.Run("git", "-C", top, "rev-parse", "--path-format=absolute", "--git-path", "info/exclude")
	if err != nil || os.MkdirAll(filepath.Dir(f), 0o755) != nil {
		return
	}
	_ = ignoreLine(f, ".smithy/config.json")
}

func ignoreLine(file, line string) error {
	b, _ := os.ReadFile(file)
	for _, l := range strings.Split(string(b), "\n") {
		if l == line {
			return nil
		}
	}
	add := line + "\n"
	if len(b) > 0 && b[len(b)-1] != '\n' {
		add = "\n" + add
	}
	fh, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = fh.WriteString(add)
	if cerr := fh.Close(); err == nil {
		err = cerr
	}
	return err
}

// TaskDirMain runs `smithy task-dir <slug>` and returns the exit code.
func TaskDirMain(args []string) int {
	if len(args) != 1 || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprintln(os.Stderr, taskDirUsage)
		return 2
	}
	dir, board, err := TaskDir(args[0])
	var ask *AskError
	switch {
	case errors.As(err, &ask):
		fmt.Println(ask.Error())
		return 3
	case errors.Is(err, ErrNotRepo):
		fmt.Fprintln(os.Stderr, err)
		return 2
	case err != nil:
		fmt.Fprintln(os.Stderr, err)
		return 1
	case board:
		fmt.Println("board", dir)
	default:
		fmt.Println(dir)
	}
	return 0
}
