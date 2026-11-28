//go:build harness

package chat

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/api"
	"github.com/Zovaris/smithy/internal/ghx"
	"github.com/charmbracelet/x/ansi"
)

func TestActivityLabelCountsKinds(t *testing.T) {
	tools := []chatTool{{Name: "Bash"}, {Name: "Bash"}, {Name: "Edit"}, {Name: "Read"}}
	if got := activityLabel(tools, 1); got != "+ 2 commands, 1 edit, 1 read, 1 thought" {
		t.Fatalf("got %q", got)
	}
	if got := activityLabel(nil, 2); got != "+ 2 thoughts" {
		t.Fatalf("got %q", got)
	}
	if got := activityLabel([]chatTool{{Name: "WebFetch"}}, 0); got != "+ 1 tool" {
		t.Fatalf("got %q", got)
	}
}

func TestContextLabel(t *testing.T) {
	if got := ctxLabel(29400, "claude-sonnet-4-5"); got != "29.4K (15%)" {
		t.Fatalf("got %q", got)
	}
	if got := ctxLabel(29400, "opus[1m]"); got != "29.4K (3%)" {
		t.Fatalf("got %q", got)
	}
	if got := ctxLabel(900, "some-unknown"); got != "900" {
		t.Fatalf("unknown window shows tokens only, got %q", got)
	}
	if got := ctxLabel(0, "haiku"); got != "" {
		t.Fatalf("nothing used yet shows nothing, got %q", got)
	}
}

func TestTurnFooter(t *testing.T) {
	got := turnFooter("claude", "haiku", 12300*time.Millisecond, 41.6, " · ")
	if got != "claude · haiku · 12.3s · 42 tok/s" {
		t.Fatalf("got %q", got)
	}
	if got := turnFooter("claude", "", 2*time.Second, 0, " · "); got != "claude · 2.0s" {
		t.Fatalf("got %q", got)
	}
}

func layoutView(w int) chatView {
	m := newChatTest(0)
	m.width, m.height = w, 40
	m.current = "s1"
	m.sessions = []chatSession{{id: "s1", title: "fix the login", agent: "claude", model: "haiku", state: "done"}}
	return m
}

func TestTabStripShowsSessionsAndPlus(t *testing.T) {
	m := layoutView(140)
	row := ansi.Strip(m.header(140)[1])
	if !strings.Contains(row, "Fix the login") || !strings.Contains(row, "+") {
		t.Fatalf("tab strip lacks the title or +: %q", row)
	}
}

func TestSidebarWideOnlyAndToggles(t *testing.T) {
	m := layoutView(140)
	m.ctx = map[string]int64{"s1": 29400}
	m.cost = map[string]float64{"s1": 0.12}
	out := screen(m)
	for _, want := range []string{"Context", "29.4K", "15%", "$0.12", "Links"} {
		if !strings.Contains(out, want) {
			t.Fatalf("sidebar lacks %q:\n%s", want, out)
		}
	}
	if s := screen(layoutView(90)); strings.Contains(s, "Links") {
		t.Fatalf("sidebar shows on a narrow terminal:\n%s", s)
	}
	m = cpress(typeIn(m, "/sidebar"), ckey("enter"))
	if strings.Contains(screen(m), "Links") {
		t.Fatal("/sidebar must hide it")
	}
}

func TestSidebarListsLinkedPRs(t *testing.T) {
	m := layoutView(140)
	m.sessions[0].prs = []string{"https://github.com/o/r/pull/7"}
	if out := screen(m); !strings.Contains(out, "pull/7") {
		t.Fatalf("sidebar lacks the PR:\n%s", out)
	}
}

func TestMinimalCollapsesActivityAndExpands(t *testing.T) {
	m := layoutView(140)
	m.mode = modeMinimal
	m.push(chatMsg{who: "you", text: "go"})
	m.push(chatMsg{who: "daemon", tool: &chatTool{Name: "Bash", Arg: "go test ./...", Status: "ok"}})
	m.push(chatMsg{who: "daemon", kind: "thinking", text: "considering the fix"})
	m.push(chatMsg{who: "daemon", tool: &chatTool{Name: "Bash", Arg: "go vet", Status: "ok"}})
	m.push(chatMsg{who: "agent", text: "done"})
	out := screen(m)
	if !strings.Contains(out, "+ 2 commands, 1 thought") || strings.Contains(out, "considering the fix") {
		t.Fatalf("activity not collapsed:\n%s", out)
	}
	m = m.toggle(1)
	if out := screen(m); !strings.Contains(out, "go vet") || !strings.Contains(out, "considering the fix") {
		t.Fatalf("expanded activity lacks its rows:\n%s", out)
	}
}

func TestTurnFooterAfterAnswer(t *testing.T) {
	m := layoutView(140)
	m.sessions[0].state = "running"
	m.sessions[0].turn = time.Now().Add(-3 * time.Second)
	m.push(chatMsg{who: "agent", text: "all green"})
	m.track("s1", "done")
	if out := screen(m); !strings.Contains(out, "claude "+m.th.G.Dot+" haiku "+m.th.G.Dot+" 3.") {
		t.Fatalf("no turn footer:\n%s", out)
	}
}

func TestAnsweredAskStaysAsQA(t *testing.T) {
	m := layoutView(140)
	d, _ := json.Marshal(map[string]any{"id": "q1", "question": "Merge now?", "options": []string{"yes", "no"}, "session": m.current})
	m.event("decision_asked", m.current, time.Now(), d)
	a, _ := json.Marshal(map[string]any{"id": "q1", "answer": "yes"})
	m.event("decision_answered", "", time.Now(), a)
	out := screen(m)
	if !strings.Contains(out, "Merge now?") || !strings.Contains(out, "→ yes") {
		t.Fatalf("no Q → A block:\n%s", out)
	}
}

func TestComposerShowsAgentAndFooterContext(t *testing.T) {
	m := layoutView(140)
	m.ctx = map[string]int64{"s1": 29400}
	m.sideOff = true
	comp := ansi.Strip(strings.Join(m.composer(140), "\n"))
	if !strings.Contains(comp, " claude ") || !strings.Contains(comp, " haiku ") || !strings.Contains(comp, "|") {
		t.Fatalf("composer lacks the accent bar or agent · model:\n%s", comp)
	}
	foot := ansi.Strip(m.footer(140))
	if !strings.Contains(foot, "29.4K (15%)") || !strings.Contains(foot, "ctrl+k") {
		t.Fatalf("footer lacks context or the palette key: %q", foot)
	}
}

func TestTurnFooterOnlyUnderAnAnswerOnce(t *testing.T) {
	m := layoutView(140)
	m.sessions[0].state = "running"
	m.push(chatMsg{who: "you", text: "go"})
	m.track("s1", "done")
	if strings.Contains(screen(m), "haiku "+m.th.G.Dot) {
		t.Fatal("footer without an answer")
	}
	m.track("s1", "running")
	m.push(chatMsg{who: "agent", text: "ok"})
	m.track("s1", "done")
	m.track("s1", "running")
	m.track("s1", "done")
	if n := strings.Count(screen(m), "claude "+m.th.G.Dot+" haiku "+m.th.G.Dot); n != 1 {
		t.Fatalf("want one footer, got %d:\n%s", n, screen(m))
	}
}

func TestLocalAnswerStaysAsQAOnce(t *testing.T) {
	m := layoutView(140)
	m.decide = func(id, a string) tea.Cmd { return nil }
	d, _ := json.Marshal(map[string]any{"id": "q1", "question": "Ship it?", "options": []string{"yes", "no"}, "session": "s1"})
	m.event("decision_asked", "s1", time.Now(), d)
	m.answer("no")
	m.decided(chatDecidedMsg{id: "q1"})
	a, _ := json.Marshal(map[string]any{"id": "q1", "answer": "no"})
	m.event("decision_answered", "", time.Now(), a)
	if out := screen(m); strings.Count(out, "Ship it?") != 1 || !strings.Contains(out, "→ no") {
		t.Fatalf("want one Q → A block:\n%s", out)
	}
}

func TestSidebarShowsLiveLinks(t *testing.T) {
	m := layoutView(140)
	m.sessions[0].links = &api.SessionLinks{PRs: []ghx.PRLink{{Number: 42, State: "OPEN", Checks: "green", Title: "add health"}}, Issues: []api.LinkIssue{{ID: "TK-9", Status: "doing", Title: "health"}}}
	out := screen(m)
	for _, want := range []string{"PR #42", "open", "TK-9", "doing"} {
		if !strings.Contains(out, want) {
			t.Fatalf("sidebar lacks %q:\n%s", want, out)
		}
	}
}
