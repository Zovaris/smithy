//go:build harness

package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/daemon"
	"github.com/Zovaris/smithy/internal/usage"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

func TestHeaderShowsFolderModelEffortTokensCost(t *testing.T) {
	m := newChatTest(0)
	m.width, m.height = 160, 40
	m.dir, m.effort, m.current = "/work/proj", "medium", "s-1"
	m.setupSt = &daemon.Setup{Default: daemon.AgentChoice{Agent: "claude", Model: "haiku"}}
	c := 0.12
	m = chatStep(m, sev(1, daemon.KindSessionUsage, "s-1", usage.Usage{Input: 1000, Output: 234, CostUSD: &c}))
	head := ansi.Strip(strings.Join(m.header(160), "\n"))
	for _, want := range []string{"proj", "haiku", "medium", "1.2k tok", "$0.12"} {
		if !strings.Contains(head, want) {
			t.Errorf("header lacks %q:\n%s", want, head)
		}
	}
}

func TestHeaderBandStaysStillWhileWorking(t *testing.T) {
	m := newChatView(nil, "http://127.0.0.1:7477", newTheme(false, colorprofile.TrueColor, true))
	m.width, m.height = 120, 30
	m.sessions = []chatSession{{id: "s-1", state: "running", turn: time.Now()}}
	m.current = "s-1"
	m.frame = 3
	a := strings.Join(m.header(120), "\n")
	m.frame = 41
	if b := strings.Join(m.header(120), "\n"); a != b {
		t.Fatal("the header band moves while a session works")
	}
	m.sessions[0].state = "done"
	if c := strings.Join(m.header(120), "\n"); c != a {
		t.Fatal("the header band jumps back when the turn ends")
	}
}

func TestLoaderIsOneSmallRowWithLiveTokRate(t *testing.T) {
	m := newChatTest(0)
	m.width, m.height = 100, 30
	m.current = "s-1"
	m.sessions = []chatSession{{id: "s-1", state: "running", turn: time.Now().Add(-2 * time.Second)}}
	m = chatStep(m, sev(1, daemon.KindSessionText, "s-1", map[string]string{"text": strings.Repeat("word ", 160)}))
	m.msgs = nil
	row := ansi.Strip(m.spinRow(100))
	if !strings.Contains(row, "tok/s") {
		t.Fatalf("no tok/s: %q", row)
	}
	if r := m.tokRate("s-1"); r < 50 || r > 150 {
		t.Fatalf("rate %.0f, want ~100 (200 tokens in 2s)", r)
	}
	lead := strings.TrimLeft(row, " ")
	if strings.ContainsAny(lead[:4], "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏") {
		t.Fatalf("loader still the braille spinner: %q", row)
	}
}
