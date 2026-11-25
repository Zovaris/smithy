// Package uninstall runs `smithy uninstall`: it lists everything smithy put on the machine, asks,
// and removes only that. The repo's uninstall.sh does the same without a smithy binary.
package uninstall

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/Zovaris/smithy/internal/agents"
	"github.com/Zovaris/smithy/internal/auto"
	"github.com/Zovaris/smithy/internal/daemon"
	"github.com/Zovaris/smithy/internal/ghx"
	"github.com/Zovaris/smithy/internal/layout"
)

const Usage = `usage: smithy uninstall [--yes] [--purge [--confirm-purge]]
  Lists what it would remove, with sizes, then asks (a terminal) or needs --yes:
  the installed versions and the smithy command (~/.local/share/smithy, ~/.local/bin/smithy, the
  old ~/.smithy/bin), the skill folders ` + "`smithy skills install`" + ` wrote, a running daemon,
  smithy-* container images, the smithy-internal/smithy-egress networks, and the PATH line
  install.sh or ` + "`smithy setup path add`" + ` added. ~/.smithy (keys, db, plans) stays unless --purge,
  and then only after typing purge (or --confirm-purge without a terminal).`

// Plan is what uninstall would remove.
type Plan struct {
	Paths     []string // folders and links, removed with RemoveAll
	Skills    []string // skill folders from the manifest (also in Paths)
	DaemonPID int
	Engine    string
	Images    []string
	Networks  []string
	RCFiles   []string // startup files holding the PATH line
	Home      string   // set with --purge
}

// Engine runs the container engine; tests replace it.
var Engine = func(e string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, e, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func inside(p, dir string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
}

func lines(file string) []string {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// Gather builds the plan from the machine; engine is "" to skip images and networks.
func Gather(purge bool, engine string) Plan {
	var p Plan
	data, legacy := layout.DataDir(), layout.LegacyBin()
	if fi, err := os.Stat(data); err == nil && fi.IsDir() {
		p.Paths = append(p.Paths, data)
	}
	if t, err := os.Readlink(layout.Link()); err == nil {
		if !filepath.IsAbs(t) {
			t = filepath.Join(layout.BinDir(), t)
		}
		if inside(t, data) || inside(t, legacy) {
			p.Paths = append(p.Paths, layout.Link())
		}
	} else if fi, err := os.Stat(layout.BinDir()); runtime.GOOS == "windows" && err == nil && fi.IsDir() && filepath.Base(layout.BinDir()) == "smithy" {
		p.Paths = append(p.Paths, layout.BinDir()) // Windows: %LOCALAPPDATA%\Programs\smithy is ours
	}
	if _, err := os.Lstat(legacy); err == nil {
		p.Paths = append(p.Paths, legacy)
	}
	for _, d := range lines(layout.SkillsManifest()) {
		fi, err := os.Lstat(d)
		rel, rerr := filepath.Rel(layout.UserHome(), d)
		if err != nil || !fi.IsDir() || rerr != nil || !strings.HasPrefix(rel, ".") || strings.HasPrefix(rel, "..") || filepath.Base(filepath.Dir(d)) != "skills" || !isKnown(filepath.Base(d)) {
			continue
		}
		if _, err := os.Stat(filepath.Join(d, "SKILL.md")); err == nil {
			p.Skills = append(p.Skills, d)
			p.Paths = append(p.Paths, d)
		}
	}
	if i, err := daemon.ReadInfo(); err == nil && i != nil && auto.AliveAs(i.PID, i.PIDStart) {
		p.DaemonPID = i.PID
	}
	if engine != "" {
		p.Engine = engine
		if out, err := Engine(engine, "image", "ls", "--format", "{{.Repository}}:{{.Tag}}"); err == nil {
			for _, l := range strings.Split(out, "\n") {
				if strings.HasPrefix(l, "smithy-") {
					p.Images = append(p.Images, l)
				}
			}
		}
		for _, n := range []string{agents.ProxyNetwork, agents.EgressNetwork} {
			if _, err := Engine(engine, "network", "inspect", n); err == nil {
				p.Networks = append(p.Networks, n)
			}
		}
	}
	for _, rc := range lines(layout.PathRecord()) {
		if b, err := os.ReadFile(rc); err == nil && hasLine(string(b), layout.PathMarker) {
			p.RCFiles = append(p.RCFiles, rc)
		}
	}
	if h := filepath.Clean(ghx.Home()); purge && h != filepath.Clean(layout.UserHome()) && h != filepath.Dir(h) {
		if fi, err := os.Stat(h); err == nil && fi.IsDir() {
			p.Home = ghx.Home()
		}
	}
	return p
}

func isKnown(name string) bool {
	for _, k := range agents.Known {
		if k == name {
			return true
		}
	}
	return false
}

func hasLine(text, line string) bool {
	for _, l := range strings.Split(text, "\n") {
		if l == line {
			return true
		}
	}
	return false
}

// Empty reports whether there is nothing to remove.
func (p Plan) Empty() bool {
	return len(p.Paths)+len(p.Images)+len(p.Networks)+len(p.RCFiles) == 0 && p.DaemonPID == 0 && p.Home == ""
}

func size(p string) string {
	var n int64
	_ = filepath.WalkDir(p, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if i, e := d.Info(); e == nil {
				n += i.Size()
			}
		}
		return nil
	})
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1fG", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1fM", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%dK", (n+1023)/1024)
}

// List writes the plan, one line per item, with sizes.
func (p Plan) List(w io.Writer) {
	fmt.Fprintln(w, "smithy uninstall: would remove")
	for _, x := range p.Paths {
		fmt.Fprintf(w, "  %-8s %s\n", size(x), x)
	}
	if p.DaemonPID != 0 {
		fmt.Fprintf(w, "  daemon   pid %d (stopped)\n", p.DaemonPID)
	}
	for _, i := range p.Images {
		fmt.Fprintf(w, "  image    %s\n", i)
	}
	for _, n := range p.Networks {
		fmt.Fprintf(w, "  network  %s\n", n)
	}
	for _, rc := range p.RCFiles {
		fmt.Fprintf(w, "  PATH line in %s\n", rc)
	}
	if p.Home != "" {
		fmt.Fprintf(w, "  %-8s %s\n           holds: %s\n", size(p.Home), p.Home, strings.Join(entries(p.Home), " "))
	}
}

func entries(dir string) []string {
	es, _ := os.ReadDir(dir)
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

// Apply removes what the plan lists; it reports each step to w and returns the failures.
func (p Plan) Apply(w io.Writer) int {
	failed := 0
	say := func(f string, a ...any) { fmt.Fprintf(w, "smithy uninstall: "+f+"\n", a...) }
	if p.DaemonPID != 0 {
		if pr, err := os.FindProcess(p.DaemonPID); err == nil && (pr.Signal(syscall.SIGTERM) == nil || pr.Kill() == nil) {
			say("stopped the daemon (pid %d)", p.DaemonPID)
		} else {
			failed++
			say("could not stop the daemon (pid %d)", p.DaemonPID)
		}
	}
	for _, i := range p.Images {
		if _, err := Engine(p.Engine, "image", "rm", i); err != nil {
			failed++
			say("could not remove image %s: %v", i, err)
		} else {
			say("removed image %s", i)
		}
	}
	for _, n := range p.Networks {
		if _, err := Engine(p.Engine, "network", "rm", n); err != nil {
			failed++
			say("could not remove network %s: %v", n, err)
		} else {
			say("removed network %s", n)
		}
	}
	for _, rc := range p.RCFiles {
		if err := dropPathLine(rc); err != nil {
			failed++
			say("could not edit %s: %v", rc, err)
		} else {
			say("removed the PATH line from %s", rc)
		}
	}
	if len(p.RCFiles) > 0 {
		os.Remove(layout.PathRecord())
	}
	if len(p.Skills) > 0 {
		os.Remove(layout.SkillsManifest())
	}
	for _, x := range p.Paths {
		if err := os.RemoveAll(x); err != nil {
			failed++
			say("could not remove %s: %v", x, err)
		} else {
			say("removed %s", x)
		}
	}
	if p.Home != "" {
		if err := os.RemoveAll(p.Home); err != nil {
			failed++
			say("could not remove %s: %v", p.Home, err)
		} else {
			say("removed %s", p.Home)
		}
	} else {
		say("kept %s (keys, db, plans); --purge removes it", ghx.Home())
	}
	return failed
}

// dropPathLine removes the marker line and the PATH line after it from rc.
func dropPathLine(rc string) error {
	b, err := os.ReadFile(rc)
	if err != nil {
		return err
	}
	var out []string
	ls := strings.Split(string(b), "\n")
	for i := 0; i < len(ls); i++ {
		if ls[i] == layout.PathMarker {
			if i+1 < len(ls) && (strings.HasPrefix(ls[i+1], "export PATH=") || strings.HasPrefix(ls[i+1], "fish_add_path ")) {
				i++
			}
			continue
		}
		out = append(out, ls[i])
	}
	fi, err := os.Stat(rc)
	if err != nil {
		return err
	}
	return os.WriteFile(rc, []byte(strings.Join(out, "\n")), fi.Mode().Perm())
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func ask(in *bufio.Reader, w io.Writer, q string) string {
	fmt.Fprint(w, q)
	s, _ := in.ReadString('\n')
	return strings.TrimSpace(s)
}

// Main runs `smithy uninstall`.
func Main(args []string) int {
	return Run(args, os.Stdin, isTerminal(os.Stdin), os.Stderr, agents.ContainerEngine(agents.HostProbe()))
}

// Run is Main with its input, terminal check, output and container engine given.
func Run(args []string, stdin io.Reader, tty bool, w io.Writer, engine string) int {
	yes, purge, confirm := false, false, false
	for _, a := range args {
		switch a {
		case "--yes", "-y":
			yes = true
		case "--purge":
			purge = true
		case "--confirm-purge":
			confirm = true
		case "-h", "--help":
			fmt.Fprintln(w, Usage)
			return 0
		default:
			fmt.Fprintln(w, Usage)
			return 2
		}
	}
	p := Gather(purge, engine)
	if p.Empty() {
		fmt.Fprintln(w, "smithy uninstall: nothing to remove")
		return 0
	}
	p.List(w)
	in := bufio.NewReader(stdin)
	if !yes {
		if !tty {
			fmt.Fprintln(w, "smithy uninstall: no terminal to ask; nothing removed. Re-run with --yes")
			return 1
		}
		switch strings.ToLower(ask(in, w, "smithy uninstall: remove all of the above? [y/N] ")) {
		case "y", "yes":
		default:
			fmt.Fprintln(w, "smithy uninstall: nothing removed")
			return 1
		}
	}
	if p.Home != "" && !confirm {
		if !tty {
			fmt.Fprintf(w, "smithy uninstall: keeping %s: --purge without a terminal also needs --confirm-purge\n", p.Home)
			p.Home = ""
		} else if ask(in, w, fmt.Sprintf("smithy uninstall: %s holds your keys (auth.json), the db and plans; type purge to delete it: ", p.Home)) != "purge" {
			fmt.Fprintf(w, "smithy uninstall: keeping %s\n", p.Home)
			p.Home = ""
		}
	}
	if p.Apply(w) > 0 {
		return 1
	}
	return 0
}
