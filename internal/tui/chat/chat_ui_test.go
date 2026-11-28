//go:build harness

package chat

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/api"
	"github.com/Zovaris/smithy/internal/daemon"
	"github.com/Zovaris/smithy/internal/tui/kit"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func write(t *testing.T, path, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func sev(seq int64, kind, session string, data any) api.ClientMsg {
	msg := ev(seq, kind, data)
	msg.Event.Session = session
	return msg
}

// newChatScene is the Chat mockups' turn: a prompt, thinking, four tools, the answer and the turn
// stats, with one decision waiting on the user.
func newChatScene() chatView {
	m := newChatView(nil, "http://127.0.0.1:7477", kit.NewTheme(true, colorprofile.Ascii, true))
	m.width, m.height = 100, 32
	m.clock = func() time.Time { return time.Date(2026, 11, 25, 9, 7, 0, 0, time.Local) }
	m.setPending([]api.Decision{{ID: "d1", Question: "Merge #142 into release/v0.17?", Options: []string{"merge", "later"}}})
	m.current = "TK-6"
	m.push(chatMsg{who: "you", text: "Add usage-limit detection for Pi (TK-6)."})
	steps := []api.ClientMsg{
		sev(1, "thinking", "TK-6", map[string]string{"text": "Pi's limit errors are typed classes; match their names, not prose."}),
		sev(2, daemon.KindSessionText, "TK-6", map[string]string{"text": "Pi prints its limit errors from packages/ai/src/errors.ts. I'll match those near the end of a failed run."}),
		sev(3, "tool", "TK-6", chatTool{Name: "Read", Arg: "internal/pool/detect.go", Stat: "134 lines", Took: "0.1s", Status: "ok", Lines: []string{"134 lines read"}}),
		sev(4, "tool", "TK-6", chatTool{Name: "Grep", Arg: `"LimitError" in pi`, Stat: "2 matches", Took: "0.4s", Status: "ok", Lines: []string{"packages/ai/src/errors.ts:41  class GoUsageLimitError"}}),
		sev(5, "tool", "TK-6", chatTool{Name: "Edit", Arg: "internal/pool/detect.go", Stat: "+6 −1", Took: "0.2s", Status: "ok", Lines: []string{"+\tpiLimitRe = regexp.MustCompile(`GoUsageLimitError`)"}}),
		sev(6, "tool", "TK-6", chatTool{Name: "Bash", Arg: "go test ./internal/pool", Stat: "exit 0", Took: "3.8s", Status: "ok", Lines: []string{"ok  github.com/Zovaris/smithy/internal/pool 0.61s"}}),
		sev(7, daemon.KindSessionMessage, "TK-6", map[string]string{"role": "agent", "text": "Done: a Pi limit now marks the account limited until its reset."}),
		sev(8, "turn", "TK-6", map[string]string{"text": "turn · 4 tools · in 18.2k · out 1.1k · $0.06 · 4.5s"}),
		sev(9, "running", "TK-29", nil),
		sev(10, "failed", "TES-9", nil),
	}
	for _, s := range steps {
		s.Event.Time = s.Event.Time.UTC()
		m = chatStep(m, s)
	}
	for _, c := range m.msgs {
		c.at, c.lines = "", nil
	}
	m.noise = nil
	return m
}

func ckey(s string) tea.KeyPressMsg {
	switch s {
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "ctrl+k":
		return tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl}
	case "ctrl+o":
		return tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl}
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}

func typeIn(m chatView, s string) chatView {
	for _, r := range s {
		m = chatStep(m, ckey(string(r)))
	}
	return m
}

func screen(m chatView) string { return ansi.Strip(m.View().Content) }

// rowOf is the screen row whose text contains s.
func rowOf(t *testing.T, m chatView, s string) int {
	t.Helper()
	for y, l := range strings.Split(screen(m), "\n") {
		if strings.Contains(l, s) {
			return y
		}
	}
	t.Fatalf("%q not on screen:\n%s", s, screen(m))
	return -1
}

func TestChatModesCycleWithCtrlO(t *testing.T) {
	m := newChatScene()
	if m.mode != modeMinimal {
		t.Fatalf("starts in %v", m.mode)
	}
	out := screen(m)
	if !strings.Contains(out, "✓  + 1 command, 1 edit, 2 reads") || strings.Contains(out, "134 lines read") || strings.Contains(out, "typed classes") {
		t.Fatalf("minimal shows tool details:\n%s", out)
	}
	m = chatStep(m, ckey("ctrl+o"))
	out = screen(m)
	if m.mode != modeExpandable || !strings.Contains(out, "✓  Grep  \"LimitError\" in pi") || strings.Contains(out, "134 lines read") {
		t.Fatalf("expandable:\n%s", out)
	}
	m = chatStep(m, ckey("ctrl+o"))
	m = chatStep(m, tea.WindowSizeMsg{Width: m.width, Height: 40})
	out = screen(m)
	if m.mode != modeVerbose || !strings.Contains(out, "134 lines read") || !strings.Contains(out, "typed classes") || !strings.Contains(out, "turn · 4 tools") {
		t.Fatalf("verbose:\n%s", out)
	}
	if m = chatStep(m, ckey("ctrl+o")); m.mode != modeMinimal {
		t.Fatal("v does not wrap back to minimal")
	}
	m = typeIn(m, "av")
	if m.mode != modeMinimal || m.input.Value() != "av" {
		t.Fatal("v while typing must type, not switch mode")
	}
}

func TestChatClickExpandsToolBlock(t *testing.T) {
	m := chatStep(newChatScene(), ckey("ctrl+o"))
	m = chatStep(m, tea.MouseClickMsg{X: 5, Y: rowOf(t, m, "Grep"), Button: tea.MouseLeft})
	if !strings.Contains(screen(m), "class GoUsageLimitError") || m.input.Focused() {
		t.Fatalf("click did not open and focus the block:\n%s", screen(m))
	}
	m = chatStep(m, tea.MouseClickMsg{X: 5, Y: rowOf(t, m, "Grep"), Button: tea.MouseLeft})
	if strings.Contains(screen(m), "class GoUsageLimitError") {
		t.Fatal("second click did not collapse")
	}
	m = chatStep(m, tea.MouseClickMsg{X: 5, Y: m.height - 2, Button: tea.MouseLeft})
	if !m.input.Focused() {
		t.Fatal("click on the input did not focus it")
	}
}

func TestChatClickMinimalSummaryOpensRun(t *testing.T) {
	m := newChatScene()
	m = chatStep(m, tea.MouseClickMsg{X: 5, Y: rowOf(t, m, "+ 1 command"), Button: tea.MouseLeft})
	if out := screen(m); !strings.Contains(out, "✓  Bash") || strings.Contains(out, "+ 1 command") {
		t.Fatalf("summary click did not open the run:\n%s", out)
	}
}

func TestChatDecideErrorIsShown(t *testing.T) {
	m := newChatScene()
	m = chatStep(m, chatDecidedMsg{id: "d1", err: fmt.Errorf("daemon decisions: 500")})
	if len(m.pending) != 1 || !strings.Contains(screen(m), "answer failed: daemon decisions: 500") {
		t.Fatalf("error not shown:\n%s", screen(m))
	}
}

func TestChatAgentsOverlay(t *testing.T) {
	m := chatStep(newChatScene(), ckey("tab"))
	out := screen(m)
	for _, s := range []string{"Agents", "1 running", "●  TK-29", "✗  TES-9", "WAITING ON YOU", "?  Merge #142"} {
		if !strings.Contains(out, s) {
			t.Fatalf("agents overlay missing %q:\n%s", s, out)
		}
	}
	if m = chatStep(m, ckey("tab")); m.overlay != overlayNone {
		t.Fatal("tab does not close the overlay")
	}
	m = chatStep(chatStep(m, ckey("tab")), ckey("esc"))
	if m.overlay != overlayNone {
		t.Fatal("esc does not close the overlay")
	}
}

func TestChatPalette(t *testing.T) {
	m := chatStep(newChatScene(), ckey("ctrl+k"))
	out := screen(m)
	for _, s := range []string{"WAITING ON YOU", "ACTIONS", "SESSIONS", "SETTINGS", "Merge #142", "Verbosity: minimal", "TK-29"} {
		if !strings.Contains(out, s) {
			t.Fatalf("palette missing %q:\n%s", s, out)
		}
	}
	m = typeIn(m, "verb")
	if out = screen(m); len(m.palItems()) != 1 || !strings.Contains(out, "Verbosity") || !strings.Contains(out, "1 results") {
		t.Fatalf("filter:\n%s", out)
	}
	m = chatStep(m, ckey("enter"))
	if m.overlay != overlayNone || m.mode != modeExpandable {
		t.Fatalf("enter did not run the action: overlay %v mode %v", m.overlay, m.mode)
	}
	m = chatStep(m, ckey("ctrl+k"))
	m = typeIn(m, "agents")
	m = chatStep(m, ckey("enter"))
	if m.overlay != overlayAgents {
		t.Fatal("palette did not open the agents overlay")
	}
	m = chatStep(chatStep(m, ckey("esc")), ckey("ctrl+k"))
	m = chatStep(m, ckey("esc"))
	if m.overlay != overlayNone || m.input.Value() != "" {
		t.Fatal("esc does not close the palette")
	}
}

func TestChatSlashCommands(t *testing.T) {
	m := newChatScene()
	m = chatStep(typeIn(m, "/mode verbose"), ckey("enter"))
	if m.mode != modeVerbose {
		t.Fatal("/mode verbose")
	}
	m = chatStep(typeIn(m, "/agents"), ckey("enter"))
	if m.overlay != overlayAgents {
		t.Fatal("/agents")
	}
	m = chatStep(m, ckey("esc"))
	m = chatStep(typeIn(m, "/bogus"), ckey("enter"))
	if !strings.Contains(screen(m), "unknown command /bogus") {
		t.Fatalf("unknown command:\n%s", screen(m))
	}
	if _, cmd := typeIn(m, "/quit").Update(ckey("enter")); cmd == nil {
		t.Fatal("/quit did not quit")
	}
	if _, cmd := m.Update(ckey("esc")); cmd != nil {
		t.Fatal("esc with no overlay must not quit; ctrl+c does")
	}
}

func TestChatModeChangeKeepsVirtualized(t *testing.T) {
	m := newChatTest(10000)
	for i := range 10000 {
		if i%3 == 0 {
			m.msgs[i] = &chatMsg{who: "daemon", tool: &chatTool{Name: "Bash", Arg: fmt.Sprint(i), Status: "ok", Lines: []string{"ok"}}}
		}
	}
	m = chatStep(m, ckey("ctrl+o"))
	m.View()
	wrapped := 0
	for _, c := range m.msgs {
		if c.lines != nil {
			wrapped++
		}
	}
	if wrapped > 2*m.height {
		t.Fatalf("wrapped %d messages for a %d-row screen", wrapped, m.height)
	}
}

func TestChatGolden(t *testing.T) {
	for _, g := range []struct {
		name string
		keys []string
	}{
		{"minimal", nil}, {"expandable", []string{"ctrl+o"}}, {"verbose", []string{"ctrl+o", "ctrl+o"}},
		{"agents", []string{"tab"}}, {"palette", []string{"ctrl+k"}},
	} {
		m := newChatScene()
		for _, k := range g.keys {
			m = chatStep(m, ckey(k))
		}
		got := screen(m)
		lines := strings.Split(got, "\n")
		if len(lines) != m.height {
			t.Errorf("%s: %d lines", g.name, len(lines))
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w > m.width {
				t.Errorf("%s: line %d is %d wide", g.name, i, w)
			}
		}
		file := filepath.Join("testdata", fmt.Sprintf("chat-%s-%dx%d.golden", g.name, m.width, m.height))
		if *update {
			write(t, file, got)
			continue
		}
		want, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if got != string(want) {
			t.Errorf("%s differs (go test -tags harness ./internal/tui/chat -update):\n%s", file, got)
		}
	}
}

func TestChatAskCommandPostsDemoQuestion(t *testing.T) {
	var id, q string
	var opts []string
	m := newChatScene()
	m.ask = func(i, question string, o []string) tea.Cmd { id, q, opts = i, question, o; return nil }
	m.input.SetValue("/ask")
	m = chatStep(m, ckey("enter"))
	if !strings.HasPrefix(id, "ask:demo") || q == "" || len(opts) < 2 {
		t.Fatalf("/ask posted %q %q %v", id, q, opts)
	}
	m.input.SetValue("/ask Ship on Friday?")
	m = chatStep(m, ckey("enter"))
	if q != "Ship on Friday?" {
		t.Fatalf("/ask <question> posted %q", q)
	}
	m.ask = nil
	m.input.SetValue("/ask")
	if out := screen(chatStep(m, ckey("enter"))); !strings.Contains(out, "needs smithy daemon") {
		t.Fatalf("/ask without a daemon said nothing:\n%s", out)
	}
}
