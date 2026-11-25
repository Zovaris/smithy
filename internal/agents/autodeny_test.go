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

func TestAutoDeniedBlocksApprovals(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	d := AutoDenied([]string{"/bin/smithy"})
	for _, b := range []string{"smithy", "/bin/smithy"} {
		for _, sub := range []string{"proxy approve *", "proxy deny *", "chat", "chat *", "attach", "attach *", "web", "web *", "tui", "tui *"} {
			if want := "Bash(" + b + " " + sub + ")"; !slices.Contains(d, want) {
				t.Errorf("AutoDenied lacks %s", want)
			}
		}
	}
}

func TestSecretDeniedCatchesGlobAndVariableForms(t *testing.T) {
	d := SecretDenied()
	for _, cmd := range []string{"cat ~/.s*/id_ed25519", "cat /Users/me/.s*/id_ed25519", "cat $HOME/.netrc", "cat ${HOME}/x", "cat ~/.smithy/auth.j*",
		"cat /Users/me/.smithy/auth*", "cat ~/.a*/credentials", "head ~/.config/g?/hosts.yml", "cat /Users/me/.[s]sh/id_rsa", "cat ~/.git-cred*"} {
		if !matchAny(d, "Bash", cmd) {
			t.Errorf("SecretDenied lets %q through", cmd)
		}
	}
	for _, ok := range []string{"go test ./...", "cat README.md", "git status"} {
		if matchAny(d, "Bash", ok) {
			t.Errorf("SecretDenied blocks %q", ok)
		}
	}
}

func TestNativeSettingsDenyDaemonToken(t *testing.T) {
	s := NativeSettings(SandboxPaths{Cwd: "/w/r", Home: "/h", SmithyHome: "/h/.smithy", Warden: "/h/.smithy/warden"}, nil, "linux", false)
	if !strings.Contains(s, `"denyRead":["/h/.smithy/auth.json","/h/.smithy/daemon.token"`) {
		t.Fatalf("daemon token readable: %s", s)
	}
}
