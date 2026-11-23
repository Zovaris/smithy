package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestKindOf(t *testing.T) {
	for key, want := range map[string]string{"auto.paused": "bool", "warden.max": "number", "auto.mode": "enum",
		"auto.sources": "list", "linear.team": "list", "git.branch": "text", "models.claude.cheap": "text", "linear.key_env": "text"} {
		if got, _ := KindOf(key); got != want {
			t.Errorf("KindOf(%s) = %s, want %s", key, got, want)
		}
	}
	if _, opts := KindOf("agent.effort"); strings.Join(opts, ",") != "low,medium" {
		t.Fatalf("agent.effort options = %v", opts)
	}
}

func TestNormalizeTyped(t *testing.T) {
	for _, c := range []struct{ key, in, out string }{
		{"auto.paused", "TRUE", "true"}, {"warden.max", " 7 ", "7"}, {"auto.mode", "all", "all"}, {"auto.sources", "github, linear", "github,linear"},
	} {
		got, _, err := Normalize(c.key, c.in)
		if err != nil || got != c.out {
			t.Errorf("Normalize(%s,%q) = %q %v", c.key, c.in, got, err)
		}
	}
	for _, c := range [][2]string{{"auto.paused", "maybe"}, {"warden.max", "-1"}, {"warden.max", "x"}, {"auto.mode", "some"}} {
		if _, _, err := Normalize(c[0], c[1]); err == nil {
			t.Errorf("Normalize(%s,%q) accepted", c[0], c[1])
		}
	}
}

func TestApplyAndRemove(t *testing.T) {
	e := setup(t)
	file, saved, _, err := Apply(false, "warden.max", "6")
	if err != nil || file != e.user || saved != "6" {
		t.Fatalf("Apply = %s %s %v", file, saved, err)
	}
	if v, src, _ := Effective("warden.max"); v != "6" || src != "user" {
		t.Fatalf("Effective = %s %s", v, src)
	}
	if _, _, _, err := Apply(false, "warden.max", "lots"); err == nil {
		t.Fatal("bad number saved")
	}
	if _, _, _, err := Apply(true, "warden.max", "6"); err == nil {
		t.Fatal("repo scope outside a repo")
	}
	gitInit(t, e.repo)
	file, _, _, err = Apply(true, "auto.paused", "true")
	if err != nil || !strings.HasSuffix(file, filepath.Join(".smithy", "config.json")) {
		t.Fatalf("repo Apply = %s %v", file, err)
	}
	if v, src, _ := Effective("auto.paused"); v != "true" || src != "repo" {
		t.Fatalf("after repo Apply = %s %s", v, src)
	}
	if _, found, err := Remove(true, "auto.paused"); !found || err != nil {
		t.Fatalf("Remove = %v %v", found, err)
	}
	if v, src, _ := Effective("auto.paused"); v != "false" || src != "default" {
		t.Fatalf("after Remove = %s %s", v, src)
	}
}

func TestSecret(t *testing.T) {
	if Secret("linear.key_env") || Secret("warden.max") || !Secret("notify.telegram.token") || !Secret("x.api_key") {
		t.Fatal("Secret misclassifies")
	}
}
