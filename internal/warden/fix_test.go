package warden

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/api"
	"github.com/Zovaris/smithy/internal/daemon"
)

func fixEnv(t *testing.T, tty bool, answers ...string) doctorEnv {
	t.Helper()
	d := newDoctor(t, "git", "gh")
	t.Setenv("WARDEN_HARNESS", "claude-code")
	writeFile(t, filepath.Join(d.home, ".claude", "settings.json"), `{"model":"x","permissions":{"allow":["Bash(ls:*)"]}}`)
	os.MkdirAll(filepath.Join(d.root, "smithy"), 0o700)
	daemon.WriteInfo(api.Info{PID: 999999, Addr: "127.0.0.1:7477", Started: time.Now()})
	os.WriteFile(daemon.TokenFile(), []byte("x\n"), 0o644)
	oldTTY, oldAsk := stdinTTY, askFix
	stdinTTY = func() bool { return tty }
	askFix = func(string) bool {
		if len(answers) == 0 {
			t.Fatal("asked with no answer left")
		}
		a := answers[0]
		answers = answers[1:]
		return a == "y"
	}
	t.Cleanup(func() { stdinTTY, askFix = oldTTY, oldAsk })
	return d
}

func allowList(t *testing.T, d doctorEnv) ([]string, map[string]any) {
	b, _ := os.ReadFile(filepath.Join(d.home, ".claude", "settings.json"))
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, v := range m["permissions"].(map[string]any)["allow"].([]any) {
		out = append(out, v.(string))
	}
	return out, m
}

func tokenMode() os.FileMode {
	fi, _ := os.Stat(daemon.TokenFile())
	return fi.Mode().Perm()
}

func TestDoctorFixYes(t *testing.T) {
	d := fixEnv(t, false)
	o, _ := doctor(t, "--fix", "--yes")
	for _, want := range []string{"fix   remove stale " + daemon.InfoFile(), "fix   chmod 600 " + daemon.TokenFile(), `fix   add "Bash(smithy pr merge:*)" to permissions.allow in ` + filepath.Join(d.home, ".claude", "settings.json"), "changed: "} {
		if !strings.Contains(o, want) {
			t.Errorf("missing %q in:\n%s", want, o)
		}
	}
	if _, err := os.Stat(daemon.InfoFile()); !os.IsNotExist(err) {
		t.Error("stale info file kept")
	}
	if tokenMode() != 0o600 {
		t.Errorf("token mode %v", tokenMode())
	}
	allow, m := allowList(t, d)
	if strings.Join(allow, ",") != "Bash(ls:*)" || m["model"] != "x" {
		t.Errorf("--yes granted a permission: %v %v", allow, m)
	}
	if !strings.Contains(o, "skipped (grants a permission, answer it without --yes): ") {
		t.Errorf("grant not reported as skipped:\n%s", o)
	}
	o, _ = doctor(t, "--fix", "--yes")
	if strings.Contains(o, "fix   remove") || strings.Contains(o, "fix   chmod") {
		t.Errorf("second run still fixes:\n%s", o)
	}
}

func TestDoctorFixAsksEach(t *testing.T) {
	d := fixEnv(t, true, "n", "y", "n")
	o, _ := doctor(t, "--fix")
	if _, err := os.Stat(daemon.InfoFile()); err != nil {
		t.Error("declined fix applied")
	}
	if tokenMode() != 0o600 {
		t.Error("accepted fix not applied")
	}
	if allow, _ := allowList(t, d); len(allow) != 1 {
		t.Errorf("declined settings fix applied: %v", allow)
	}
	if !strings.Contains(o, "skipped") {
		t.Errorf("no skipped line:\n%s", o)
	}
}

func TestDoctorFixNoTTYAppliesNone(t *testing.T) {
	d := fixEnv(t, false)
	o, _ := doctor(t, "--fix")
	if tokenMode() != 0o644 {
		t.Error("applied without tty or --yes")
	}
	if _, err := os.Stat(daemon.InfoFile()); err != nil {
		t.Error("removed without tty or --yes")
	}
	if allow, _ := allowList(t, d); len(allow) != 1 {
		t.Error("settings changed")
	}
	if !strings.Contains(o, "--yes") {
		t.Errorf("no hint:\n%s", o)
	}
}

func TestDoctorNoFixFlagChangesNothing(t *testing.T) {
	fixEnv(t, true)
	o, _ := doctor(t)
	if tokenMode() != 0o644 || strings.Contains(o, "fix   ") {
		t.Errorf("plain doctor fixed:\n%s", o)
	}
}

func TestDoctorFixBadSettingsUntouched(t *testing.T) {
	d := fixEnv(t, false)
	p := filepath.Join(d.home, ".claude", "settings.json")
	writeFile(t, p, "{not json")
	o, _ := doctor(t, "--fix", "--yes")
	if b, _ := os.ReadFile(p); string(b) != "{not json" {
		t.Errorf("rewrote invalid settings: %q", b)
	}
	if strings.Contains(o, "fix   add") {
		t.Errorf("offered a fix on invalid json:\n%s", o)
	}
}

func TestDoctorUnknownFlag(t *testing.T) {
	newDoctor(t, "git", "gh")
	if _, c := doctor(t, "--fxi"); c != 2 {
		t.Errorf("code %d", c)
	}
}

func TestDoctorFixSkipsSymlinkSettings(t *testing.T) {
	d := fixEnv(t, false)
	p := filepath.Join(d.home, ".claude", "settings.json")
	real := filepath.Join(d.root, "dotfiles.json")
	writeFile(t, real, `{}`)
	os.Remove(p)
	os.Symlink(real, p)
	o, _ := doctor(t, "--fix", "--yes")
	if fi, _ := os.Lstat(p); fi.Mode()&os.ModeSymlink == 0 || strings.Contains(o, "fix   add") {
		t.Errorf("symlinked settings replaced or offered:\n%s", o)
	}
}

func TestDoctorFixTokenOnlyWhenLoose(t *testing.T) {
	fixEnv(t, false)
	os.WriteFile(daemon.TokenFile(), nil, 0o600)
	os.Chmod(daemon.TokenFile(), 0o600)
	if o, _ := doctor(t, "--fix", "--yes"); strings.Contains(o, "fix   chmod") {
		t.Errorf("chmod offered for an empty 0600 token:\n%s", o)
	}
}

func TestDoctorFixKeepsRevivedDaemonInfo(t *testing.T) {
	fixEnv(t, true, "y", "n", "n")
	askOld := askFix
	askFix = func(c string) bool {
		if strings.HasPrefix(c, "remove stale") {
			daemon.WriteInfo(api.Info{PID: os.Getpid(), Addr: "127.0.0.1:7477", Started: time.Now()})
		}
		return askOld(c)
	}
	doctor(t, "--fix")
	if _, err := os.Stat(daemon.InfoFile()); err != nil {
		t.Error("removed a live daemon's info file")
	}
}
