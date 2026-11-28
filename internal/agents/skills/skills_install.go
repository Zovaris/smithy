package skills

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/agents/catalog"
	"github.com/Zovaris/smithy/internal/layout"
)

// SkillsArchive is the release asset holding every skill folder of this repo.
const SkillsArchive = "smithy-skills.tar.gz"

const maxSkillsArchive = 64 << 20

// BinaryVersion is set by main; `skills install` downloads the skills of this release.
var BinaryVersion = "dev"

var installHarnesses = catalog.Names(func(a catalog.Agent) bool { return a.Skills })

func harnessName(h string) string {
	if a, ok := catalog.Lookup(h); ok {
		return a.Tier
	}
	return h
}

func harnessMarker(h, home string, getenv func(string) string) string {
	switch h {
	case "claude":
		return filepath.Join(home, ".claude")
	case "codex":
		return filepath.Join(home, ".codex")
	case "opencode":
		return filepath.Join(envOr(getenv, "XDG_CONFIG_HOME", filepath.Join(home, ".config")), "opencode")
	case "agy":
		return filepath.Join(home, ".gemini")
	case "copilot":
		return envOr(getenv, "COPILOT_HOME", filepath.Join(home, ".copilot"))
	case "grok":
		return envOr(getenv, "GROK_HOME", filepath.Join(home, ".grok"))
	case "cursor":
		return filepath.Join(home, ".cursor")
	case "pi":
		return envOr(getenv, "PI_CODING_AGENT_DIR", filepath.Join(home, ".pi", "agent"))
	case "dsh":
		return envOr(getenv, "DSH_HOME", filepath.Join(home, ".dsh"))
	}
	return ""
}

// SkillsInstall configures InstallSkills.
type SkillsInstall struct {
	BaseURL   string
	Client    *http.Client
	Home      string
	Getenv    func(string) string
	Harnesses []string
	Out       io.Writer
	// Manifest, when set, gets each skill folder written (one per line) so uninstall removes only those.
	Manifest string
}

// RecordLine appends line to file unless it is there already.
func RecordLine(file, line string) {
	b, _ := os.ReadFile(file)
	for _, l := range strings.Split(string(b), "\n") {
		if l == line {
			return
		}
	}
	if os.MkdirAll(filepath.Dir(file), 0o755) != nil {
		return
	}
	if f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		fmt.Fprintln(f, line)
		f.Close()
	}
}

// InstallSkills downloads the release skills archive, checks it against the release's
// checksums.txt and copies this repo's skills into each harness's user skills folder.
func InstallSkills(o SkillsInstall) error {
	dirs, err := installTargets(o)
	if err != nil {
		return err
	}
	tgz, err := fetch(o.Client, o.BaseURL+"/"+SkillsArchive, maxSkillsArchive)
	if err != nil {
		return err
	}
	sums, err := fetch(o.Client, o.BaseURL+"/checksums.txt", 1<<20)
	if err != nil {
		return err
	}
	want := ""
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) == 2 && strings.TrimPrefix(f[1], "*") == SkillsArchive {
			want = f[0]
		}
	}
	got := sha256.Sum256(tgz)
	if want == "" || !strings.EqualFold(want, hex.EncodeToString(got[:])) {
		return fmt.Errorf("checksum mismatch for %s, nothing installed", SkillsArchive)
	}
	stage, err := os.MkdirTemp("", "smithy-skills-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := untar(tgz, stage); err != nil {
		return fmt.Errorf("%s: %v, nothing installed", SkillsArchive, err)
	}
	var linked []string
	for _, d := range dirs {
		for _, s := range Known {
			src := filepath.Join(stage, s)
			if !isFile(filepath.Join(src, "SKILL.md")) {
				continue
			}
			dst := filepath.Join(d, s)
			if fi, err := os.Lstat(dst); err == nil && fi.Mode()&os.ModeSymlink != 0 {
				linked = append(linked, dst)
				continue
			}
			if err := replaceDir(src, dst); err != nil {
				return err
			}
			if o.Manifest != "" {
				RecordLine(o.Manifest, dst)
			}
		}
		fmt.Fprintf(o.Out, "installed skills into %s\n", d)
	}
	if len(linked) > 0 {
		fmt.Fprintf(o.Out, "skipped %d symlinked skills (a checkout?), left as is: %s\n", len(linked), strings.Join(linked, ", "))
	}
	return nil
}

func installTargets(o SkillsInstall) ([]string, error) {
	hs := o.Harnesses
	if len(hs) == 0 {
		for _, h := range installHarnesses {
			if fi, err := os.Stat(harnessMarker(h, o.Home, o.Getenv)); err == nil && fi.IsDir() {
				hs = append(hs, h)
			}
		}
		if len(hs) == 0 {
			return nil, fmt.Errorf("no harness found; pass --harness <%s>", strings.Join(installHarnesses, "|"))
		}
	}
	var dirs []string
	seen := map[string]bool{}
	for _, h := range hs {
		d := HarnessSkillDirs(harnessName(h), "", o.Home, o.Getenv)
		if d == nil {
			return nil, fmt.Errorf("unknown harness %q (known: %s)", h, strings.Join(installHarnesses, ", "))
		}
		if !seen[d[0]] {
			seen[d[0]] = true
			dirs = append(dirs, d[0])
		}
	}
	return dirs, nil
}

func fetch(c *http.Client, url string, limit int64) ([]byte, error) {
	resp, err := c.Get(url)
	if err != nil {
		return nil, fmt.Errorf("download %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("download %s: %v", url, err)
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("download %s: larger than %d bytes", url, limit)
	}
	return b, nil
}

func untar(tgz []byte, dst string) error {
	gz, err := gzip.NewReader(bytes.NewReader(tgz))
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	var total int64
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := strings.TrimPrefix(h.Name, "./")
		clean := path.Clean(name)
		if name == "" || clean == "." {
			continue
		}
		if path.IsAbs(name) || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(name, `\`) {
			return fmt.Errorf("unsafe path %q", h.Name)
		}
		p := filepath.Join(dst, filepath.FromSlash(clean))
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			total += h.Size
			if total > 4*maxSkillsArchive {
				return errors.New("archive expands too large")
			}
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(0o644)
			if h.Mode&0o111 != 0 {
				mode = 0o755
			}
			f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
			if err != nil {
				return err
			}
			_, err = io.Copy(f, io.LimitReader(tr, h.Size))
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported entry %q (type %c)", h.Name, h.Typeflag)
		}
	}
}

func replaceDir(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.new-%d", dst, time.Now().UnixNano())
	if err := os.CopyFS(tmp, os.DirFS(src)); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	old := ""
	if _, err := os.Lstat(dst); err == nil {
		old = fmt.Sprintf("%s.old-%d", dst, time.Now().UnixNano())
		if err := os.Rename(dst, old); err != nil {
			os.RemoveAll(tmp)
			return err
		}
	}
	if err := os.Rename(tmp, dst); err != nil {
		if old != "" {
			_ = os.Rename(old, dst)
		}
		os.RemoveAll(tmp)
		return err
	}
	if old != "" {
		os.RemoveAll(old)
	}
	return nil
}

func parseInstallArgs(args []string) ([]string, error) {
	var hs []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--harness":
			if i+1 >= len(args) {
				return nil, errors.New("--harness needs a name")
			}
			i++
			hs = append(hs, args[i])
		case strings.HasPrefix(a, "--harness="):
			hs = append(hs, strings.TrimPrefix(a, "--harness="))
		case strings.HasPrefix(a, "-"):
			return nil, fmt.Errorf("unknown flag %s", a)
		default:
			hs = append(hs, a)
		}
	}
	return hs, nil
}

func skillsInstallMain(args []string) int {
	hs, err := parseInstallArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "smithy skills install:", err)
		return 2
	}
	if BinaryVersion == "dev" {
		fmt.Fprintln(os.Stderr, "smithy skills install: a dev build has no release to download; install a release binary (install.sh)")
		return 1
	}
	home := layout.UserHome()
	err = InstallSkills(SkillsInstall{
		BaseURL: "https://github.com/Zovaris/smithy/releases/download/v" + strings.TrimPrefix(BinaryVersion, "v"),
		Client: &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if r.URL.Scheme != "https" || len(via) > 5 {
				return errors.New("refusing redirect to " + r.URL.String())
			}
			return nil
		}},
		Home:      home,
		Getenv:    os.Getenv,
		Harnesses: hs,
		Out:       os.Stdout,
		Manifest:  layout.SkillsManifest(),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "smithy skills install:", err)
		return 1
	}
	return 0
}
