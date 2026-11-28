//go:build harness

package chat

import (
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/api"
	"github.com/Zovaris/smithy/internal/workflow"
)

func TestModelSpaceCompletesTheAgentsModels(t *testing.T) {
	_, m := noSessionScene()
	m = typeIn(m, "/model")
	n, cmd := m.Update(ckey(" "))
	m = run(n.(chatView), cmd)
	var got []string
	for _, it := range m.popup() {
		got = append(got, it.fill)
	}
	if !strings.Contains(strings.Join(got, ","), "/model opencode/beam-free") {
		t.Fatalf("models %v", got)
	}
	m.input.SetValue("/model beam")
	if p := m.popup(); len(p) != 1 || p[0].fill != "/model opencode/beam-free" {
		t.Fatalf("filtered %v", p)
	}
}

func paletteScene(t *testing.T) (*fakeSettings, chatView) {
	t.Helper()
	fs, m := noSessionScene()
	n, cmd := m.Update(ckey("ctrl+k"))
	return fs, run(n.(chatView), cmd)
}

func TestPaletteFindsSettingsByKeyAndLabel(t *testing.T) {
	_, m := paletteScene(t)
	m = typeIn(m, "sett")
	if len(m.palItems()) < 3 {
		t.Fatalf("sett finds the settings rows: %v", m.palItems())
	}
	m.pal.query = "theme"
	got := m.palItems()
	if len(got) == 0 || got[0].label != "ui.theme" {
		t.Fatalf("a setting is found by its key: %+v", got)
	}
}

func TestPaletteSettingCyclesWithLeftRight(t *testing.T) {
	fs, m := paletteScene(t)
	m.pal.query = "chat.mode"
	before := m.mode
	m = chatStep(m, ckey("right"))
	if m.mode == before {
		t.Fatalf("right cycles chat.mode")
	}
	m = chatStep(m, ckey("left"))
	if m.mode != before {
		t.Fatalf("left cycles chat.mode back")
	}
	m.pal = palette{query: "ui.theme"}
	n, cmd := m.Update(ckey("right"))
	m = run(n.(chatView), cmd)
	if len(fs.set) != 1 || fs.set[0] != [2]string{"ui.theme", "frost"} {
		t.Fatalf("right saves the next option: %v", fs.set)
	}
	if !strings.Contains(screen(m), "frost") {
		t.Fatalf("the row shows the new value:\n%s", screen(m))
	}
	m.pal = palette{query: "auto.paused"}
	n, cmd = m.Update(ckey("left"))
	run(n.(chatView), cmd)
	if fs.set[len(fs.set)-1] != [2]string{"auto.paused", "true"} {
		t.Fatalf("a bool flips: %v", fs.set)
	}
}

func TestBareSetOpensTheSettingsList(t *testing.T) {
	_, m := noSessionScene()
	m = typeIn(m, "/set")
	n, cmd := m.Update(ckey("enter"))
	m = run(n.(chatView), cmd)
	if m.overlay != overlayPalette {
		t.Fatalf("one enter on /set opens the settings list, overlay %v", m.overlay)
	}
	for _, it := range m.palItems() {
		if it.group != "SETTINGS" {
			t.Fatalf("filtered to settings, got %+v", it)
		}
	}
}

func TestEscFromANestedViewClosesTheStack(t *testing.T) {
	w, _ := workflow.Parse([]byte(workflow.Example))
	f := &fakeFlows{l: api.WorkflowList{Workflows: []workflow.Info{{Workflow: w, File: "/nowhere/x.json"}}}}
	m := newChatTest(0)
	m.width, m.height = 160, 48
	m.flows = f
	m = cpress(typeIn(m, "/workflows"), ckey("enter"))
	m = cpress(m, ckey("enter"))
	if m.overlay != overlayInfo {
		t.Fatalf("enter opens the steps")
	}
	m = cpress(m, ckey("esc"))
	if m.overlay != overlayNone {
		t.Fatalf("esc closes the whole stack, overlay %v", m.overlay)
	}
	m = typeIn(m, "/goal ship r")
	if len(f.runs) != 0 || m.input.Value() != "/goal ship r" {
		t.Fatalf("typed text reaches the input: runs %v input %q", f.runs, m.input.Value())
	}
}

func TestSlashOnAListGoesToTheInput(t *testing.T) {
	w, _ := workflow.Parse([]byte(workflow.Example))
	f := &fakeFlows{l: api.WorkflowList{Workflows: []workflow.Info{{Workflow: w, File: "/nowhere/x.json"}}}}
	m := newChatTest(0)
	m.width, m.height = 160, 48
	m.flows = f
	m = cpress(typeIn(m, "/workflows"), ckey("enter"))
	m = typeIn(m, "/goal r")
	if len(f.runs) != 0 || m.overlay != overlayNone || m.input.Value() != "/goal r" {
		t.Fatalf("/ leaves the list for the input: runs %v overlay %v input %q", f.runs, m.overlay, m.input.Value())
	}
}

func TestGoalMetMarkerIsHidden(t *testing.T) {
	m := newChatTest(0)
	m.width, m.height = 120, 30
	m.push(chatMsg{who: "agent", text: "all tests pass\nGOAL MET: go test ./... ok"})
	out := screen(m)
	if strings.Contains(out, "GOAL MET") || !strings.Contains(out, "all tests pass") {
		t.Fatalf("the marker line is hidden, the reply kept:\n%s", out)
	}
}

func TestStatusLineNamesEventsInWords(t *testing.T) {
	cases := map[string][2]string{
		"workflow_run": {`{"workflow":"repo-health","status":"running","trigger":"tui"}`, "workflow repo-health running"},
		"event":        {`{"kind":"ci","item":"TK-1","detail":"red"}`, "TK-1 · ci red"},
	}
	for kind, c := range cases {
		got := eventText(kind, "", []byte(c[0]))
		if got != c[1] {
			t.Errorf("%s: got %q want %q", kind, got, c[1])
		}
	}
	if got := eventText("event", "", []byte(`{}`)); got != "" {
		t.Errorf("an empty event says nothing, got %q", got)
	}
	if got := eventText("some_kind", "", nil); got != "some kind" {
		t.Errorf("an unknown kind reads as words, got %q", got)
	}
}

func TestPopupDoesNotCoverTheLatestMessage(t *testing.T) {
	_, m := noSessionScene()
	m.width, m.height = 120, 30
	m.system("daemon started on 127.0.0.1:7777")
	m.input.SetValue("/effort ")
	out := screen(m)
	if !strings.Contains(out, "daemon started on") || !strings.Contains(out, "/effort low") {
		t.Fatalf("the popup sits under the latest message, not over it:\n%s", out)
	}
}
