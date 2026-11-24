package agents

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestProxyBinary(t *testing.T) {
	if b, err := ProxyBinary("linux", "amd64", "", "/usr/bin/smithy", "/h/.smithy"); err != nil || b != "/usr/bin/smithy" {
		t.Errorf("linux: %q %v", b, err)
	}
	if b, _ := ProxyBinary("darwin", "arm64", "", "/x/smithy", "/h/.smithy"); b != "/h/.smithy/bin/smithy-linux-arm64" {
		t.Errorf("darwin: %q", b)
	}
	if b, _ := ProxyBinary("darwin", "arm64", "/opt/smithy-linux", "/x/smithy", "/h/.smithy"); b != "/opt/smithy-linux" {
		t.Errorf("setting: %q", b)
	}
	if _, err := ProxyBinary("darwin", "arm64", "relative/smithy", "/x/smithy", "/h"); err == nil {
		t.Error("relative setting accepted")
	}
}

func TestContainerCheckProxyNeedsLinuxBinary(t *testing.T) {
	setNetwork(t, "proxy")
	t.Setenv("SMITHY_HOME", t.TempDir())
	p := withCred(probe("darwin", "docker"))
	p.Exists = func(string) bool { return false }
	err := SandboxCheck("container", "claude", p)
	if err == nil || !strings.Contains(err.Error(), "GOOS=linux") {
		t.Fatalf("missing binary: %v", err)
	}
	p.Exists = func(f string) bool { return strings.HasSuffix(f, "smithy-linux-"+goarch()) }
	if err := SandboxCheck("container", "claude", p); err != nil {
		t.Errorf("binary present: %v", err)
	}
}

func TestProxySidecarArgv(t *testing.T) {
	s := ProxySidecar{Engine: "docker", Image: "img", Bin: "/b/smithy-linux", Home: "/h/.smithy", Allow: []string{"github.com", "x.dev"}}
	a := s.RunArgv()
	j := strings.Join(a, " ")
	for _, want := range []string{"docker run -d --rm --name smithy-proxy", "--network smithy-egress", "--cap-drop ALL", "--read-only",
		"-v /b/smithy-linux:/usr/local/bin/smithy-proxy:ro", "-v /h/.smithy:/smithy", "-e SMITHY_HOME=/smithy",
		"--entrypoint /usr/local/bin/smithy-proxy img proxy serve --addr :8877 --allow github.com,x.dev"} {
		if !strings.Contains(j, want) {
			t.Errorf("argv lacks %q: %s", want, j)
		}
	}
	if strings.Contains(j, "bridge") || strings.Contains(j, " -p ") {
		t.Errorf("sidecar on the shared bridge or published: %s", j)
	}
	if !slices.Contains(a, "smithy.proxy="+s.Hash()) {
		t.Errorf("no config label: %s", j)
	}
	if s2 := s; func() bool { s2.Allow = []string{"github.com"}; return s2.Hash() == s.Hash() }() {
		t.Error("hash ignores allow list")
	}
}

func TestProxyWorkerEnvAndNetwork(t *testing.T) {
	env := ProxyEnv([]string{"A=1", "HTTPS_PROXY=http://evil:1"})
	for _, want := range []string{"HTTPS_PROXY=http://smithy-proxy:8877", "HTTP_PROXY=http://smithy-proxy:8877", "NO_PROXY=smithy-proxy", "https_proxy=http://smithy-proxy:8877", "SMITHY_PROXY=http://smithy-proxy:8877"} {
		if !slices.Contains(env, want) {
			t.Errorf("env lacks %s: %v", want, env)
		}
	}
	if slices.Contains(env, "HTTPS_PROXY=http://evil:1") {
		t.Error("worker proxy setting kept")
	}
	_, names := ContainerEnv(env)
	if !slices.Contains(names, "SMITHY_PROXY") || !slices.Contains(names, "HTTPS_PROXY") {
		t.Errorf("names %v", names)
	}
}

func TestBuildProxyAllowsCurlToProxyOnly(t *testing.T) {
	t.Setenv("SMITHY_CONFIG", filepath.Join(t.TempDir(), "c.json"))
	o := Options{Profile: "auto", Proxy: true}
	s, err := Build("claude", o, "p", nil, "", func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	j := strings.Join(s.Argv, "\n")
	if strings.Contains(j, "Bash(curl *)") || !strings.Contains(j, "Bash(curl http://smithy-proxy:8877/s/*)") || !strings.Contains(j, "secret:<name>") {
		t.Errorf("proxy build: %s", j)
	}
	o.Proxy = false
	s, _ = Build("claude", o, "p", nil, "", func(string) string { return "" })
	if j := strings.Join(s.Argv, "\n"); !strings.Contains(j, "Bash(curl *)") || strings.Contains(j, "smithy-proxy") {
		t.Error("no proxy: curl must stay denied")
	}
}
