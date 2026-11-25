package agents

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/Zovaris/smithy/internal/ghx"
	"github.com/Zovaris/smithy/internal/layout"
	"github.com/Zovaris/smithy/internal/proxy"
)

// Under auto.sandbox_network proxy, container workers join ProxyNetwork (internal: no route out)
// and reach the outside only through the ProxyName sidecar, which is on it and on EgressNetwork,
// a network of its own (not the shared bridge, where other containers could call it).
const (
	ProxyNetwork  = "smithy-internal"
	EgressNetwork = "smithy-egress"
	ProxyName     = "smithy-proxy"
)

// ProxyURL is the sidecar as a worker sees it.
var ProxyURL = "http://" + ProxyName + ":" + strconv.Itoa(proxy.Port)

// ProxyNote tells a proxied worker how to reach the network and use secrets.
var ProxyNote = "Network: only GitHub, model APIs, package registries and the hosts the user allowed are reachable, through smithy's egress proxy. " +
	"Secrets: you never see API keys. To call a service with secret:<name>, send the request to " + ProxyURL + "/s/<name>/<path> " +
	"(for example curl " + ProxyURL + "/s/linear/graphql, with curl options after the URL); the proxy adds the key for the host the user bound it to. " +
	"Don't send auth headers; a write may wait for the user's approval or be refused (403)."

func goarch() string { return runtime.GOARCH }

// ProxyBinary is the linux smithy binary the sidecar runs: auto.sandbox_proxy_binary (an absolute
// path), else this binary on linux, else <data dir>/smithy-linux-<arch>, or the legacy
// <smithy home>/bin/smithy-linux-<arch> when only that one exists.
func ProxyBinary(goos, arch, setting, exe, data, home string, exists func(string) bool) (string, error) {
	switch {
	case setting != "":
		if !filepath.IsAbs(setting) {
			return "", fmt.Errorf("auto.sandbox_proxy_binary %q is not an absolute path", setting)
		}
		return setting, nil
	case goos == "linux":
		return exe, nil
	}
	b := filepath.Join(data, "smithy-linux-"+arch)
	if legacy := filepath.Join(home, "bin", "smithy-linux-"+arch); exists != nil && !exists(b) && exists(legacy) {
		return legacy, nil
	}
	return b, nil
}

func proxyBinary(p Probe) (string, error) {
	set, err := SandboxSetting("auto.sandbox_proxy_binary", "")
	if err != nil {
		return "", err
	}
	exe, _ := os.Executable()
	b, err := ProxyBinary(p.GOOS, goarch(), set, exe, layout.DataDir(), ghx.Home(), p.Exists)
	if err != nil {
		return "", err
	}
	if p.Exists == nil || !p.Exists(b) {
		return "", fmt.Errorf("no linux smithy for the proxy sidecar at %s: build it from the smithy repo with GOOS=linux GOARCH=%s CGO_ENABLED=0 go build -o %s ./cmd/smithy, or set auto.sandbox_proxy_binary", b, goarch(), b)
	}
	return b, nil
}

// ProxyDomains is proxy.DefaultDomains plus the comma list extra (auto.sandbox_domains).
func ProxyDomains(extra string) []string {
	out := append([]string{}, proxy.DefaultDomains...)
	for _, d := range strings.Split(extra, ",") {
		if d = strings.ToLower(strings.TrimSpace(d)); d != "" && !slices.Contains(out, d) {
			out = append(out, d)
		}
	}
	return out
}

// ProxySidecar is the smithy-proxy container: on smithy-egress, read-only, no capabilities, running the
// linux smithy as `smithy proxy serve` with only auth.json (read-only, bound secrets) and
// <home>/proxy (its events: asks, approvals, refusals) mounted. Workers mount neither.
type ProxySidecar struct {
	Engine, Image, Bin, Home, User string
	Allow                          []string
}

// Hash identifies the sidecar's config, so a changed allow list, binary or auth.json restarts it:
// auth.json is rewritten by rename, which a single-file mount never sees.
func (s ProxySidecar) Hash() string {
	auth, _ := os.ReadFile(filepath.Join(s.Home, "auth.json"))
	sum := sha256.Sum256(auth)
	h := sha256.Sum256([]byte(strings.Join([]string{EgressNetwork, "narrow-mounts", s.Image, s.Bin, s.Home, s.User, strings.Join(s.Allow, ","), hex.EncodeToString(sum[:])}, "\x00")))
	return hex.EncodeToString(h[:8])
}

// RunArgv starts the sidecar.
func (s ProxySidecar) RunArgv() []string {
	a := []string{s.Engine, "run", "-d", "--rm", "--name", ProxyName, "--network", EgressNetwork, "--label", "smithy.proxy=" + s.Hash(),
		"--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--read-only", "--pids-limit", "128", "--memory", "256m"}
	if s.User != "" {
		a = append(a, "--user", s.User)
	}
	return append(a, "-v", s.Bin+":/usr/local/bin/smithy-proxy:ro", "-v", filepath.Join(s.Home, "auth.json")+":/smithy/auth.json:ro", "-v", filepath.Join(s.Home, "proxy")+":/smithy/proxy", "-e", "SMITHY_HOME=/smithy",
		"--entrypoint", "/usr/local/bin/smithy-proxy", s.Image, "proxy", "serve", "--addr", ":"+strconv.Itoa(proxy.Port), "--allow", strings.Join(s.Allow, ","))
}

// Ensure creates the internal network and (re)starts the sidecar when it is missing or its config
// changed. env is the engine's env (DOCKER_HOST).
func (s ProxySidecar) Ensure(env []string) error {
	run := func(args ...string) (string, error) {
		c := exec.Command(s.Engine, args...)
		c.Env = env
		out, err := c.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	if err := os.MkdirAll(filepath.Join(s.Home, "proxy"), 0o700); err != nil {
		return err
	}
	// a missing file would make the engine create a directory in its place
	if f, err := os.OpenFile(filepath.Join(s.Home, "auth.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600); err == nil {
		f.WriteString("{}\n")
		f.Close()
	}
	for _, n := range [][]string{{ProxyNetwork, "--internal"}, {EgressNetwork}} {
		if _, err := run("network", "inspect", n[0]); err != nil {
			if out, err := run(append([]string{"network", "create"}, append(n[1:], n[0])...)...); err != nil {
				return fmt.Errorf("creating network %s: %v: %s", n[0], err, out)
			}
		}
	}
	label, err := run("inspect", "--format", `{{.State.Running}} {{index .Config.Labels "smithy.proxy"}}`, ProxyName)
	if err == nil && label == "true "+s.Hash() {
		return nil
	}
	if err == nil {
		run("rm", "-f", ProxyName)
	}
	a := s.RunArgv()
	if out, err := run(a[1:]...); err != nil {
		return fmt.Errorf("starting %s: %v: %s", ProxyName, err, out)
	}
	if out, err := run("network", "connect", ProxyNetwork, ProxyName); err != nil {
		run("rm", "-f", ProxyName)
		return fmt.Errorf("connecting %s to %s: %v: %s", ProxyName, ProxyNetwork, err, out)
	}
	return nil
}

var proxyVars = []string{"HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "https_proxy", "http_proxy", "no_proxy", "SMITHY_PROXY", "ALL_PROXY", "all_proxy"}

// ProxyEnv replaces env's proxy variables with the sidecar's.
func ProxyEnv(env []string) []string {
	var out []string
	for _, kv := range env {
		if k, _, _ := strings.Cut(kv, "="); !slices.Contains(proxyVars, k) {
			out = append(out, kv)
		}
	}
	for _, k := range []string{"HTTPS_PROXY", "HTTP_PROXY", "https_proxy", "http_proxy", "SMITHY_PROXY"} {
		out = append(out, k+"="+ProxyURL)
	}
	return append(out, "NO_PROXY="+ProxyName, "no_proxy="+ProxyName)
}

// startProxy ensures the sidecar for this machine's settings and moves box onto the internal network.
func startProxy(box *Container, env []string, p Probe) error {
	bin, err := proxyBinary(p)
	if err != nil {
		return err
	}
	extra, err := SandboxSetting("auto.sandbox_domains", "")
	if err != nil {
		return err
	}
	s := ProxySidecar{Engine: box.Engine, Image: box.Image, Bin: bin, Home: ghx.Home(), User: box.User, Allow: ProxyDomains(extra)}
	if err := s.Ensure(env); err != nil {
		return err
	}
	box.Network = ProxyNetwork
	return nil
}
