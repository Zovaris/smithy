// Package selfupdate runs install.sh --update: the binary plus the skills.
package selfupdate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Usage is the help text of `smithy update`.
const Usage = `usage: smithy update [<version>] [--check] [--force]
Updates the smithy binary and the skills to the latest release, or to <version>. It downloads
install.sh from main and runs it as an update, which verifies the checksum and build attestation
and then runs smithy skills install.
--check prints the current version and the latest release tag and changes nothing (GH_TOKEN if set).
--force updates even when this binary is not the installed one (~/.local/bin/smithy), e.g. a dev build.`

// InstallURL and APIBase are package vars so tests can point them at a local server.
var (
	InstallURL = "https://raw.githubusercontent.com/Zovaris/smithy/main/install.sh"
	APIBase    = "https://api.github.com"
)

const maxScript = 1 << 20

var client = &http.Client{Timeout: 30 * time.Second}

// Options carry what the command needs from the running binary.
type Options struct {
	Version string
	Exe     string
	Stdout  io.Writer
	Stderr  io.Writer
}

// Main runs `smithy update` for the running binary.
func Main(args []string, version string) int {
	exe, _ := os.Executable()
	return Run(args, Options{Version: version, Exe: exe, Stdout: os.Stdout, Stderr: os.Stderr})
}

// Run parses args and checks or updates; it returns install.sh's exit code.
func Run(args []string, o Options) int {
	var check, force bool
	var pin string
	for _, a := range args {
		switch {
		case a == "--check":
			check = true
		case a == "--force":
			force = true
		case strings.HasPrefix(a, "-") || pin != "":
			fmt.Fprintf(o.Stderr, "smithy update: unexpected argument %s\n%s\n", a, Usage)
			return 2
		default:
			pin = strings.TrimPrefix(a, "v")
		}
	}
	if check {
		tag, err := latest()
		if err != nil {
			fmt.Fprintf(o.Stderr, "smithy update: %v\n", err)
			return 1
		}
		fmt.Fprintf(o.Stdout, "current %s\nlatest  %s\n", vtag(o.Version), tag)
		return 0
	}
	if !force && !isInstalled(o.Exe) {
		fmt.Fprintf(o.Stderr, "smithy update: this binary (%s) is not the installed one (%s); run that one, or pass --force\n", o.Exe, installedPath())
		return 2
	}
	file, err := download()
	if err != nil {
		fmt.Fprintf(o.Stderr, "smithy update: %v\n", err)
		return 1
	}
	defer func() { _ = os.Remove(file) }()
	shArgs := []string{file, "--update"}
	if pin != "" {
		shArgs = append(shArgs, pin)
	}
	cmd := exec.Command("sh", shArgs...) // #nosec G204 -- fixed program, our own args
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, o.Stdout, o.Stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		fmt.Fprintf(o.Stderr, "smithy update: %v\n", err)
		return 1
	}
	return 0
}

func vtag(v string) string {
	if v == "dev" || strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}

func installedPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "smithy", "smithy.exe")
	}
	if d := os.Getenv("XDG_BIN_HOME"); filepath.IsAbs(d) {
		return filepath.Join(d, "smithy")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "bin", "smithy")
}

func isInstalled(exe string) bool {
	a, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return false
	}
	b, err := filepath.EvalSymlinks(installedPath())
	return err == nil && a == b
}

func get(url string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(url, APIBase) {
		req.Header.Set("Accept", "application/vnd.github+json")
		if tok := os.Getenv("GH_TOKEN"); tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return resp, nil
}

func latest() (string, error) {
	resp, err := get(APIBase + "/repos/Zovaris/smithy/releases/latest")
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxScript)).Decode(&rel); err != nil || rel.TagName == "" {
		return "", fmt.Errorf("can't read the latest release: %v", err)
	}
	return rel.TagName, nil
}

func download() (string, error) {
	resp, err := get(InstallURL)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxScript+1))
	if err != nil {
		return "", err
	}
	if len(body) > maxScript {
		return "", fmt.Errorf("install.sh is larger than %d bytes", maxScript)
	}
	f, err := os.CreateTemp("", "smithy-install-*.sh")
	if err != nil {
		return "", err
	}
	if _, err := f.Write(body); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), f.Close()
}
