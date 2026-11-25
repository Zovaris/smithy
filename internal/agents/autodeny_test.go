package agents

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestAutoDeniedGuardsCardsAndDaemonToken(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	wh := t.TempDir()
	t.Setenv("WARDEN_HOME", wh)
	d := AutoDenied([]string{"/bin/smithy"})
	p := filepath.ToSlash(wh)
	for _, want := range []string{"Edit(/" + p + "/**/*.json)", "Edit(~/.smithy/warden/**/*.json)", "Read(~/.smithy/daemon.token)", "Bash(*daemon.token*)"} {
		if !slices.Contains(d, want) {
			t.Errorf("AutoDenied lacks %s", want)
		}
	}
}

func TestNativeSettingsDenyDaemonToken(t *testing.T) {
	s := NativeSettings(SandboxPaths{Cwd: "/w/r", Home: "/h", SmithyHome: "/h/.smithy", Warden: "/h/.smithy/warden"}, nil, "linux", false)
	if !strings.Contains(s, `"denyRead":["/h/.smithy/auth.json","/h/.smithy/daemon.token"`) {
		t.Fatalf("daemon token readable: %s", s)
	}
}
