//go:build harness

package chat

import (
	"fmt"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/api"
	"github.com/Zovaris/smithy/internal/daemon"
	"github.com/Zovaris/smithy/internal/tui/kit"
	"github.com/charmbracelet/colorprofile"
)

func newChatTest(n int) chatView {
	m := newChatView(nil, "http://127.0.0.1:7477", kit.NewTheme(true, colorprofile.Ascii, false))
	m.width, m.height = 80, 20
	for i := range n {
		m.push(chatMsg{who: "daemon", text: fmt.Sprintf("message %d", i)})
	}
	return m
}

func chatStep(m chatView, msg tea.Msg) chatView {
	n, _ := m.Update(msg)
	return n.(chatView)
}

func TestChatInputPinnedFullscreen(t *testing.T) {
	m := newChatTest(3)
	v := m.View()
	if !v.AltScreen {
		t.Fatal("chat is not fullscreen")
	}
	lines := strings.Split(v.Content, "\n")
	if len(lines) != m.height {
		t.Fatalf("got %d lines, want %d", len(lines), m.height)
	}
	if !strings.Contains(lines[len(lines)-5], "for commands") && !strings.Contains(lines[len(lines)-4], "for commands") || !strings.Contains(lines[len(lines)-1], "enter send") {
		t.Fatalf("input and footer not pinned at the bottom:\n%s", v.Content)
	}
	if !strings.Contains(v.Content, "message 2") {
		t.Fatalf("latest message missing:\n%s", v.Content)
	}
}

func TestChatEnterAddsUserMessage(t *testing.T) {
	m := newChatTest(0)
	for _, r := range "hello" {
		m = chatStep(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	m = chatStep(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(m.msgs) != 1 || m.msgs[0].text != "hello" || m.input.Value() != "" {
		t.Fatalf("msgs %+v input %q", m.msgs, m.input.Value())
	}
}

func TestChatVirtualizedRendersOnlyVisible(t *testing.T) {
	m := newChatTest(10000)
	m.View()
	wrapped := 0
	for _, c := range m.msgs {
		if c.lines != nil {
			wrapped++
		}
	}
	if wrapped > m.height {
		t.Fatalf("wrapped %d messages for a %d-row screen", wrapped, m.height)
	}
}

func TestChatScrollUpAndClampAtTop(t *testing.T) {
	m := newChatTest(100)
	m = chatStep(m, tea.KeyPressMsg{Code: tea.KeyPgUp})
	if m.offset == 0 || strings.Contains(m.View().Content, "message 99") {
		t.Fatalf("pgup did not scroll: offset %d", m.offset)
	}
	m = chatStep(m, tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	m = chatStep(m, tea.KeyPressMsg{Code: tea.KeyHome})
	if out := m.View().Content; !strings.Contains(out, "message 0") {
		t.Fatalf("top not reachable:\n%s", out)
	}
	top := m.offset
	m = chatStep(m, tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if m.offset != top {
		t.Fatal("offset not clamped at the top")
	}
	m = chatStep(m, tea.KeyPressMsg{Code: tea.KeyEnd})
	if m.offset != 0 {
		t.Fatal("end did not return to the bottom")
	}
}

func TestChatStreamResumesAfterLastSeq(t *testing.T) {
	m := newChatTest(0)
	m = chatStep(m, api.ClientMsg{Connected: true})
	m = chatStep(m, ev(1, "log", map[string]string{"line": "cycle started"}))
	m = chatStep(m, api.ClientMsg{Err: fmt.Errorf("lost")})
	m = chatStep(m, ev(1, "log", map[string]string{"line": "cycle started"}))
	m = chatStep(m, ev(2, daemon.KindAsked, api.Decision{ID: "d1", Question: "merge?"}))
	if len(m.noise) != 2 || len(m.pending) != 1 || m.last != 2 || m.conn != "reconnecting" {
		t.Fatalf("noise %d pending %d last %d conn %s", len(m.noise), len(m.pending), m.last, m.conn)
	}
}

func BenchmarkChatView10k(b *testing.B) {
	m := newChatTest(10000)
	m.width, m.height = 120, 50
	b.ResetTimer()
	for i := range b.N {
		m.offset = (i * 7) % 20000
		m.View()
	}
}

func TestChatView10kFast(t *testing.T) {
	limit := int64(5_000_000)
	if os.Getenv("CI") != "" {
		limit = 25_000_000 // shared runners are noisy; still catches a non-virtualized transcript
	}
	r := testing.Benchmark(BenchmarkChatView10k)
	if ns := r.NsPerOp(); ns > limit {
		t.Fatalf("a frame of a 10k-message chat took %dns, want < %dns", ns, limit)
	}
}
