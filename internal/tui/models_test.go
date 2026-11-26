//go:build harness

package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/models"
)

type fakeModels struct {
	*fakeSetup
	asked   []string
	refresh int
}

func (f *fakeModels) Models(_ context.Context, agent string, refresh bool) (models.List, error) {
	f.asked = append(f.asked, agent)
	if refresh {
		f.refresh++
	}
	p := 1.5
	return models.List{Agent: agent, Source: "test", Models: []models.Model{
		{ID: "opencode/big-pickle", Name: "Big Pickle", Default: true},
		{ID: "opencode/beam-free", Name: "Beam Free", InPerM: &p, OutPerM: &p},
		{ID: "anthropic/claude-x", Name: "Claude X"},
	}}, nil
}

func modelsScene() (*fakeModels, chatView) {
	f := &fakeModels{fakeSetup: firstRunSetup()}
	f.s.Chosen = true
	m := newChatScene()
	m.pending = nil
	m.setup = f
	return f, run(m, m.loadSetup(true))
}

func TestModelListFromTheAgent(t *testing.T) {
	f, m := modelsScene()

	m2, cmd := m.modelCommand("")
	m = run(m2.(chatView), cmd)
	out := screen(m)
	for _, want := range []string{"Model for claude", "agent default", "opencode/big-pickle", "Beam Free", "$1.50"} {
		if !strings.Contains(out, want) {
			t.Fatalf("model list lacks %q:\n%s", want, out)
		}
	}
	if len(f.asked) != 1 || f.asked[0] != "claude" {
		t.Fatalf("asked %v", f.asked)
	}
	sendKey(typeIn(m, "beam"), ckey("enter"))
	if f.set[len(f.set)-1] != "claude|opencode/beam-free" {
		t.Fatalf("filter then enter picks the match: %v", f.set)
	}
}

func TestModelListDefaultRowAndOther(t *testing.T) {
	f, m := modelsScene()
	m2, cmd := m.modelCommand("")
	m = run(m2.(chatView), cmd)
	m = sendKey(m, ckey("enter"))
	if f.set[len(f.set)-1] != "claude|" {
		t.Fatalf("the first row is the agent default: %v", f.set)
	}
	m2, cmd = m.modelCommand("")
	m = run(m2.(chatView), cmd)
	m = typeIn(m, "my-own-model")
	if !strings.Contains(screen(m), "Other") {
		t.Fatalf("no match offers Other:\n%s", screen(m))
	}
	sendKey(m, ckey("enter"))
	if f.set[len(f.set)-1] != "claude|my-own-model" {
		t.Fatalf("Other uses the typed name: %v", f.set)
	}
}

func TestModelListRefresh(t *testing.T) {
	f, m := modelsScene()
	m2, cmd := m.modelCommand("")
	m = run(m2.(chatView), cmd)
	sendKey(m, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if f.refresh != 1 {
		t.Fatalf("ctrl+r refreshes: %d", f.refresh)
	}
}

func TestCtrlLEnterOpensModelsForCLI(t *testing.T) {
	f, m := modelsScene()
	m = sendKey(m, tea.KeyPressMsg{Code: 'l', Mod: tea.ModCtrl})
	m = sendKey(m, ckey("enter"))
	if m.setupStep != stepModel || !strings.Contains(screen(m), "agent default") {
		t.Fatalf("ctrl+l enter on a CLI shows its models:\n%s", screen(m))
	}
	_ = f
}
