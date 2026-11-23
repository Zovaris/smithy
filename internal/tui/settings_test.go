package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func settingsAt(t *testing.T, key string) Model {
	t.Helper()
	m := demo(t, 160, 48)
	m, cmd := press(m, "9")
	nm, _ := m.Update(cmd())
	m = nm.(Model)
	for i, s := range m.sys.Settings {
		if s.Key == key {
			m.sel[vSystem] = i
			return m
		}
	}
	t.Fatalf("no setting %s", key)
	return m
}

func TestSettingsEdit(t *testing.T) {
	m := settingsAt(t, "warden.max")
	var got []string
	m.setter = func(repo, unset bool, key, val string) (string, error) {
		got = append(got, strings.TrimSpace(fmt.Sprintln(repo, unset, key, val)))
		return "saved " + key, nil
	}
	m, _ = press(m, "enter")
	if m.edit == nil || m.edit.kind != "number" || !strings.Contains(ansi.Strip(m.render()), "Edit warden.max") {
		t.Fatal("enter did not open the editor")
	}
	m.edit.in.SetValue("lots")
	m, _ = press(m, "enter")
	if m.edit.confirm || !strings.Contains(m.edit.err, "whole number") {
		t.Fatalf("bad value passed validation: %q", m.edit.err)
	}
	m.edit.in.SetValue("8")
	m, _ = press(m, "enter")
	out := ansi.Strip(m.render())
	if !m.edit.confirm || !strings.Contains(out, "5 → 8") || !strings.Contains(out, "~/.smithy/config.json") {
		t.Fatalf("confirm step missing old → new and scope:\n%s", out)
	}
	m, _ = press(m, "tab")
	if !m.edit.repo {
		t.Fatal("tab did not switch scope")
	}
	m, cmd := press(m, "enter")
	if m.edit != nil || cmd == nil {
		t.Fatal("enter did not save")
	}
	nm, cmd := m.Update(cmd())
	m = nm.(Model)
	if strings.Join(got, "|") != "true false warden.max 8" || m.sys != nil || cmd == nil {
		t.Fatalf("write = %v", got)
	}
}

func TestSettingsEnumAndUnset(t *testing.T) {
	m := settingsAt(t, "auto.mode")
	var got []string
	m.setter = func(repo, unset bool, key, val string) (string, error) {
		got = append(got, strings.TrimSpace(fmt.Sprintln(repo, unset, key)))
		return "", nil
	}
	m, _ = press(m, "enter")
	m, _ = press(m, "right")
	m, _ = press(m, "enter")
	if m.edit.val != "all" {
		t.Fatalf("enum = %q", m.edit.val)
	}
	m, _ = press(m, "esc")
	m, _ = press(m, "esc")
	if m.edit != nil {
		t.Fatal("esc did not close")
	}
	if m, _ = press(m, "u"); m.edit != nil || !strings.Contains(m.status, "default") {
		t.Fatal("u on a default opened the editor")
	}
	m = settingsAt(t, "warden.merge")
	m.setter = func(repo, unset bool, key, val string) (string, error) {
		got = append(got, strings.TrimSpace(fmt.Sprintln(repo, unset, key)))
		return "", nil
	}
	m, _ = press(m, "u")
	if m.edit == nil || !m.edit.unset || !strings.Contains(ansi.Strip(m.render()), "Unset warden.merge?") {
		t.Fatal("u did not ask to unset")
	}
	m, cmd := press(m, "enter")
	m.Update(cmd())
	if len(got) == 0 || got[len(got)-1] != "false true warden.merge" {
		t.Fatalf("unset = %v", got)
	}
}

func TestSettingsDemoOnlySays(t *testing.T) {
	m := settingsAt(t, "auto.paused")
	m.setter = nil
	m, _ = press(m, "enter")
	m, _ = press(m, "right")
	m, _ = press(m, "enter")
	m, cmd := press(m, "enter")
	if cmd != nil || !strings.Contains(m.toast, "demo: would run smithy config set auto.paused true") {
		t.Fatalf("demo save: %q", m.toast)
	}
}

func TestSettingsSecretsLocked(t *testing.T) {
	rows := settingRows("claude", func(string) (string, string, error) { return "LINEAR_API_KEY", "user", nil })
	for _, r := range rows {
		if r.Key == "linear.key_env" && (r.Value != "LINEAR_API_KEY" || r.Secret) {
			t.Fatal("key_env names must show and stay editable")
		}
	}
	m := settingsAt(t, "warden.max")
	m.sys.Settings = []Setting{{Key: "x.token", Value: "****", Src: "user", Secret: true}}
	m.sel[vSystem] = 0
	if m, _ = press(m, "enter"); m.edit != nil || !strings.Contains(m.status, "smithy auth") {
		t.Fatal("secret opened the editor")
	}
}

func TestSettingsWritePath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SMITHY_CONFIG", filepath.Join(dir, "config.json"))
	if _, err := writeSetting(false, false, "warden.max", "x"); err == nil {
		t.Fatal("invalid value written")
	}
	if msg, err := writeSetting(false, false, "warden.max", "3"); err != nil || !strings.Contains(msg, "saved warden.max = 3") {
		t.Fatalf("write = %q %v", msg, err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "config.json"))
	if !strings.Contains(string(b), `"max": 3`) {
		t.Fatalf("file = %s", b)
	}
	if msg, err := writeSetting(false, true, "warden.max", ""); err != nil || !strings.Contains(msg, "removed") {
		t.Fatalf("unset = %q %v", msg, err)
	}
}

// TestSettingsFrames prints the settings list, edit modal and confirm as text (go test -run Frames -v).
func TestSettingsFrames(t *testing.T) {
	if !testing.Verbose() {
		t.Skip()
	}
	m := settingsAt(t, "warden.max")
	t.Log("\n" + ansi.Strip(m.render()))
	m, _ = press(m, "enter")
	m.edit.in.SetValue("8")
	t.Log("\n" + ansi.Strip(m.render()))
	m, _ = press(m, "enter")
	t.Log("\n" + ansi.Strip(m.render()))
}
