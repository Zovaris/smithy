//go:build harness

package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func noSessionScene() (*fakeSettings, chatView) {
	_, m := modelsScene()
	fs := &fakeSettings{}
	m.cfg = fs
	m.current = ""
	return fs, m
}

func TestEffortWithoutSessionSetsTheDefault(t *testing.T) {
	fs, m := noSessionScene()
	m2, cmd := m.effortCommand("low")
	m = run(m2.(chatView), cmd)
	if len(fs.set) != 1 || fs.set[0] != [2]string{"agent.effort", "low"} {
		t.Fatalf("set %v", fs.set)
	}
	if !strings.Contains(screen(m), "next session") {
		t.Fatalf("says it is the default for the next session:\n%s", screen(m))
	}
	m2, cmd = m.effortCommand("off")
	run(m2.(chatView), cmd)
	if len(fs.unset) != 1 || fs.unset[0] != "agent.effort" {
		t.Fatalf("off unsets agent.effort: %v", fs.unset)
	}
	m2, cmd = m.effortCommand("xhigh")
	run(m2.(chatView), cmd)
	if len(fs.set) != 1 {
		t.Fatalf("a level above the cap is refused: %v", fs.set)
	}
}

func TestEffortPickerWithoutSessionSavesTheDefault(t *testing.T) {
	fs, m := noSessionScene()
	m2, cmd := m.effortCommand("")
	m = run(m2.(chatView), cmd)
	if !strings.Contains(screen(m), "next session") {
		t.Fatalf("the picker says it sets the next session's default:\n%s", screen(m))
	}
	m = run(sendKey(m, ckey("down")), nil)
	sendKey(m, ckey("enter"))
	if len(fs.set) != 1 || fs.set[0] != [2]string{"agent.effort", "low"} {
		t.Fatalf("set %v", fs.set)
	}
}

func TestEffortSpaceCompletesLevels(t *testing.T) {
	_, m := noSessionScene()
	m.input.SetValue("/effort ")
	var got []string
	for _, it := range m.popup() {
		got = append(got, it.fill)
	}
	if strings.Join(got, ",") != "/effort default,/effort low,/effort medium" {
		t.Fatalf("levels %v", got)
	}
	m.input.SetValue("/effort me")
	if p := m.popup(); len(p) != 1 || p[0].fill != "/effort medium" {
		t.Fatalf("filtered %v", p)
	}
}

func TestTopEffortShimmersUnlessStill(t *testing.T) {
	_, m := noSessionScene()
	m2, cmd := m.effortCommand("medium")
	m = run(m2.(chatView), cmd)
	if m.shimmer <= 0 || !m.animating() {
		t.Fatalf("the top level starts a shimmer")
	}
	_, m = noSessionScene()
	m.still = true
	m2, cmd = m.effortCommand("medium")
	m = run(m2.(chatView), cmd)
	if m.shimmer != 0 {
		t.Fatalf("reduced motion plays no shimmer")
	}
	_, m = noSessionScene()
	m2, cmd = m.effortCommand("low")
	m = run(m2.(chatView), cmd)
	if m.shimmer != 0 {
		t.Fatalf("only the top level shimmers")
	}
}

func TestSlashMenuCommandsThenSkillsByNameAndDescription(t *testing.T) {
	m := skillScene(&fakeSessions{})
	m.input.SetValue("/")
	items := m.popup()
	seenSkill := false
	for _, it := range items {
		if it.skill {
			seenSkill = true
		} else if seenSkill {
			t.Fatalf("a command after a skill: %v", items)
		}
	}
	if !seenSkill {
		t.Fatalf("skills listed: %v", items)
	}
	m.input.SetValue("/browser")
	found := false
	for _, it := range m.popup() {
		if it.label == "/web" {
			found = true
		}
	}
	if !found {
		t.Fatalf("matches by description: %v", m.popup())
	}
	m.width, m.height = 160, 48
	out := ansi.Strip(screen(typeIn(m, "/pd")))
	if strings.Contains(out, "Skills") || strings.Contains(out, "Commands") {
		t.Fatalf("no section titles:\n%s", out)
	}
}
