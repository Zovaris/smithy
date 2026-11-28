//go:build harness

package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/api"
)

type fakeSettings struct {
	serve   []bool
	set     [][2]string
	unset   []string
	removed []string
	tested  int
	doctor  []string
}

func (f *fakeSettings) Settings(context.Context) (api.Settings, error) {
	return api.Settings{Serve: true, Prompt: api.PromptState{Path: "/home/dev/.smithy/prompt.md", Active: true}, Keys: []api.KeyRow{{Service: "linear", Masked: "****wxyz"}}}, nil
}
func (f *fakeSettings) SetSetting(_ context.Context, k, v string) error {
	f.set = append(f.set, [2]string{k, v})
	return nil
}
func (f *fakeSettings) UnsetSetting(_ context.Context, k string) error {
	f.unset = append(f.unset, k)
	return nil
}
func (f *fakeSettings) RemoveKey(_ context.Context, s string) error {
	f.removed = append(f.removed, s)
	return nil
}
func (f *fakeSettings) SetServe(_ context.Context, on bool) error {
	f.serve = append(f.serve, on)
	return nil
}
func (f *fakeSettings) Doctor(context.Context) ([]string, error) {
	if f.doctor != nil {
		return f.doctor, nil
	}
	return []string{"ok   git found"}, nil
}

func TestDetachQuitsAndLeavesTheDaemon(t *testing.T) {
	m := newChatTest(0)
	m.where = whereBackground
	m = typeIn(m, "/detach")
	_, cmd := m.Update(ckey("enter"))
	if cmd == nil {
		t.Fatal("/detach must quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("/detach must quit")
	}
	m = newChatTest(0)
	m.where = whereStandalone
	m = typeIn(m, "/detach")
	n, cmd := m.Update(ckey("enter"))
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatal("standalone: the daemon dies with this process, so /detach must not quit")
		}
	}
	if !strings.Contains(screen(n.(chatView)), "standalone") {
		t.Fatal("standalone /detach must say why it stays")
	}
}

func TestSettingsCommandsGoThroughTheDaemon(t *testing.T) {
	f := &fakeSettings{}
	m := newChatTest(0)
	m.width, m.height = 160, 48
	m.cfg = f
	for _, line := range []string{"/serve off", "/serve on", "/set linear.team ENG", "/unset linear.team", "/key rm linear"} {
		m = cpress(typeIn(m, line), ckey("enter"))
	}
	if len(f.serve) != 2 || f.serve[0] || !f.serve[1] {
		t.Fatalf("serve: %v", f.serve)
	}
	if len(f.set) != 1 || f.set[0] != [2]string{"linear.team", "ENG"} || len(f.unset) != 1 || len(f.removed) != 1 {
		t.Fatalf("set %v unset %v removed %v", f.set, f.unset, f.removed)
	}
	m = cpress(typeIn(m, "/doctor"), ckey("enter"))
	if !strings.Contains(screen(m), "git found") {
		t.Fatalf("doctor lines missing:\n%s", screen(m))
	}
	m = cpress(m, ckey("esc"))
	m = cpress(typeIn(m, "/keys"), ckey("enter"))
	if out := screen(m); !strings.Contains(out, "linear ****wxyz") || !strings.Contains(out, "serve on") {
		t.Fatalf("keys/serve missing:\n%s", out)
	}
}

func (f *fakeSettings) Tend(context.Context) (api.TendList, error) {
	return api.TendList{Mode: "fix", PRs: []api.TendPR{
		{TendRef: api.TendRef{URL: "https://github.com/o/r/pull/7", Batch: "b1", Item: "01"}, Action: "fix-ci", Checks: "red", Status: "fixing", PID: 42},
		{TendRef: api.TendRef{URL: "https://github.com/o/r/pull/8", Batch: "b1", Item: "02"}, Action: "done", Checks: "green", Status: "green"},
		{TendRef: api.TendRef{URL: "https://github.com/o/r/pull/9", Session: "s-abc"}, Action: "wait", Checks: "pending", Status: "waiting"},
	}}, nil
}

func TestTendPanelShowsEveryWatchedPR(t *testing.T) {
	m := newChatTest(0)
	m.width, m.height = 160, 48
	m.cfg = &fakeSettings{}
	m = cpress(typeIn(m, "/tend"), ckey("enter"))
	out := screen(m)
	for _, want := range []string{"tend.daemon  ‹ fix ›", "pull/7", "fixing", "fix-ci", "pull/8", "green", "session s-abc", "pull/9"} {
		if !strings.Contains(out, want) {
			t.Fatalf("/tend lacks %q:\n%s", want, out)
		}
	}
}

func TestPromptCommandShowsTheFileAndOverride(t *testing.T) {
	m := newChatTest(0)
	m.width, m.height = 160, 48
	m.cfg = &fakeSettings{}
	m = cpress(typeIn(m, "/prompt"), ckey("enter"))
	if out := screen(m); !strings.Contains(out, "/home/dev/.smithy/prompt.md") || !strings.Contains(out, "override active") {
		t.Fatalf("/prompt missing path or state:\n%s", out)
	}
}
