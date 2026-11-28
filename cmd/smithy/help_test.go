package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the help golden files")

func build() string {
	if harnessBuild {
		return "harness"
	}
	return "skills"
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "help", name+".golden")
	if *update {
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test -run TestHelp -update, both builds)", err)
	}
	if got != string(want) {
		t.Errorf("%s differs from %s:\n%s", name, path, got)
	}
}

func fits80(t *testing.T, name, s string) {
	t.Helper()
	for _, l := range strings.Split(s, "\n") {
		if len([]rune(l)) > 80 {
			t.Errorf("%s: line over 80 columns: %q", name, l)
		}
	}
}

func TestHelpGoldenFiles(t *testing.T) {
	repo, env := sandbox(t)
	cases := map[string][]string{
		"top-" + build():     {"help"},
		"top-all-" + build(): {"help", "--all"},
		"cmd-doctor":         {"help", "doctor"},
		"cmd-receipt":        {"receipt", "--help"},
		"cmd-config-get":     {"config", "get", "--help"},
		"cmd-auth-set":       {"help", "auth", "set"},
	}
	for name, args := range cases {
		out, errOut, code := run(t, repo, env, args...)
		if code != 0 || errOut != "" {
			t.Errorf("%v: exit %d, stderr %q", args, code, errOut)
		}
		golden(t, name, out)
		fits80(t, name, out)
	}
	errs := map[string][]string{
		"err-unknown-command": {"stauts"},
		"err-unknown-flag":    {"scan", "--bsae", "main"},
		"err-missing-value":   {"scan", "--base"},
		"err-help-unknown":    {"help", "recipt"},
	}
	for name, args := range errs {
		out, errOut, code := run(t, repo, env, args...)
		if code != 2 || out != "" {
			t.Errorf("%v: exit %d, stdout %q", args, code, out)
		}
		golden(t, name, errOut)
	}
}

func TestTopHelpSameForEveryEntry(t *testing.T) {
	repo, env := sandbox(t)
	want, _, _ := run(t, repo, env, "help")
	for _, a := range []string{"--help", "-h"} {
		if out, _, code := run(t, repo, env, a); code != 0 || out != want {
			t.Errorf("smithy %s: exit %d, differs from smithy help", a, code)
		}
	}
	if strings.Contains(want, "smithy brief") {
		t.Error("default help lists plumbing (brief); it belongs under --all")
	}
}

func TestEveryCommandHasFullHelp(t *testing.T) {
	for name, c := range commands {
		d, ok := helpDocs[name]
		if !ok {
			t.Errorf("%s: no helpDocs entry", name)
			continue
		}
		if summary(name, c) == "" {
			t.Errorf("%s: no summary", name)
		}
		if strings.TrimSpace(c.usage) == "" {
			t.Errorf("%s: no usage", name)
		}
		if len(d.examples) == 0 {
			t.Errorf("%s: no example", name)
		}
		for _, e := range d.examples {
			if !strings.HasPrefix(e, "smithy "+name) {
				t.Errorf("%s: example %q is not a %s command", name, e, name)
			}
		}
		for _, s := range d.see {
			if _, ok := commands[s]; !ok {
				t.Errorf("%s: see also %q is not a command", name, s)
			}
		}
		var b bytes.Buffer
		commandHelp(&b, name, nil)
		fits80(t, name, b.String())
	}
}

func TestSuggest(t *testing.T) {
	for in, want := range map[string]string{"stauts": "status", "recipt": "receipt", "zzzzzzzz": ""} {
		if got := suggest(in, []string{"status", "receipt", "scan"}); got != want {
			t.Errorf("suggest(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWrapLineKeepsTheUsageTableColumn(t *testing.T) {
	got := wrapLine("  run       run it now (through the daemon when one runs, else here; ask steps need the daemon);")
	if len(got) != 2 || !strings.HasPrefix(got[0], "  run       run it now") || !strings.HasPrefix(got[1], strings.Repeat(" ", 12)) || got[1][12] == ' ' {
		t.Fatalf("table row lost its column:\n%s", strings.Join(got, "\n"))
	}
	for _, l := range got {
		if len([]rune(l)) > width {
			t.Fatalf("too wide: %q", l)
		}
	}
}
