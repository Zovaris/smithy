//go:build harness

package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestBangRunsShellInSessionFolderAndShowsOutput(t *testing.T) {
	f := &fakeSessions{}
	m := newChatTest(0)
	m.sess = f
	m.dir = "/work/proj"
	var gotDir, gotLine string
	m.shell = func(_ context.Context, dir, line string) (string, error) {
		gotDir, gotLine = dir, line
		return "a.go\nb.go\n", nil
	}
	m = typeIn(m, "!ls -1")
	m = cpress(m, ckey("enter"))
	if gotDir != "/work/proj" || gotLine != "ls -1" {
		t.Fatalf("ran %q in %q", gotLine, gotDir)
	}
	if len(f.started) != 0 || len(f.sent) != 0 {
		t.Fatalf("shell line went to the agent: %v %v", f.started, f.sent)
	}
	out := screen(m)
	for _, s := range []string{"$ ls -1", "a.go", "b.go"} {
		if !strings.Contains(out, s) {
			t.Fatalf("%q missing:\n%s", s, out)
		}
	}
}

func TestBangShowsFailure(t *testing.T) {
	m := newChatTest(0)
	m.shell = func(context.Context, string, string) (string, error) {
		return "nope\n", errors.New("exit status 2")
	}
	m = typeIn(m, "!false")
	m = cpress(m, ckey("enter"))
	out := screen(m)
	if !strings.Contains(out, "nope") || !strings.Contains(out, "exit status 2") {
		t.Fatalf("failure not shown:\n%s", out)
	}
}

func TestBangAloneRunsNothing(t *testing.T) {
	m := newChatTest(0)
	ran := false
	m.shell = func(context.Context, string, string) (string, error) { ran = true; return "", nil }
	m = typeIn(m, "!")
	m = cpress(m, ckey("enter"))
	if ran {
		t.Fatal("empty command ran")
	}
}

func TestRunShellUsesFolder(t *testing.T) {
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "here.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runShell(context.Background(), d, "ls")
	if err != nil || !strings.Contains(out, "here.txt") {
		t.Fatalf("out %q err %v", out, err)
	}
}

func TestCtrlVAttachesClipboardImageAndSendsItsPath(t *testing.T) {
	f := &fakeSessions{}
	m := newChatTest(0)
	m.sess = f
	m.pasteDir = t.TempDir()
	m.clip = func() ([]byte, error) { return []byte("\x89PNG fake"), nil }
	m = cpress(m, tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	if len(m.images) != 1 {
		t.Fatalf("images %v", m.images)
	}
	if b, err := os.ReadFile(m.images[0]); err != nil || string(b) != "\x89PNG fake" {
		t.Fatalf("saved %q %v", b, err)
	}
	if !strings.Contains(screen(m), "[image 1]") {
		t.Fatalf("no chip:\n%s", screen(m))
	}
	m = typeIn(m, "what is this")
	m = cpress(m, ckey("enter"))
	if len(f.started) != 1 || !strings.Contains(f.started[0], "what is this") || !strings.Contains(f.started[0], m.pasteDir) {
		t.Fatalf("sent %q", f.started)
	}
	if len(m.images) != 0 || strings.Contains(screen(m), "[image 1]") {
		t.Fatal("attachment kept after send")
	}
}

func TestCtrlVWithoutImageSaysSo(t *testing.T) {
	m := newChatTest(0)
	m.pasteDir = t.TempDir()
	m.clip = func() ([]byte, error) { return nil, errNoImage }
	m = cpress(m, tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	if len(m.images) != 0 || !strings.Contains(screen(m), "no image in the clipboard") {
		t.Fatalf("images %v\n%s", m.images, screen(m))
	}
}

func TestEscDropsAttachments(t *testing.T) {
	m := newChatTest(0)
	m.images = []string{"/tmp/x.png"}
	m = cpress(m, ckey("esc"))
	if len(m.images) != 0 {
		t.Fatal("esc kept the attachment")
	}
}

func TestParseOsascriptPNG(t *testing.T) {
	b, err := parseOsaPNG("«data PNGf89504E47»\n")
	if err != nil || string(b) != "\x89PNG" {
		t.Fatalf("%q %v", b, err)
	}
	if _, err := parseOsaPNG(""); err == nil {
		t.Fatal("empty parsed")
	}
}
