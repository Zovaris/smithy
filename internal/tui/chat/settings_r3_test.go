//go:build harness

package chat

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Zovaris/smithy/internal/api"
	"github.com/Zovaris/smithy/internal/config"
)

func knownRows() []api.SettingRow {
	var out []api.SettingRow
	for _, k := range config.Known {
		r := api.SettingRow{Key: k.Key, Default: k.Default, Meaning: k.Meaning}
		r.Kind, r.Options = config.KindOf(k.Key)
		out = append(out, r)
	}
	return out
}

func selectKey(t *testing.T, m chatView, key string) chatView {
	t.Helper()
	i := slices.IndexFunc(m.configRows(), func(r api.SettingRow) bool { return r.Key == key })
	if i < 0 {
		t.Fatalf("%s not listed", key)
	}
	m.pal.sel = i
	return m
}

func TestConfigEnterPicksFromAList(t *testing.T) {
	m := configChat(knownRows()...)
	f := m.cfg.(*fakeSettings)
	m = cpress(selectKey(t, m, "tend.daemon"), ckey("enter"))
	if m.cfgEd == nil || m.cfgEd.kind != "pick" {
		t.Fatalf("enter on a fixed set opens a picker: %+v", m.cfgEd)
	}
	out := ansi.Strip(screen(m))
	for _, o := range []string{"off", "watch", "fix"} {
		if !strings.Contains(out, o) {
			t.Fatalf("picker lists %q:\n%s", o, out)
		}
	}
	m = cpress(m, ckey("down"))
	m = cpress(m, ckey("enter"))
	if m.cfgEd != nil || len(f.set) == 0 || f.set[len(f.set)-1][0] != "tend.daemon" {
		t.Fatalf("enter saves the picked value and closes the picker: %v %+v", f.set, m.cfgEd)
	}
	if m.overlay != overlayConfig || m.input.Value() != "" {
		t.Fatalf("nothing goes to the chat input: %q", m.input.Value())
	}
}

func TestConfigServeAgentPicksFromTheAgents(t *testing.T) {
	m := configChat(knownRows()...)
	m = cpress(selectKey(t, m, "auto.agent"), ckey("enter"))
	if m.cfgEd == nil || m.cfgEd.kind != "pick" || !slices.Contains(m.cfgEd.opts, "claude") || !slices.Contains(m.cfgEd.opts, "opencode") {
		t.Fatalf("auto.agent picks from the agents: %+v", m.cfgEd)
	}
}

func TestConfigTextValueEditsInline(t *testing.T) {
	m := configChat(knownRows()...)
	f := m.cfg.(*fakeSettings)
	m = cpress(selectKey(t, m, "tend.label"), ckey("enter"))
	if m.cfgEd == nil || m.cfgEd.kind != "text" {
		t.Fatalf("enter on free text opens an inline field: %+v", m.cfgEd)
	}
	m = cpress(m, ckey("ctrl+u"))
	m = typeIn(m, "keep-green")
	m = cpress(m, ckey("enter"))
	if len(f.set) == 0 || f.set[len(f.set)-1] != [2]string{"tend.label", "keep-green"} {
		t.Fatalf("enter saves the typed value: %v", f.set)
	}
	if m.input.Value() != "" || m.overlay != overlayConfig {
		t.Fatalf("the value never goes through the chat: %q", m.input.Value())
	}
}

func TestConfigCommaListIsAMultiSelect(t *testing.T) {
	rows := knownRows()
	for i := range rows {
		if rows[i].Key == "notify.kinds" {
			rows[i].Value = "pr_ready,blocked"
		}
	}
	m := configChat(rows...)
	f := m.cfg.(*fakeSettings)
	m = cpress(selectKey(t, m, "notify.kinds"), ckey("enter"))
	if m.cfgEd == nil || m.cfgEd.kind != "multi" {
		t.Fatalf("a comma list opens a multi-select: %+v", m.cfgEd)
	}
	i := slices.Index(m.cfgEd.opts, "stuck")
	if i < 0 {
		t.Fatalf("notify.kinds offers stuck: %v", m.cfgEd.opts)
	}
	m.cfgEd.sel = i
	m = cpress(m, ckey("space"))
	m = cpress(m, ckey("enter"))
	if len(f.set) == 0 || f.set[len(f.set)-1] != [2]string{"notify.kinds", "pr_ready,blocked,stuck"} {
		t.Fatalf("enter saves the checked kinds in order: %v", f.set)
	}
}

func TestAdvancedSettingsAreGroupedWithClearLabels(t *testing.T) {
	m := configChat(knownRows()...)
	main := map[string]bool{}
	for _, c := range configMain {
		main[c.key] = true
	}
	for _, k := range config.Known {
		if main[k.Key] {
			continue
		}
		if _, ok := advLabels[k.Key]; !ok {
			t.Errorf("%s has no clear label", k.Key)
		}
		if g := advGroup(k.Key); g == "" || g == "Other" {
			t.Errorf("%s has no topic", k.Key)
		}
	}
	if l := configLabel("auto.every"); !strings.Contains(l, "every") || !strings.Contains(l, "(s)") {
		t.Errorf("auto.every reads %q", l)
	}
	m.cfgAdv, m.cfgFull = true, true
	m.height = 200
	out := ansi.Strip(screen(m))
	for _, h := range []string{"Batches", "Guard", "Chat apps", "Board and Linear"} {
		if !strings.Contains(out, h) {
			t.Fatalf("advanced heading %q missing:\n%s", h, out)
		}
	}
	if strings.Contains(out, "─") {
		t.Fatalf("no rules under headings:\n%s", out)
	}
}

func TestConfigFullscreenWithADetailPane(t *testing.T) {
	m := configChat(knownRows()...)
	m.width = 200
	m = cpress(m, ckey("f"))
	if !m.cfgFull || m.pal.query != "" {
		t.Fatalf("f toggles fullscreen, it does not filter: %v %q", m.cfgFull, m.pal.query)
	}
	lines := strings.Split(screen(m), "\n")
	if w := ansi.StringWidth(lines[1]); w < 196 {
		t.Fatalf("fullscreen spans the screen: %d", w)
	}
	if !strings.Contains(ansi.Strip(screen(m)), "fullscreen") {
		t.Fatalf("the key hints stay on screen in fullscreen:\n%s", screen(m))
	}
	if !strings.Contains(ansi.Strip(screen(m)), "Default") {
		t.Fatalf("the detail pane names the default:\n%s", screen(m))
	}
	m = cpress(m, ckey("f"))
	if m.cfgFull {
		t.Fatal("f again leaves fullscreen")
	}
}

func TestConfigEditorHintsMatchItsKind(t *testing.T) {
	m := cpress(selectKey(t, configChat(knownRows()...), "tend.daemon"), ckey("enter"))
	if strings.Contains(ansi.Strip(screen(m)), "space check") {
		t.Fatal("a picker has nothing to check")
	}
	m = cpress(selectKey(t, configChat(knownRows()...), "notify.kinds"), ckey("enter"))
	if !strings.Contains(ansi.Strip(screen(m)), "space check") {
		t.Fatal("a multi-select says space checks")
	}
}
