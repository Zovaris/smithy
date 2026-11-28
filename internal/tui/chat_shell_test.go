//go:build harness

package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/daemon"
)

func TestBangRunsShellInSessionFolderAndShowsOutput(t *testing.T) {
	f := &fakeSessions{}
	m := newChatTest(0)
	m.sess = f
	m.dir = "/work/proj"
	var gotDir, gotLine string
	m.shell = func(_ context.Context, dir, line string) daemon.ShellRun {
		gotDir, gotLine = dir, line
		return daemon.ShellRun{Lines: []daemon.ShellLine{{Text: "a.go"}, {Text: "b.go"}}, Took: 40 * time.Millisecond}
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
	for _, s := range []string{"$ ls -1", "ok", "40ms", "a.go", "b.go"} {
		if !strings.Contains(out, s) {
			t.Fatalf("%q missing:\n%s", s, out)
		}
	}
}

func TestBangShowsFailure(t *testing.T) {
	m := newChatTest(0)
	m.shell = func(context.Context, string, string) daemon.ShellRun {
		return daemon.ShellRun{Lines: []daemon.ShellLine{{Text: "nope", Err: true}}, Code: 2}
	}
	m = typeIn(m, "!false")
	m = cpress(m, ckey("enter"))
	out := screen(m)
	if !strings.Contains(out, "nope") || !strings.Contains(out, "exit 2") {
		t.Fatalf("failure not shown:\n%s", out)
	}
}

func TestBangAloneRunsNothing(t *testing.T) {
	m := newChatTest(0)
	ran := false
	m.shell = func(context.Context, string, string) daemon.ShellRun { ran = true; return daemon.ShellRun{} }
	m = typeIn(m, "!")
	m = cpress(m, ckey("enter"))
	if ran {
		t.Fatal("empty command ran")
	}
}

func TestShellBlockFoldsLongOutputAndExpandsOnClick(t *testing.T) {
	m := newChatTest(0)
	m.height = 60
	var ls []daemon.ShellLine
	for i := range 30 {
		ls = append(ls, daemon.ShellLine{Text: fmt.Sprintf("line-%02d", i)})
	}
	m.shell = func(context.Context, string, string) daemon.ShellRun { return daemon.ShellRun{Lines: ls} }
	m = typeIn(m, "!seq 30")
	m = cpress(m, ckey("enter"))
	out := screen(m)
	if !strings.Contains(out, "line-19") || strings.Contains(out, "line-20") || !strings.Contains(out, "10 more lines") {
		t.Fatalf("not folded after 20 lines:\n%s", out)
	}
	m = chatStep(m, tea.MouseClickMsg{X: 5, Y: rowOf(t, m, "$ seq 30"), Button: tea.MouseLeft})
	if out := screen(m); !strings.Contains(out, "line-29") {
		t.Fatalf("a click did not expand:\n%s", out)
	}
	m = cpress(m, ckey("enter"))
	if out := screen(m); strings.Contains(out, "line-29") {
		t.Fatalf("enter on the focused block did not fold it:\n%s", out)
	}
}

func TestShellBlockShowsFolderOnlyWhenNotTheSessions(t *testing.T) {
	m := newChatTest(0)
	m.height = 30
	m.dir = "/work/other"
	m.current = "s1"
	m.sessions = []chatSession{{id: "s1", dir: "/work/proj"}}
	m.shell = func(context.Context, string, string) daemon.ShellRun { return daemon.ShellRun{} }
	m = typeIn(m, "!true")
	m = cpress(m, ckey("enter"))
	if out := screen(m); !strings.Contains(out, "/work/other") {
		t.Fatalf("other folder not shown:\n%s", out)
	}
	m.sessions[0].dir = "/work/other"
	m.ver++
	if out := screen(m); strings.Contains(out, "/work/other") {
		t.Fatalf("session folder shown:\n%s", out)
	}
}

func TestRunShellUsesFolder(t *testing.T) {
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "here.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	r := daemon.RunShell(context.Background(), d, "ls")
	if r.Code != 0 || !strings.Contains(r.Output(), "here.txt") {
		t.Fatalf("run %+v", r)
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

func TestShellOutputStripsTerminalEscapes(t *testing.T) {
	m := newChatTest(0)
	m.shell = func(context.Context, string, string) daemon.ShellRun {
		return daemon.ShellRun{Lines: []daemon.ShellLine{{Text: "ok\x1b]52;c;ZXZpbA==\x07\x1b[2Jdone\r\x08"}}}
	}
	m = typeIn(m, "!x")
	m = cpress(m, ckey("enter"))
	last := m.msgs[len(m.msgs)-1].sh.run.Lines[0].Text
	if strings.ContainsAny(last, "\x1b\x07\x08\r") || !strings.Contains(last, "okdone") {
		t.Fatalf("got %q", last)
	}
}

func TestRunShellCapsOutput(t *testing.T) {
	r := daemon.RunShell(context.Background(), t.TempDir(), "head -c 5000000 /dev/zero | tr '\\0' a")
	if out := r.Output(); len(out) > 256<<10+100 || !r.Cut {
		t.Fatalf("output %d bytes", len(out))
	}
}

func TestDefaultPasteDirIsPrivateAndFresh(t *testing.T) {
	m := newChatTest(0)
	m.clip = func() ([]byte, error) { return []byte("png"), nil }
	m = cpress(m, tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	if len(m.images) != 1 {
		t.Fatal("no image")
	}
	d := filepath.Dir(m.images[0])
	defer os.RemoveAll(d)
	st, err := os.Stat(d)
	if err != nil || st.Mode().Perm() != 0o700 || filepath.Base(d) == "smithy-paste" {
		t.Fatalf("dir %s mode %v err %v", d, st.Mode(), err)
	}
}
