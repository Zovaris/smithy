//go:build harness

package chat

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/api"
	"github.com/Zovaris/smithy/internal/daemon"
	"github.com/charmbracelet/x/ansi"
)

// fakeSessions records what the chat asked of the sessions API.
type fakeSessions struct {
	started []string
	sent    [][2]string
	stopped []string
}

func (f *fakeSessions) StartSession(_ context.Context, agent, dir, msg string) (api.Session, error) {
	f.started = append(f.started, msg)
	return api.Session{ID: "s-1", Agent: "claude", Status: daemon.StatusWorking}, nil
}

func (f *fakeSessions) SendSession(_ context.Context, id, text string) error {
	f.sent = append(f.sent, [2]string{id, text})
	return nil
}

func (f *fakeSessions) StopSession(_ context.Context, id string) error {
	f.stopped = append(f.stopped, id)
	return nil
}

// press applies msg and feeds back what its command leads to.
func cpress(m chatView, msg tea.Msg) chatView {
	n, cmd := m.Update(msg)
	return run(n.(chatView), cmd)
}

func TestSlashPopupListsEveryCommandWithItsDescription(t *testing.T) {
	m := newChatTest(0)
	m.width, m.height = 160, 48
	for _, c := range []string{"/new", "/sessions", "/agent", "/model", "/skills", "/connect", "/web", "/remote", "/stop", "/mode", "/help", "/quit"} {
		m.input.SetValue(c[:3])
		if out := screen(m); !strings.Contains(out, c) {
			t.Errorf("popup lacks %s:\n%s", c, out)
		}
	}
	for _, c := range slashCommands {
		if c.desc == "" {
			t.Errorf("%s has no description", c.name)
		}
	}
	m.input.SetValue("")
	m = typeIn(m, "/mo")
	out := screen(m)
	if !strings.Contains(out, "/mode") || !strings.Contains(out, "/model") || strings.Contains(out, "/quit") {
		t.Fatalf("filter /mo:\n%s", out)
	}
	m = chatStep(m, ckey("down"))
	m = chatStep(m, ckey("tab"))
	if m.input.Value() != "/mode " {
		t.Fatalf("tab completes the selected command, got %q", m.input.Value())
	}
}

func TestSlashPopupEnterRunsSelected(t *testing.T) {
	m := newChatTest(0)
	m = typeIn(m, "/sess")
	m = chatStep(m, ckey("enter"))
	if m.overlay != overlayAgents {
		t.Fatalf("/sessions did not open the sessions overlay: %v", m.overlay)
	}
}

func TestAtPopupCompletesFiles(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"main.go", "internal/pool/detect.go", "README.md"} {
		p := filepath.Join(dir, f)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("x"), 0o644)
	}
	m := newChatTest(0)
	m.dir = dir
	m = typeIn(m, "fix @det")
	out := screen(m)
	if !strings.Contains(out, "internal/pool/detect.go") || strings.Contains(out, "README.md") {
		t.Fatalf("@ popup:\n%s", out)
	}
	m = chatStep(m, ckey("tab"))
	if m.input.Value() != "fix @internal/pool/detect.go " {
		t.Fatalf("got %q", m.input.Value())
	}
}

func TestDaemonNoiseGoesToStatusLineNotTranscript(t *testing.T) {
	m := newChatTest(0)
	m.width = 120
	m = chatStep(m, ev(1, "daemon_up", nil))
	m = chatStep(m, evNow(2, "log", map[string]string{"line": "serve: poll: github: To get started with GitHub CLI"}))
	if len(m.msgs) != 0 {
		t.Fatalf("noise reached the transcript: %+v", m.msgs[0])
	}
	if !strings.Contains(ansi.Strip(m.statusLine(120)), "poll: github") {
		t.Fatalf("status line: %q", ansi.Strip(m.statusLine(120)))
	}
	m = chatStep(m, ckey("/"))
	m = typeIn(m, "log")
	m = chatStep(m, ckey("enter"))
	if !strings.Contains(screen(m), "daemon up") {
		t.Fatalf("/log does not show the daemon log:\n%s", screen(m))
	}
}

func TestStatusLineSaysWhereTheDaemonRuns(t *testing.T) {
	m := newChatTest(0)
	m.where = whereBackground
	if s := ansi.Strip(m.statusLine(100)); !strings.Contains(s, "background") || !strings.Contains(s, "smithy stop") {
		t.Fatalf("status line: %q", s)
	}
	m.where = whereStandalone
	if s := ansi.Strip(m.statusLine(100)); !strings.Contains(s, "standalone") || strings.Contains(s, "smithy stop") {
		t.Fatalf("standalone: %q", s)
	}
}

func TestFirstMessageStartsSessionThenSends(t *testing.T) {
	f := &fakeSessions{}
	m := newChatTest(0)
	m.sess = f
	m = typeIn(m, "hello")
	m = cpress(m, ckey("enter"))
	if len(f.started) != 1 || f.started[0] != "hello" || m.current != "s-1" {
		t.Fatalf("started %v current %q", f.started, m.current)
	}
	m.track("s-1", "done")
	m = typeIn(m, "again")
	m = cpress(m, ckey("enter"))
	if len(f.sent) != 1 || f.sent[0] != [2]string{"s-1", "again"} {
		t.Fatalf("sent %v", f.sent)
	}
	m = typeIn(m, "/stop")
	m = cpress(m, ckey("enter"))
	if len(f.stopped) != 1 || f.stopped[0] != "s-1" {
		t.Fatalf("stopped %v", f.stopped)
	}
	m = typeIn(m, "/new")
	m = cpress(m, ckey("enter"))
	if m.current != "" || len(m.msgs) != 0 {
		t.Fatalf("/new kept %q with %d msgs", m.current, len(m.msgs))
	}
}

func TestSessionEventsRenderInTheirSession(t *testing.T) {
	m := newChatTest(0)
	m.current = "s-1"
	m = chatStep(m, sev(1, daemon.KindSessionMessage, "s-1", map[string]string{"role": "user", "text": "fix it"}))
	m = chatStep(m, sev(2, daemon.KindSessionTool, "s-1", daemon.ToolEvent{Name: "Bash", Args: []byte(`{"command":"go test ./..."}`), Output: "ok"}))
	m = chatStep(m, sev(3, daemon.KindSessionMessage, "s-1", map[string]string{"role": "agent", "text": "Done: fixed."}))
	m = chatStep(m, sev(4, daemon.KindSessionUsage, "s-1", map[string]any{"input_tokens": 10, "cost_usd": 0.42}))
	m = chatStep(m, sev(5, daemon.KindSessionMessage, "s-2", map[string]string{"role": "user", "text": "other"}))
	out := screen(m)
	for _, want := range []string{"fix it", "+ 1 command", "Done: fixed.", "$0.42"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "other") {
		t.Fatalf("another session leaked into this one:\n%s", out)
	}
}

func TestHelpKeyOpensHelp(t *testing.T) {
	m := newChatTest(0)
	m.width, m.height = 120, 40
	m = chatStep(chatStep(m, ckey("?")), ckey("enter"))
	if m.overlay != overlayHelp || !strings.Contains(screen(m), "/sessions") {
		t.Fatalf("? help:\n%s", screen(m))
	}
	m = chatStep(m, ckey("esc"))
	if m.overlay != overlayNone {
		t.Fatal("esc does not close help")
	}
}

func TestHeaderIsTheAnvilBand(t *testing.T) {
	m := newChatTest(0)
	m.width, m.height = 160, 48
	lines := strings.Split(screen(m), "\n")
	if !strings.Contains(strings.Join(lines[:headRows], ""), "smithy") {
		t.Fatalf("no header:\n%s", strings.Join(lines[:3], "\n"))
	}
	if m.animating() {
		t.Fatal("idle chat animates")
	}
	m.track("s-1", daemon.StatusWorking)
	if !m.animating() {
		t.Fatal("running session does not animate")
	}
}

func TestPaletteIsAFloatingBox(t *testing.T) {
	m := newChatScene()
	m.width, m.height = 160, 48
	m = chatStep(m, ckey("ctrl+k"))
	out := screen(m)
	if !strings.Contains(out, "results") || !strings.Contains(out, "Waiting on you") || !strings.Contains(out, "tab next group") {
		t.Fatalf("palette:\n%s", out)
	}
	row := strings.Split(out, "\n")[rowOf(t, m, "Waiting on you")]
	if strings.Index(row, "Waiting") < 20 {
		t.Fatalf("palette is not centered: %q", row)
	}
}

func TestRawAgentJSONStaysOutOfTheTranscript(t *testing.T) {
	m := newChatTest(0)
	m.current = "s-1"
	m = chatStep(m, sev(1, daemon.KindSessionText, "s-1", map[string]string{"text": `{"type":"result","total_cost_usd":0.1}`}))
	m = chatStep(m, sev(2, daemon.KindSessionDiff, "s-1", map[string]any{"stat": " main.go | 2 +-", "patch": "diff --git a/main.go b/main.go\nindex 1..2 100644\n--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n-a\n+b\n"}))
	if len(m.msgs) != 1 || m.msgs[0].tool == nil || m.msgs[0].tool.Stat != "+1 −1" {
		t.Fatalf("msgs %+v", m.msgs)
	}
	for _, l := range m.msgs[0].tool.Lines {
		if strings.HasPrefix(l, "diff --git") || strings.HasPrefix(l, "index ") {
			t.Fatalf("git header kept: %q", l)
		}
	}
}

func TestToolBlockCountsLinesAndShortensPaths(t *testing.T) {
	m := newChatTest(0)
	m.current, m.dir = "s-1", "/work/proj"
	m = chatStep(m, sev(1, daemon.KindSessionTool, "s-1", daemon.ToolEvent{Name: "Read", Args: []byte(`{"file_path":"/work/proj/main.go"}`), Output: "1\tpackage main\n2\n3\tfunc add() {}\n", Took: "0.3s"}))
	tl := m.msgs[0].tool
	if tl.Arg != "main.go" || tl.Stat != "3 lines" || tl.Took != "0.3s" || len(tl.Lines) != 3 {
		t.Fatalf("%+v", tl)
	}
}
