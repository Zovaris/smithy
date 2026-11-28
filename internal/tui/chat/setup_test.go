//go:build harness

package chat

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/agents"
	"github.com/Zovaris/smithy/internal/api"
)

type fakeSetup struct {
	s       api.Setup
	set     []string
	kits    []api.KitChoice
	install []string
	fail    error
}

func (f *fakeSetup) Setup(context.Context) (api.Setup, error) { return f.s, f.fail }
func (f *fakeSetup) SetDefault(_ context.Context, a, m string) error {
	f.set = append(f.set, a+"|"+m)
	f.s.Default, f.s.Chosen = api.AgentChoice{Agent: a, Model: m}, true
	return f.fail
}
func (f *fakeSetup) SetKit(_ context.Context, k api.KitChoice) error {
	f.kits = append(f.kits, k)
	f.s.Kit = k
	return f.fail
}
func (f *fakeSetup) InstallSkills(_ context.Context, h string) (string, error) {
	f.install = append(f.install, h)
	return "installed 8 skills into claude", f.fail
}

func firstRunSetup() *fakeSetup {
	return &fakeSetup{s: api.Setup{
		Agents: []agents.Ready{
			{Name: "claude", Kind: "cli", Ready: true, Detail: "/opt/homebrew/bin/claude"},
			{Name: "codex", Kind: "cli", Detail: "codex not found", Fix: "npm install -g @openai/codex"},
			{Name: "api:anthropic", Kind: "api", Ready: true, Detail: "smithy auth"},
			{Name: "api:openai", Kind: "api", Detail: "no key", Fix: "smithy auth set openai"},
		},
		Default: api.AgentChoice{Agent: "claude"},
		Kit:     api.KitChoice{MCP: true, Skills: []string{}},
		Skills:  []string{"smelt", "hush"},
		Harnesses: []agents.HarnessInventory{
			{Harness: "claude", Present: true, SkillsDir: "~/.claude/skills", Smithy: []string{"smelt", "hush"}, Missing: []string{"use-smithy", "warden", "ledger", "scout", "tend", "simple"}, MCP: []string{"linear"}},
			{Harness: "pi", Present: false},
		},
	}}
}

// run feeds cmd's message (and the ones it leads to) back into m; a command that doesn't answer
// at once (a cursor blink tick) is dropped.
func run(m chatView, cmd tea.Cmd) chatView {
	for i := 0; cmd != nil && i < 8; i++ {
		got := make(chan tea.Msg, 1)
		go func(c tea.Cmd) { got <- c() }(cmd)
		var msg tea.Msg
		select {
		case msg = <-got:
		case <-time.After(100 * time.Millisecond):
		}
		if b, ok := msg.(tea.BatchMsg); ok {
			for _, c := range b {
				m = run(m, c)
			}
			return m
		}
		if msg == nil {
			break
		}
		var next tea.Model
		next, cmd = m.Update(msg)
		m = next.(chatView)
	}
	return m
}

func setupScene(f *fakeSetup) chatView {
	m := newChatScene()
	m.pending = nil
	m.setup = f
	return run(m, m.loadSetup(true))
}

func TestFirstRunShowsEveryAgentReadyOrMissingWithItsFix(t *testing.T) {
	f := firstRunSetup()
	m := sendKey(setupScene(f), ckey("enter"))
	if m.overlay != overlaySetup {
		t.Fatalf("the welcome's first step is the agents, got %v", m.overlay)
	}
	out := screen(m)
	for _, want := range []string{"1/4  agents", "claude", "/opt/homebrew/bin/claude", "codex", "npm install -g @openai/codex", "smithy auth set openai"} {
		if !strings.Contains(out, want) {
			t.Fatalf("first run screen lacks %q:\n%s", want, out)
		}
	}
	m = sendKey(m, ckey("enter"))
	if len(f.set) != 1 || f.set[0] != "claude|" {
		t.Fatalf("enter on claude picks it: %v", f.set)
	}
	if m = sendKey(m, ckey("esc")); !strings.Contains(screen(sendKey(m, ckey("esc"))), "new sessions start with claude") {
		t.Fatalf("picking says so:\n%s", screen(m))
	}
}

func TestFirstRunSkippedOnceChosen(t *testing.T) {
	f := firstRunSetup()
	f.s.Chosen = true
	if m := setupScene(f); m.overlay != overlayNone {
		t.Fatalf("an agent already picked: no first-run overlay, got %v", m.overlay)
	}
}

func TestAgentPickerAndModel(t *testing.T) {
	f := firstRunSetup()
	f.s.Chosen = true
	m := setupScene(f)
	m = sendKey(m, tea.KeyPressMsg{Code: 'l', Mod: tea.ModCtrl})
	if m.overlay != overlayPicker || !strings.Contains(screen(m), "Pick an agent") {
		t.Fatalf("ctrl+l picker:\n%s", screen(m))
	}
	m = sendKey(sendKey(m, ckey("down")), ckey("enter"))
	if len(f.set) != 0 || !strings.Contains(screen(m), "npm install -g @openai/codex") {
		t.Fatalf("a missing agent is not picked and shows its fix: %v\n%s", f.set, screen(m))
	}
	m = sendKey(sendKey(m, ckey("down")), ckey("enter"))
	if m.setupStep != stepModel || !strings.Contains(screen(m), "Model for api:anthropic") {
		t.Fatalf("an api agent asks for its model:\n%s", screen(m))
	}
	m = sendKey(typeIn(m, "claude-sonnet-5-5"), ckey("enter"))
	if len(f.set) != 1 || f.set[0] != "api:anthropic/claude-sonnet-5-5|" {
		t.Fatalf("api agent with its model: %v", f.set)
	}
	m = sendKey(typeIn(m, "/agent claude"), ckey("enter"))
	if f.set[len(f.set)-1] != "claude|" {
		t.Fatalf("/agent claude: %v", f.set)
	}
	m = sendKey(typeIn(m, "/model opus"), ckey("enter"))
	if f.set[len(f.set)-1] != "claude|opus" {
		t.Fatalf("/model opus keeps the agent: %v", f.set)
	}
	if c := strings.Join(m.composer(160), "\n"); !strings.Contains(c, "claude") || !strings.Contains(c, "opus") {
		t.Fatalf("the composer names the agent and model: %s", c)
	}
	m = sendKey(typeIn(m, "/model"), ckey("enter"))
	if m.overlay != overlayPicker || m.setupStep != stepModel || !strings.Contains(screen(m), "Model for claude") {
		t.Fatalf("/model alone asks for the current agent's model:\n%s", screen(m))
	}
	m = sendKey(m, ckey("esc"))
	m = sendKey(typeIn(m, "/agent nope"), ckey("enter"))
	if !strings.Contains(screen(m), "nope") || f.set[len(f.set)-1] != "claude|opus" {
		t.Fatalf("/agent with an unknown agent: %v\n%s", f.set, screen(m))
	}
}

func TestSkillsOverlayTogglesAndInstalls(t *testing.T) {
	f := firstRunSetup()
	f.s.Chosen = true
	m := setupScene(f)
	m = sendKey(typeIn(m, "/skills"), ckey("enter"))
	if m.overlay != overlaySkills {
		t.Fatalf("/skills: %v", m.overlay)
	}
	out := screen(m)
	for _, want := range []string{"smithy MCP server", "smelt", "hush", "claude", "2/8", "linear", "pi", "not installed"} {
		if !strings.Contains(out, want) {
			t.Fatalf("skills overlay lacks %q:\n%s", want, out)
		}
	}
	m = sendKey(m, ckey(" "))
	if len(f.kits) != 1 || f.kits[0].MCP {
		t.Fatalf("space on the MCP row turns it off: %+v", f.kits)
	}
	m = sendKey(sendKey(m, ckey("down")), ckey(" "))
	if k := f.kits[len(f.kits)-1]; len(k.Skills) != 1 || k.Skills[0] != "smelt" || k.MCP {
		t.Fatalf("space on smelt injects it: %+v", k)
	}
	m = sendKey(m, ckey("i"))
	if len(f.install) != 1 || f.install[0] != "" {
		t.Fatalf("i installs smithy skills into every harness: %v", f.install)
	}
	if !strings.Contains(screen(m), "installed 8 skills") {
		t.Fatalf("install output shown:\n%s", screen(m))
	}
}

func TestSetupErrorIsShown(t *testing.T) {
	f := firstRunSetup()
	f.s.Chosen = true
	m := setupScene(f)
	f.fail = errors.New("daemon down")
	m = sendKey(typeIn(m, "/agent claude"), ckey("enter"))
	if !strings.Contains(screen(m), "daemon down") {
		t.Fatalf("error:\n%s", screen(m))
	}
}

// sendKey sends msg and feeds the commands it leads to back in.
func sendKey(m chatView, msg tea.Msg) chatView {
	next, cmd := m.Update(msg)
	return run(next.(chatView), cmd)
}
