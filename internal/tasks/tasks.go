// Package tasks reads and writes the shared task list: one Markdown file per task
// (`NN-<slug>.md`, `# NN · <title>` then `Key: value` header lines) in a task dir.
package tasks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/cards"
	"github.com/Zovaris/smithy/internal/ghx"
	"github.com/Zovaris/smithy/internal/names"
)

// Statuses a task may have.
var Statuses = []string{"todo", "doing", "done", "dropped"}

var (
	taskRe   = regexp.MustCompile(`^\d{2,}-.*\.md$`)
	headerRe = regexp.MustCompile(`^([A-Z][A-Za-z ]+):\s*(.*)$`)
	numRe    = regexp.MustCompile(`^(\d+)`)
	nonDigit = regexp.MustCompile(`\D`)
)

// Task is one parsed task file.
type Task struct {
	File, NN, Title, Status, PR string
	Blockers                    []string
}

func isTask(name string) bool { return taskRe.MatchString(name) }

// PlansHome is where local plans live: $SMELT_PLANS, else <smithy home>/plans.
func PlansHome() string {
	if p := os.Getenv("SMELT_PLANS"); p != "" {
		return p
	}
	return filepath.Join(ghx.Home(), "plans")
}

func repoRoot() string {
	cwd, _ := os.Getwd()
	if r := ghx.RepoRoot(cwd); r != "" {
		return r
	}
	return cwd
}

func hasTasks(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if isTask(e.Name()) {
			return true
		}
	}
	return false
}

func mtime(p string) time.Time {
	fi, err := os.Stat(p)
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}

// Dirs lists this repo's task dirs, newest first: docs/specs/*, .docs/specs/*,
// <plans>/<repo>/*, and any colon-separated $SMELT_TASKS_DIRS.
func Dirs() []string { return scan(true) }

// scan lists the subdirs of the task-dir bases, newest first; withTasks keeps only those
// holding tasks.
func scan(withTasks bool) []string {
	root := repoRoot()
	bases := []string{filepath.Join(root, "docs", "specs"), filepath.Join(root, ".docs", "specs"), filepath.Join(PlansHome(), filepath.Base(root))}
	for _, d := range strings.Split(os.Getenv("SMELT_TASKS_DIRS"), ":") {
		if d != "" {
			if !filepath.IsAbs(d) {
				d = filepath.Join(root, d)
			}
			bases = append(bases, d)
		}
	}
	var dirs []string
	for _, b := range bases {
		entries, err := os.ReadDir(b)
		if err != nil {
			continue
		}
		for _, e := range entries {
			full := filepath.Join(b, e.Name())
			if e.IsDir() && (!withTasks || hasTasks(full)) {
				dirs = append(dirs, full)
			}
		}
	}
	sort.SliceStable(dirs, func(i, j int) bool { return mtime(dirs[i]).After(mtime(dirs[j])) })
	return dirs
}

func resolve(dir string) (string, error) {
	if dir != "" {
		if _, err := os.Stat(dir); err != nil {
			return "", fmt.Errorf("no task dir %s", dir)
		}
		return filepath.Abs(dir)
	}
	d := Dirs()
	if len(d) == 0 {
		d = scan(false)
	}
	if len(d) == 0 {
		return "", fmt.Errorf("no task dir found (docs/specs/, .docs/specs/, ~/.smithy/plans/<repo>/ or $SMELT_TASKS_DIRS); pass one")
	}
	return d[0], nil
}

// Parse reads one task file.
func Parse(file string) (Task, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return Task{}, err
	}
	lines := strings.Split(string(b), "\n")
	t := Task{File: file, Status: "todo", PR: "-"}
	if m := numRe.FindStringSubmatch(filepath.Base(file)); m != nil {
		t.NN = m[1]
	}
	for _, l := range lines {
		if strings.HasPrefix(l, "# ") {
			title := strings.TrimSpace(strings.TrimPrefix(l, "#"))
			if i := strings.Index(title, "·"); i >= 0 && numRe.MatchString(title) {
				title = strings.TrimSpace(title[i+len("·"):])
			}
			t.Title = title
			break
		}
	}
	end := len(lines)
	if end > 15 {
		end = 15
	}
	got := map[string]bool{}
	for _, l := range lines[min(1, len(lines)):end] {
		if strings.HasPrefix(l, "## ") {
			break
		}
		m := headerRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		v := strings.TrimSpace(m[2])
		k := strings.ToLower(m[1])
		if got[k] {
			continue
		}
		got[k] = true
		switch k {
		case "status":
			t.Status = strings.ToLower(v)
		case "pr":
			t.PR = v
		case "blocked by":
			if strings.ToLower(v) != "none" {
				for _, f := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' }) {
					if n := nonDigit.ReplaceAllString(f, ""); n != "" {
						t.Blockers = append(t.Blockers, n)
					}
				}
			}
		}
	}
	return t, nil
}

// Load reads every task in a dir, in NN order, and remembers the dir for `board --all`.
func Load(dir string) []Task {
	remember(dir)
	return load(dir)
}

func load(dir string) []Task {
	entries, _ := os.ReadDir(dir)
	var out []Task
	for _, e := range entries {
		if isTask(e.Name()) {
			if t, err := Parse(filepath.Join(dir, e.Name())); err == nil {
				out = append(out, t)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return filepath.Base(out[i].File) < filepath.Base(out[j].File) })
	return out
}

func num(s string) int { n, _ := strconv.Atoi(s); return n }

// Unblocked returns todo tasks whose blockers are all done or dropped.
func Unblocked(ts []Task) []Task {
	done := map[int]bool{}
	for _, t := range ts {
		if t.Status == "done" || t.Status == "dropped" {
			done[num(t.NN)] = true
		}
	}
	var out []Task
	for _, t := range ts {
		if t.Status != "todo" {
			continue
		}
		ok := true
		for _, b := range t.Blockers {
			if !done[num(b)] {
				ok = false
			}
		}
		if ok {
			out = append(out, t)
		}
	}
	return out
}

// Set updates header lines (Status, PR, Owner...) and adds missing ones.
func Set(file string, pairs []string) (Task, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return Task{}, fmt.Errorf("no task file %s", file)
	}
	lines := strings.Split(string(b), "\n")
	h := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "# ") {
			h = i
			break
		}
	}
	if h < 0 {
		return Task{}, fmt.Errorf("%s has no '# NN · title' line", file)
	}
	hdr := regexp.MustCompile(`^[A-Z][A-Za-z ]+:\s`)
	for _, p := range pairs {
		i := strings.IndexByte(p, '=')
		if i < 1 {
			return Task{}, fmt.Errorf("not key=value: %s", p)
		}
		key := strings.TrimSpace(p[:i])
		if strings.EqualFold(key, "pr") {
			key = "PR"
		} else {
			key = strings.ToUpper(key[:1]) + key[1:]
		}
		val := strings.TrimSpace(p[i+1:])
		if key == "Status" && !contains(Statuses, val) {
			return Task{}, fmt.Errorf("Status must be one of %s", strings.Join(Statuses, ", "))
		}
		end := h + 1
		for end < len(lines) && hdr.MatchString(lines[end]) {
			end++
		}
		at := -1
		for j := h + 1; j < end; j++ {
			if strings.HasPrefix(strings.ToLower(lines[j]), strings.ToLower(key)+":") {
				at = j
			}
		}
		if at >= 0 {
			lines[at] = lines[at][:len(key)] + ": " + val // keep the file's own spelling (PR, not Pr)
		} else {
			lines = append(lines[:end], append([]string{key + ": " + val}, lines[end:]...)...)
		}
	}
	if err := os.WriteFile(file, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		return Task{}, err
	}
	return Parse(file)
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

const taskTemplate = "\n## What\n\n## Acceptance criteria\n- [ ] \n\n## Files\n- create:  · modify:  · test: \n\n## Interfaces\n- Consumes: \n- Produces: \n\n## Notes\n"

// Add writes the next NN task file from the template and returns its path.
// speed is fast, normal or full ("" means fast).
func Add(dir, title, blockedBy, key, speed string) (string, error) {
	if blockedBy == "" {
		blockedBy = "none"
	}
	if speed == "" {
		speed = "fast"
	}
	if speed != "fast" && speed != "normal" && speed != "full" {
		return "", fmt.Errorf("unknown speed %q (fast, normal, full)", speed)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	max := 0
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if isTask(e.Name()) {
			if n := num(numRe.FindString(e.Name())); n > max {
				max = n
			}
		}
	}
	nn := fmt.Sprintf("%02d", max+1)
	slug := names.Slug(title)
	if slug == "" {
		slug = "task"
	}
	file := filepath.Join(dir, nn+"-"+slug+".md")
	var b strings.Builder
	fmt.Fprintf(&b, "# %s · %s\nStatus: todo\nBlocked by: %s\nSpeed: %s\nPR: -\n", nn, title, blockedBy, speed)
	if key != "" {
		fmt.Fprintf(&b, "Key: %s\n", key)
	}
	if _, err := os.Stat(filepath.Join(dir, "spec.md")); err == nil {
		b.WriteString("Spec: ./spec.md\n")
	}
	b.WriteString(taskTemplate)
	if err := os.WriteFile(file, []byte(b.String()), 0o644); err != nil {
		return "", err
	}
	remember(dir)
	return file, nil
}

var sectionRe = regexp.MustCompile(`^##\s+(\d+)(?:\s*[·.\-]|\s+|$)\s*(.*?)\s*$`)

// Import splits a task-list file whose sections are `## NN · Title` (or `## NN Title`) into task
// files in dir (default: the file's folder) and returns their paths. A section's `Blocked by:` and
// `Speed:` lines move into the task header. It refuses to overwrite an NN that exists, and writes
// every file or none.
func Import(file, dir string) ([]string, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	if dir == "" {
		dir = filepath.Dir(file)
	}
	type sec struct {
		nn, title, blocked, speed string
		body                      []string
	}
	var secs []sec
	fence, titled := false, false
	text := strings.ReplaceAll(strings.TrimPrefix(string(b), "\ufeff"), "\r\n", "\n")
	for i, l := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			fence = !fence
			if len(secs) > 0 {
				secs[len(secs)-1].body = append(secs[len(secs)-1].body, l)
				continue
			}
		}
		if !fence && (strings.HasPrefix(l, "## ") || l == "##") {
			m := sectionRe.FindStringSubmatch(l)
			if m == nil {
				return nil, fmt.Errorf("%s:%d: heading %q is not a numbered section (`## NN · Title`)", file, i+1, strings.TrimSpace(l))
			}
			if m[2] == "" {
				return nil, fmt.Errorf("%s:%d: section %s has an empty title", file, i+1, m[1])
			}
			secs = append(secs, sec{nn: fmt.Sprintf("%02d", num(m[1])), title: m[2]})
			continue
		}
		if len(secs) == 0 {
			if strings.TrimSpace(l) == "" {
				continue
			}
			if !fence && !titled && strings.HasPrefix(l, "# ") {
				titled = true
				continue
			}
			return nil, fmt.Errorf("%s:%d: text before the first `## NN · Title` section", file, i+1)
		}
		cur := &secs[len(secs)-1]
		if !fence {
			if m := headerRe.FindStringSubmatch(l); m != nil {
				switch k := strings.ToLower(m[1]); {
				case k == "blocked by" && cur.blocked == "":
					cur.blocked = strings.TrimSpace(m[2])
					continue
				case k == "speed" && cur.speed == "":
					cur.speed = strings.TrimSpace(m[2])
					continue
				}
			}
		}
		cur.body = append(cur.body, l)
	}
	if len(secs) == 0 {
		return nil, fmt.Errorf("no `## NN · Title` sections in %s", file)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, s := range secs {
		if seen[s.nn] {
			return nil, fmt.Errorf("section %s appears twice in %s", s.nn, file)
		}
		seen[s.nn] = true
		for _, e := range entries {
			if isTask(e.Name()) && num(numRe.FindString(e.Name())) == num(s.nn) {
				return nil, fmt.Errorf("task %s exists: %s", s.nn, filepath.Join(dir, e.Name()))
			}
		}
	}
	_, specErr := os.Stat(filepath.Join(dir, "spec.md"))
	var paths, tmps []string
	cleanup := func() {
		for _, p := range tmps {
			os.Remove(p)
		}
		for _, p := range paths {
			os.Remove(p)
		}
	}
	for _, s := range secs {
		slug := names.Slug(s.title)
		if slug == "" {
			slug = "task"
		}
		p := filepath.Join(dir, s.nn+"-"+slug+".md")
		blocked, speed := s.blocked, s.speed
		if blocked == "" {
			blocked = "none"
		}
		if speed == "" {
			speed = "fast"
		}
		txt := fmt.Sprintf("# %s · %s\nStatus: todo\nBlocked by: %s\nSpeed: %s\nPR: -\n", s.nn, s.title, blocked, speed)
		if specErr == nil {
			txt += "Spec: ./spec.md\n"
		}
		txt += "\n" + strings.TrimSpace(strings.Join(s.body, "\n")) + "\n"
		tmp := filepath.Join(dir, "."+filepath.Base(p)+".tmp")
		tmps = append(tmps, tmp)
		if err := os.WriteFile(tmp, []byte(txt), 0o644); err != nil {
			cleanup()
			return nil, err
		}
		paths = append(paths, p)
	}
	var done []string
	for i, tmp := range tmps {
		if err := os.Rename(tmp, paths[i]); err != nil {
			for _, p := range done {
				os.Remove(p)
			}
			paths = nil
			cleanup()
			return nil, err
		}
		done = append(done, paths[i])
	}
	remember(dir)
	return paths, nil
}

func indexFile() string { return filepath.Join(ghx.Home(), "tasks-index.json") }

func readIndex() map[string]map[string]string {
	idx := map[string]map[string]string{}
	if b, err := os.ReadFile(indexFile()); err == nil {
		_ = json.Unmarshal(b, &idx)
	}
	return idx
}

func writeIndex(idx map[string]map[string]string) {
	if os.MkdirAll(ghx.Home(), 0o755) != nil {
		return
	}
	b, _ := json.MarshalIndent(idx, "", " ")
	_ = os.WriteFile(indexFile(), b, 0o644)
}

// remember records a task dir so `board --all` finds lists in any repo without a tracker.
func remember(dir string) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return
	}
	idx := readIndex()
	if _, ok := idx[abs]; ok {
		return
	}
	idx[abs] = map[string]string{"repo": filepath.Base(repoRoot()), "seen": time.Now().UTC().Format(time.RFC3339)}
	writeIndex(idx)
}

// Summary is "3/7 done, 1 doing, 2 ready".
func Summary(ts []Task) string {
	n := func(st string) int {
		c := 0
		for _, t := range ts {
			if t.Status == st {
				c++
			}
		}
		return c
	}
	return fmt.Sprintf("%d/%d done, %d doing, %d ready", n("done"), len(ts), n("doing"), len(Unblocked(ts)))
}

// Board lists task lists (this repo, or every remembered one with all) and warden batches.
func Board(all bool) []string {
	type entry struct{ dir, repo string }
	var list []entry
	seen := map[string]bool{}
	if all {
		idx := readIndex()
		live := map[string]map[string]string{}
		for d, v := range idx {
			if hasTasks(d) {
				live[d] = v
				list = append(list, entry{d, v["repo"]})
				seen[d] = true
			}
		}
		if len(live) != len(idx) {
			writeIndex(live)
		}
	}
	for _, d := range Dirs() {
		if !seen[d] {
			list = append(list, entry{d, filepath.Base(repoRoot())})
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].repo != list[j].repo {
			return list[i].repo < list[j].repo
		}
		return list[i].dir < list[j].dir
	})
	var rows [][]string
	for _, e := range list {
		rows = append(rows, []string{e.repo + "/" + filepath.Base(e.dir), Summary(load(e.dir)), e.dir})
	}
	for _, b := range cards.Batches() {
		r, err := cards.Rows(b)
		if err != nil || len(r) == 0 {
			continue
		}
		counts := map[string]int{}
		var order []string
		final := true
		for _, row := range r {
			if counts[row.Stage] == 0 {
				order = append(order, row.Stage)
			}
			counts[row.Stage]++
			if !contains([]string{"merged", "reported", "dropped"}, row.Stage) {
				final = false
			}
		}
		if final && !all {
			continue
		}
		parts := make([]string, len(order))
		for i, st := range order {
			parts[i] = fmt.Sprintf("%d %s", counts[st], st)
		}
		rows = append(rows, []string{"batch " + b, strings.Join(parts, ", "), ""})
	}
	if len(rows) == 0 {
		return nil
	}
	return strings.Split(cards.Table(rows), "\n")
}

const usage = `usage: smithy tasks <command>
  list  [dir]                       table of tasks with status
  next  [dir]                       first todo task whose blockers are done (exit 3 if none)
  ready [dir]                       every todo task whose blockers are done
  set   <file> key=value...         update header lines (Status, PR, Owner...)
  add   [dir] "<title>" [--blocked-by 01,02] [--key PROJ-12] [--speed fast|normal|full]   next NN file from the full template
  import <file> [dir]               split a list of "## NN · Title" sections into task files (default dir: the file's folder; never overwrites)
  board [--all]                     one line per task list and warden batch: this repo, or every repo
  where                             task dirs found for this repo
Status: todo | doing | done | dropped. Blocked by: 01, 03 | none. PR: <url> | -`

// Main runs `smithy tasks`.
func Main(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}
	arg := func(i int) string {
		if i < len(args) {
			return args[i]
		}
		return ""
	}
	fail := func(err error) int { fmt.Fprintln(os.Stderr, err); return 1 }
	switch args[0] {
	case "list":
		dir, err := resolve(arg(1))
		if err != nil {
			return fail(err)
		}
		ts := Load(dir)
		rows := [][]string{{"#", "status", "blocked by", "pr", "title"}}
		for _, t := range ts {
			b := strings.Join(t.Blockers, ",")
			if b == "" {
				b = "-"
			}
			rows = append(rows, []string{t.NN, t.Status, b, t.PR, t.Title})
		}
		fmt.Printf("%s\n%s\n%s\n", dir, cards.Table(rows), Summary(ts))
	case "next", "ready":
		dir, err := resolve(arg(1))
		if err != nil {
			return fail(err)
		}
		ts := Load(dir)
		u := Unblocked(ts)
		if len(ts) == 0 {
			hint := ""
			if lists, _ := filepath.Glob(filepath.Join(dir, "*.md")); len(lists) > 0 {
				hint = "; a single task list? smithy tasks import <file>"
			}
			fmt.Fprintf(os.Stderr, "no task files in %s%s\n", dir, hint)
			return 3
		}
		if len(u) == 0 {
			if args[0] == "next" {
				allDone := true
				for _, t := range ts {
					if t.Status != "done" && t.Status != "dropped" {
						allDone = false
					}
				}
				if allDone {
					fmt.Fprintln(os.Stderr, "all done")
				} else {
					fmt.Fprintln(os.Stderr, "nothing ready: the rest is doing or blocked")
				}
			}
			return 3
		}
		if args[0] == "next" {
			u = u[:1]
		}
		for _, t := range u {
			fmt.Println(t.File)
		}
	case "set":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, usage)
			return 2
		}
		t, err := Set(args[1], args[2:])
		if err != nil {
			return fail(err)
		}
		b, _ := json.Marshal(map[string]any{"file": t.File, "nn": t.NN, "title": t.Title, "status": t.Status, "blockers": t.Blockers, "pr": t.PR})
		fmt.Println(string(b))
	case "add":
		var rest []string
		blocked, key, speed := "", "", ""
		for i := 1; i < len(args); i++ {
			switch args[i] {
			case "--blocked-by":
				i++
				blocked = arg(i)
			case "--key":
				i++
				key = arg(i)
			case "--speed":
				i++
				speed = arg(i)
			default:
				rest = append(rest, args[i])
			}
		}
		if len(rest) == 0 {
			fmt.Fprintln(os.Stderr, `usage: smithy tasks add [dir] "<title>" [--blocked-by 01,02] [--key K] [--speed fast|normal|full]`)
			return 2
		}
		title := rest[len(rest)-1]
		var dir string
		var err error
		if len(rest) > 1 {
			dir, err = filepath.Abs(rest[0])
		} else {
			dir, err = resolve("")
		}
		if err != nil {
			return fail(err)
		}
		f, err := Add(dir, title, blocked, key, speed)
		if err != nil {
			return fail(err)
		}
		fmt.Println(f)
	case "import":
		if len(args) < 2 || len(args) > 3 {
			fmt.Fprintln(os.Stderr, "usage: smithy tasks import <tasks.md> [dir]")
			return 2
		}
		dir := ""
		if len(args) == 3 {
			d, err := filepath.Abs(args[2])
			if err != nil {
				return fail(err)
			}
			dir = d
		}
		file, err := filepath.Abs(args[1])
		if err != nil {
			return fail(err)
		}
		ps, err := Import(file, dir)
		if err != nil {
			return fail(err)
		}
		fmt.Println(strings.Join(ps, "\n"))
	case "board":
		lines := Board(contains(args, "--all"))
		if lines == nil {
			fmt.Fprintln(os.Stderr, "no task lists or batches yet")
			return 3
		}
		fmt.Println(strings.Join(lines, "\n"))
	case "where":
		d := Dirs()
		if len(d) == 0 {
			fmt.Fprintln(os.Stderr, "no task dir for this repo (smithy task-dir <slug> makes one)")
			return 3
		}
		fmt.Println(strings.Join(d, "\n"))
	default:
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}
	return 0
}
