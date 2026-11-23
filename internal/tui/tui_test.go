package tui

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/events"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

var update = flag.Bool("update", false, "rewrite the golden files")

var fixedNow = time.Date(2026, 11, 23, 14, 30, 0, 0, time.UTC)

func write(t *testing.T, path, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func utc(t *testing.T) {
	old := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = old })
}

// fixture builds a smithy home with an auto state, a batch of three workers (one PR ready, one
// stuck, one queued), a task list, a decision, two worktrees (one orphaned), a worker log and events.
func fixture(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SMITHY_HOME", home)
	t.Setenv("WARDEN_HOME", "")
	utc(t)
	write(t, filepath.Join(home, "auto", "state.json"), `{"version":1,
"queue":[{"source":"github","key":"github","id":"42","title":"Add dark mode to the report","url":"https://github.com/o/r/issues/42","model":"mid","speed":"normal","queued_at":"2026-11-23T13:00:00Z","body":"The report is too bright.\nAdd a dark theme."},
{"source":"linear","key":"linear","id":"TES-7","title":"Fix flaky login test","url":"https://linear.app/x/issue/TES-7","queued_at":"2026-11-23T14:10:00Z"}],
"launched":{"2026-11-23":[{"source":"github","id":"40","batch":"b1","item":"01-api","at":"2026-11-23T09:10:00Z"},{"source":"github","id":"41","batch":"b1","item":"02-ui","at":"2026-11-23T13:20:00Z"}]},
"last_poll":{"github":"2026-11-23T14:29:30Z"},"rate":{"github":4999}}`)
	b := filepath.Join(home, "warden", "b1")
	write(t, filepath.Join(b, "batch.json"), `{"repo":"/src/smithy","items":[
{"id":"01-api","title":"API","model":"strong","speed":"fast","source":"github#40","branch":"feat/api","worktree":"/w/api"},
{"id":"02-ui","title":"UI","route":"cheap","source":"github#41","branch":"feat/ui","worktree":"/w/ui"},
{"id":"03-db","title":"DB","route":"mid"}]}`)
	write(t, filepath.Join(b, "01-api.json"), `{"state":"ready","phase":"ship","pr":"https://github.com/o/r/pull/9","ci":"green","updated":"2026-11-23T14:20:00Z","last_beat":"2026-11-23T14:28:00Z"}`)
	write(t, filepath.Join(b, "02-ui.json"), `{"state":"blocked","phase":"build","note":"tests need a DB","updated":"2026-11-23T14:00:00Z"}`)
	write(t, filepath.Join(b, "01-api.log"), "step 1\nstep 2\nopened PR #9\n")
	for _, e := range []events.Event{
		{T: fixedNow.Add(-90 * time.Minute), Kind: "queued", Source: "github", ID: "40"},
		{T: fixedNow.Add(-80 * time.Minute), Kind: "launched", Source: "github", ID: "40", Batch: "b1", Item: "01-api"},
		{T: fixedNow.Add(-40 * time.Minute), Kind: "ci", Batch: "b1", Item: "01-api", Detail: "pending"},
		{T: fixedNow.Add(-30 * time.Minute), Kind: "ci", Batch: "b1", Item: "01-api", Detail: "green", PR: "https://github.com/o/r/pull/9"},
		{T: fixedNow.Add(-10 * time.Minute), Kind: "stage", Batch: "b1", Item: "01-api", Detail: "ready", PR: "https://github.com/o/r/pull/9"},
		{T: fixedNow.Add(-5 * time.Minute), Kind: "skipped", Source: "github", ID: "43", Detail: "no auto label: Rename the CLI"},
	} {
		if err := events.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	tdir := filepath.Join(home, "plans", "repo", "v1")
	write(t, filepath.Join(tdir, "01-a.md"), "# 01 · A\nStatus: done\n")
	write(t, filepath.Join(tdir, "02-b.md"), "# 02 · B\nStatus: doing\n\nDo the B thing.\n")
	write(t, filepath.Join(tdir, "03-c.md"), "# 03 · C\nStatus: todo\n")
	oldDirs, oldDec, oldWT, oldRepo := taskDirs, pendingDecisions, scanWorktrees, repoName
	taskDirs = func() []string { return []string{tdir} }
	pendingDecisions = func() ([]string, error) {
		return []string{"2026-11-22 · timeout · Use Postgres? → yes · fits the stack · ref: https://github.com/o/r/pull/9"}, nil
	}
	scanWorktrees = func([]Batch, time.Time) []Worktree {
		return []Worktree{{Path: "/w/api", Branch: "feat/api", Item: "b1/01-api", Age: 25 * time.Minute, Ahead: 2},
			{Path: "/w/old", Branch: "feat/old", Age: 49 * time.Hour, Dirty: true, Orphan: true, Status: []string{" M main.go"}}}
	}
	repoName = func() string { return "smithy" }
	t.Cleanup(func() { taskDirs, pendingDecisions, scanWorktrees, repoName = oldDirs, oldDec, oldWT, oldRepo })
}

func newTest(w, h int, load func(time.Time) Snapshot) Model {
	m := newModel(options{profile: colorprofile.TrueColor, dark: true, unicode: true, seed: 7,
		now: func() time.Time { return fixedNow }, load: load})
	m.width, m.height = w, h
	m.opener = func(string) error { return nil }
	m.pauser = func(bool) error { return nil }
	m.loadSys = demoSystem
	m.confirm = func(int) error { return nil }
	m.reject = func(int, string) error { return nil }
	return m
}

func model(w, h int) Model { return newTest(w, h, Load) }

func demo(t *testing.T, w, h int) Model {
	utc(t)
	return newTest(w, h, Demo)
}

func TestLoad(t *testing.T) {
	fixture(t)
	s := Load(fixedNow)
	w := s.Watcher
	if !w.Alive || w.Today != 2 || w.Cap != 20 || len(w.Sources) != 1 || w.Paused {
		t.Fatalf("watcher = %+v", w)
	}
	if len(s.Queue) != 2 || len(s.Batches) != 1 || len(s.Batches[0].Items) != 3 {
		t.Fatalf("queue/batches = %d/%+v", len(s.Queue), s.Batches)
	}
	it := s.Batches[0].Items[0]
	if it.Stage != "verify" || it.Model != "strong" || it.Speed != "fast" || it.Repo != "smithy" || it.LastBeat == "" || !strings.HasSuffix(it.Log, "01-api.log") {
		t.Fatalf("item = %+v", it)
	}
	if len(s.Events) != 6 || s.Rates["github"] != 4999 || len(s.Worktrees) != 2 || s.Repo != "smithy" || len(s.Errors) != 0 {
		t.Fatalf("events/rates/worktrees = %d %v %d %v", len(s.Events), s.Rates, len(s.Worktrees), s.Errors)
	}
	if Load(fixedNow.Add(time.Hour)).Watcher.Alive {
		t.Fatal("watcher should be stale an hour later")
	}
}

func TestStats(t *testing.T) {
	fixture(t)
	s := Load(fixedNow)
	a, _ := throughput(s.Events, fixedNow)
	if a[22] != 1 {
		t.Fatalf("launched per hour = %v", a)
	}
	runs := ciRuns(s.Events, 20)
	if len(runs) != 1 || !runs[0].green || runs[0].took != 10*time.Minute {
		t.Fatalf("ci runs = %+v", runs)
	}
	ready, _ := leadTimes(s.Events)
	if len(ready) != 1 || ready[0] != 80*time.Minute {
		t.Fatalf("lead = %v", ready)
	}
	if p := percentile([]time.Duration{1, 2, 3, 4, 100}, 0.5); p != 3 {
		t.Fatalf("median = %v", p)
	}
}

func TestDemo(t *testing.T) {
	utc(t)
	a, b := Demo(fixedNow), Demo(fixedNow)
	if fmt.Sprint(a.Events) != fmt.Sprint(b.Events) || len(a.Events) < 24*20 {
		t.Fatalf("demo not deterministic or too thin: %d events", len(a.Events))
	}
	stages := map[string]int{}
	n := 0
	for _, bt := range a.Batches {
		for _, it := range bt.Items {
			stages[stageOf(it)]++
			n++
		}
	}
	for _, s := range []string{"queued", "building", "checking", "pr_open", "ready", "blocked", "merged"} {
		if stages[s] == 0 {
			t.Errorf("demo lacks stage %s: %v", s, stages)
		}
	}
	m := demo(t, 160, 48)
	if n != 14 || len(a.Tasks) != 4 || len(a.Watcher.Sources) != 2 || len(m.rows(vNeeds)) < 5 || !a.Demo {
		t.Fatalf("demo world: %d agents, %d plans, %d needs", n, len(a.Tasks), len(m.rows(vNeeds)))
	}
}

func TestDemoWritesNothing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMITHY_HOME", home)
	t.Setenv("NO_COLOR", "")
	defer func() { events.Off = false }()
	m := New(true, true)
	m.width, m.height = 120, 40
	m.render()
	if _, cmd := press(m, "p"); cmd != nil {
		if msg := cmd(); msg.(pausedMsg).err == nil {
			t.Fatal("demo pause should refuse")
		}
	}
	if entries, _ := os.ReadDir(home); len(entries) != 0 {
		t.Fatalf("demo wrote %v", entries)
	}
}

func TestGolden(t *testing.T) {
	for _, g := range []struct {
		w, h int
		v    view
	}{{160, 48, vOverview}, {160, 48, vAgents}, {100, 30, vOverview}, {100, 30, vAgents}, {70, 24, vNeeds}, {160, 48, vDone}, {160, 48, vSystem}} {
		m := demo(t, g.w, g.h)
		m.view = g.v
		s := demoSystem()
		m.sys = &s
		got := ansi.Strip(m.View().Content)
		lines := strings.Split(got, "\n")
		if len(lines) != g.h {
			t.Errorf("%dx%d: %d lines", g.w, g.h, len(lines))
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w > g.w {
				t.Errorf("%dx%d: line %d is %d wide", g.w, g.h, i, w)
			}
		}
		file := filepath.Join("testdata", fmt.Sprintf("demo-%s-%dx%d.golden", strings.ToLower(strings.ReplaceAll(viewNames[g.v], " ", "-")), g.w, g.h))
		if *update {
			write(t, file, got)
			continue
		}
		want, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if got != string(want) {
			t.Errorf("%s differs (go test ./internal/tui -update):\n%s", file, got)
		}
	}
}

func press(m Model, k string) (Model, tea.Cmd) {
	var msg tea.KeyPressMsg
	switch k {
	case "tab":
		msg = tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		msg = tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "down":
		msg = tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		msg = tea.KeyPressMsg{Code: tea.KeyUp}
	case "right":
		msg = tea.KeyPressMsg{Code: tea.KeyRight}
	case "left":
		msg = tea.KeyPressMsg{Code: tea.KeyLeft}
	case "enter":
		msg = tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		msg = tea.KeyPressMsg{Code: tea.KeyEscape}
	case "ctrl+c":
		msg = tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	default:
		msg = tea.KeyPressMsg{Code: rune(k[0]), Text: k}
	}
	nm, cmd := m.Update(msg)
	return nm.(Model), cmd
}

func TestKeys(t *testing.T) {
	m := demo(t, 160, 48)
	if m, _ = press(m, "tab"); m.view != vAgents {
		t.Fatalf("tab -> %v", m.view)
	}
	if m, _ = press(m, "shift+tab"); m.view != vOverview {
		t.Fatalf("shift+tab -> %v", m.view)
	}
	if m, _ = press(m, "shift+tab"); m.view != vSystem {
		t.Fatalf("shift+tab wraps -> %v", m.view)
	}
	if m, _ = press(m, "1"); m.view != vOverview {
		t.Fatal("1 did not jump")
	}
	if m, _ = press(m, "right"); m.view != vOverview || m.sel[vOverview] != 1 {
		t.Fatalf("right on Overview moves the card: view %v card %d", m.view, m.sel[vOverview])
	}
	if m, _ = press(m, "down"); m.sel[vOverview] != 4 {
		t.Fatalf("down on Overview -> card %d", m.sel[vOverview])
	}
	if m, _ = press(m, "enter"); m.view != vDone {
		t.Fatalf("enter on the How long it takes card -> %v", m.view)
	}
	m, _ = press(m, "2")
	if m, _ = press(m, "j"); m.sel[vAgents] != 1 || m.view != vAgents {
		t.Fatalf("j -> %d", m.sel[vAgents])
	}
	if m, _ = press(m, "right"); m.pane[vAgents] != 1 || m.view != vAgents {
		t.Fatal("right did not focus the preview")
	}
	m, _ = press(m, "left")
	if m, _ = press(m, "enter"); !m.modal || !strings.Contains(ansi.Strip(m.render()), "timeline") {
		t.Fatal("enter did not open the worker detail")
	}
	if m, _ = press(m, "tab"); m.view != vAgents {
		t.Fatal("tab switched views under the modal")
	}
	m, _ = press(m, "esc")
	first := m.rows(vAgents)[0].cells[0].s
	if m, _ = press(m, "s"); m.agentSort != 1 || m.rows(vAgents)[0].cells[0].s == first && false {
		t.Fatal("s did not change the sort")
	}
	if m, _ = press(m, "l"); !m.textOpen || !strings.Contains(ansi.Strip(m.render()), "[smelt]") {
		t.Fatal("l did not open the log")
	}
	m, _ = press(m, "esc")
	m, _ = press(m, "6")
	if m, _ = press(m, "t"); activityTypes[m.actType] != "poll" {
		t.Fatal("t did not switch the type")
	}
	for _, r := range m.rows(vActivity) {
		if !strings.HasSuffix(r.cells[1].s, "poll") {
			t.Fatalf("type filter let %q through", r.cells[1].s)
		}
	}
	m, _ = press(m, "4")
	m.sel[vNeeds] = 0
	if m, cmd := press(m, "y"); cmd == nil || !strings.Contains(m.status, "smithy decisions confirm 1") {
		t.Fatalf("y: %q", m.status)
	}
	m, _ = press(m, "8")
	if m, _ = press(m, "enter"); !m.textOpen || !strings.Contains(ansi.Strip(m.render()), "Concepts") {
		t.Fatal("enter on a plan did not open its spec")
	}
	m, _ = press(m, "esc")
	m, _ = press(m, "right")
	if m, _ = press(m, "enter"); !m.textOpen || !strings.Contains(ansi.Strip(m.render()), "Done when") {
		t.Fatal("enter on a task did not open its file")
	}
	m, _ = press(m, "esc")
	if m, _ = press(m, "?"); !m.helpOpen || !strings.Contains(ansi.Strip(m.render()), "Every smithy worktree") {
		t.Fatal("? did not open the help overlay")
	}
	m, _ = press(m, "esc")
	for _, k := range []string{"q", "ctrl+c"} {
		_, cmd := press(m, k)
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("%s did not quit", k)
		}
	}
}

func TestFilter(t *testing.T) {
	m := demo(t, 160, 48)
	m, _ = press(m, "3")
	m, _ = press(m, "/")
	for _, r := range "cold" {
		m, _ = press(m, string(r))
	}
	if rs := m.rows(vIncoming); len(rs) != 1 || !strings.Contains(rs[0].cells[1].s, "cold start") {
		t.Fatalf("filtered = %d", len(rs))
	}
	if m, _ = press(m, "q"); !m.filtering {
		t.Fatal("q typed into the filter should not quit")
	}
	m, _ = press(m, "esc")
	if len(m.rows(vIncoming)) != 6 {
		t.Fatal("esc did not clear the filter")
	}
}

func TestLogFromFile(t *testing.T) {
	fixture(t)
	m := model(160, 48)
	m, _ = press(m, "2")
	for i, r := range m.rows(vAgents) {
		if r.cells[1].s == "API" {
			m.sel[vAgents] = i
		}
	}
	if m, _ = press(m, "l"); !m.textOpen || !strings.Contains(ansi.Strip(m.render()), "opened PR #9") {
		t.Fatal("l did not open the worker log file")
	}
}

func TestPauseKey(t *testing.T) {
	fixture(t)
	m := model(160, 48)
	var got []bool
	m.pauser = func(on bool) error { got = append(got, on); return nil }
	_, cmd := press(m, "p")
	nm, cmd2 := m.Update(cmd())
	if len(got) != 1 || !got[0] || cmd2 == nil || !strings.Contains(nm.(Model).status, "paused") {
		t.Fatalf("p: %v %q", got, nm.(Model).status)
	}
	write(t, filepath.Join(os.Getenv("SMITHY_HOME"), "config.json"), `{}`)
	if err := setPaused(true); err != nil || !Load(fixedNow).Watcher.Paused {
		t.Fatalf("setPaused: %v", err)
	}
}

func TestMouse(t *testing.T) {
	m := demo(t, 160, 48)
	_, tabY, _, _ := m.frame()
	nm, _ := m.Update(tea.MouseClickMsg{X: 2 + ansi.StringWidth(m.tabLabel(vOverview)) + 2, Y: tabY})
	if m = nm.(Model); m.view != vAgents {
		t.Fatalf("tab click -> %v", m.view)
	}
	r := m.panes()["main"]
	y := r.y + 2 + 3
	nm, _ = m.Update(tea.MouseClickMsg{X: r.x + 5, Y: y})
	if m = nm.(Model); m.sel[vAgents] != 3 || m.modal {
		t.Fatalf("row click sel=%d", m.sel[vAgents])
	}
	nm, _ = m.Update(tea.MouseClickMsg{X: r.x + 5, Y: y})
	if !nm.(Model).modal {
		t.Fatal("double click did not open")
	}
	nm, _ = m.Update(tea.MouseWheelMsg{X: r.x + 5, Y: y, Button: tea.MouseWheelDown})
	if nm.(Model).sel[vAgents] != 4 {
		t.Fatal("wheel did not move")
	}
}

func TestToast(t *testing.T) {
	fixture(t)
	m := newModel(options{profile: colorprofile.TrueColor, dark: true, unicode: true, toast: true,
		now: func() time.Time { return fixedNow }, load: Load})
	m.width, m.height = 160, 48
	if !strings.Contains(ansi.Strip(m.render()), "tab switches views") {
		t.Fatal("first-run toast missing")
	}
	m.now = func() time.Time { return fixedNow.Add(5 * time.Second) }
	nm, _ := m.Update(tickMsg(fixedNow))
	if nm.(Model).toast != "" {
		t.Fatal("toast outlived 4 s")
	}
}

func TestSanitize(t *testing.T) {
	fixture(t)
	home := os.Getenv("SMITHY_HOME")
	write(t, filepath.Join(home, "auto", "state.json"), `{"version":1,"queue":[
{"source":"github","id":"1","title":"evil\u001b]52;c;aGk=\u0007 clip","url":"https://x.io/1\u001b[2J"},
{"source":"github","id":"2","title":"\u001b[2Jwipe\u009b31m\u0001","url":"javascript:alert(1)"}]}`)
	write(t, filepath.Join(home, "warden", "b1", "02-ui.json"), `{"state":"working","note":"n\u001b[31mred\u001b]8;;http://e\u0007","branch":"x\u001b[2J"}`)
	write(t, filepath.Join(home, "warden", "b1", "01-api.log"), "ok\n\u001b]52;c;aGk=\u0007boom\n")
	write(t, filepath.Join(home, "events.jsonl"), `{"t":"2026-11-23T14:00:00Z","kind":"queued","id":"\u001b[2J9","detail":"x\u009b2J"}`+"\n")
	pendingDecisions = func() ([]string, error) { return []string{"d \u001b[2J· x"}, errors.New("bad\u001b[2J") }
	s := Load(fixedNow)
	var all []string
	for _, q := range s.Queue {
		all = append(all, q.Title, q.URL, q.ID, q.Source)
	}
	for _, b := range s.Batches {
		for _, it := range b.Items {
			all = append(all, it.Text, it.State, it.Title, it.Branch)
			for _, kv := range it.Card {
				all = append(all, kv[0], kv[1])
			}
		}
	}
	for _, e := range s.Events {
		all = append(all, e.ID, e.Detail)
	}
	all = append(all, tail(s.Batches[0].Items[0].Log, 15)...)
	all = append(all, s.Decisions...)
	all = append(all, s.Errors...)
	for _, x := range all {
		for _, r := range x {
			if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
				t.Fatalf("control char %q survived in %q", r, x)
			}
		}
	}
	if s.Queue[0].Title != "evil clip" || s.Queue[1].Title != "wipe" {
		t.Fatalf("titles = %q %q", s.Queue[0].Title, s.Queue[1].Title)
	}
}

func TestOpenRequiresWebURL(t *testing.T) {
	fixture(t)
	home := os.Getenv("SMITHY_HOME")
	write(t, filepath.Join(home, "auto", "state.json"), `{"version":1,"queue":[
{"source":"github","id":"1","title":"a","url":"javascript:alert(1)"},
{"source":"github","id":"2","title":"b","url":"https:///nohost"},
{"source":"github","id":"3","title":"c","url":"file:///etc/passwd"},
{"source":"github","id":"4","title":"d","url":"http://ok.example/4"}]}`)
	m := model(160, 48)
	m.view = vIncoming
	var opened []string
	m.opener = func(u string) error { opened = append(opened, u); return nil }
	for i := 0; i < 3; i++ {
		m.sel[vIncoming] = i
		nm, cmd := press(m, "o")
		if cmd != nil || nm.status != "not a web URL" {
			t.Fatalf("row %d: cmd=%v status=%q", i, cmd != nil, nm.status)
		}
	}
	m.sel[vIncoming] = 3
	_, cmd := press(m, "o")
	cmd()
	if len(opened) != 1 || opened[0] != "http://ok.example/4" {
		t.Fatalf("opened %q", opened)
	}
}

func TestMarkdownLinksShowTheirTarget(t *testing.T) {
	m := demo(t, 160, 48)
	m.showMarkdown("t", "see [click here](http://evil.example/x)")
	c := m.text.GetContent()
	if strings.Contains(c, "\x1b]8") || !strings.Contains(ansi.Strip(c), "evil.example/x") {
		t.Fatalf("hyperlink hides its target: %q", c)
	}
}

func TestDemoOpenOnlySays(t *testing.T) {
	m := demo(t, 160, 48)
	m.view = vIncoming
	opened := false
	m.opener = func(string) error { opened = true; return nil }
	nm, cmd := press(m, "o")
	if cmd != nil || opened || !strings.HasPrefix(nm.toast, "demo: would open ") {
		t.Fatalf("cmd=%v opened=%v toast=%q", cmd != nil, opened, nm.toast)
	}
}

func TestShellQuote(t *testing.T) {
	for in, want := range map[string]string{
		"plain/path-1.2":   "plain/path-1.2",
		"https://x/pull/3": "https://x/pull/3",
		"a b":              "'a b'",
		"it's; rm -rf ~":   `'it'\''s; rm -rf ~'`,
		"$(touch x)":       "'$(touch x)'",
		"":                 "''",
	} {
		if got := sh(in); got != want {
			t.Errorf("sh(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOverlappingLoads(t *testing.T) {
	fixture(t)
	m := model(160, 48)
	nm, cmd := m.Update(tickMsg(fixedNow))
	m = nm.(Model)
	if !m.loading || cmd == nil {
		t.Fatal("tick did not start a load")
	}
	calls := 0
	m.load = func(t time.Time) Snapshot { calls++; return Snapshot{At: t} }
	nm, _ = m.Update(tickMsg(fixedNow))
	if !nm.(Model).loading || calls != 0 {
		t.Fatal("second tick loaded while one was in flight")
	}
	nm, _ = m.Update(snapMsg(Snapshot{At: fixedNow.Add(-time.Minute)}))
	if m = nm.(Model); !m.snap.At.Equal(fixedNow) || m.loading {
		t.Fatalf("older snapshot applied (at %v) or loading kept", m.snap.At)
	}
	nm, _ = m.Update(snapMsg(Snapshot{At: fixedNow.Add(time.Second)}))
	if !nm.(Model).snap.At.Equal(fixedNow.Add(time.Second)) {
		t.Fatal("newer snapshot dropped")
	}
}

func TestResize(t *testing.T) {
	m := demo(t, 160, 48)
	for v := view(0); v < nViews; v++ {
		m.view = v
		for _, sz := range [][2]int{{60, 20}, {100, 30}, {220, 60}} {
			nm, _ := m.Update(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
			out := ansi.Strip(nm.(Model).render())
			if n := len(strings.Split(out, "\n")); n != sz[1] {
				t.Fatalf("%s at %dx%d rendered %d lines", viewNames[v], sz[0], sz[1], n)
			}
		}
	}
}

func TestAnvilShape(t *testing.T) {
	for _, s := range []sprite{bigAnvil, smallAnvil} {
		widths := make([]int, s.h())
		for y, row := range s.mask {
			widths[y] = strings.Count(row, "#")
		}
		face, waist := 0, s.w()
		for y := s.faceY; y < s.faceY+3; y++ {
			face = max(face, widths[y])
		}
		for _, w := range widths[s.faceY+3:] {
			waist = min(waist, w)
		}
		base := widths[s.h()-2]
		if !(face > base && base > 2*waist && waist > 0) {
			t.Fatalf("not face > base > 2×waist: %v", widths)
		}
		horn := s.mask[s.faceY+1]
		if horn[0] != '#' || s.mask[s.faceY][0] == '#' || s.mask[s.faceY+2][0] == '#' {
			t.Fatal("the horn is not a point on the left")
		}
		heel := strings.LastIndex(s.mask[s.faceY], "#")
		if strings.LastIndex(s.mask[s.faceY+1], "#") != heel || strings.LastIndex(s.mask[s.faceY+2], "#") != heel {
			t.Fatal("the heel is not square on the right")
		}
		if feet := strings.TrimSpace(s.mask[s.h()-1]); !strings.Contains(feet, " ") {
			t.Fatal("the base has no feet")
		}
	}
	m := demo(t, 160, 48)
	if rows := m.anvil(bigAnvil); len(rows) != 7 || ansi.StringWidth(rows[3]) != 30 {
		t.Fatalf("big anvil is %d rows", len(rows))
	}
}

func TestHeaderFrameDeterministic(t *testing.T) {
	frameAt := func(seed uint64, steps int) string {
		m := demo(t, 160, 48)
		m.motion = true
		m.fx = newFX(seed)
		m.strike(10)
		for i := 0; i < steps; i++ {
			m.fx.step()
		}
		return strings.Join(m.anvil(bigAnvil), "\n") + strings.Join(m.wordmark(), "\n")
	}
	a := frameAt(42, 6)
	if a != frameAt(42, 6) || a == frameAt(43, 6) {
		t.Fatal("frames are not a function of the seed")
	}
	still := demo(t, 160, 48)
	if a == strings.Join(still.anvil(bigAnvil), "\n")+strings.Join(still.wordmark(), "\n") {
		t.Fatal("a mid-spark frame equals the still one")
	}
	for _, w := range []int{160, 110, 109, 80, 79} {
		m := demo(t, w, 40)
		head := m.header(w)
		if len(head) > 7 || !strings.Contains(ansi.Strip(strings.Join(head, "\n")), strings.TrimRight(wordB[0], " ")) {
			t.Fatalf("width %d: header %d rows or name cut", w, len(head))
		}
		if !strings.Contains(ansi.Strip(strings.Join(head, "\n")), "DEMO") && w >= 80 {
			t.Fatalf("width %d: no DEMO pill", w)
		}
	}
}

func TestMotionStopsWhenIdle(t *testing.T) {
	m := demo(t, 160, 48)
	m.motion = true
	nm, cmd := m.Update(strikeMsg{})
	m = nm.(Model)
	if cmd == nil || !m.framing || len(m.fx.sparks) == 0 {
		t.Fatal("strike did not start frames and sparks")
	}
	for i := 0; i < 400 && m.framing; i++ {
		nm, _ = m.Update(frameMsg{})
		m = nm.(Model)
	}
	if m.framing || m.fx.busy() {
		t.Fatal("frames keep running after the sparks died")
	}
	still := demo(t, 160, 48)
	if nm, cmd := still.Update(strikeMsg{}); cmd != nil || nm.(Model).framing {
		t.Fatal("reduced motion scheduled frames")
	}
}

func TestNoColor(t *testing.T) {
	utc(t)
	m := newModel(options{profile: colorprofile.Ascii, dark: true, unicode: false,
		now: func() time.Time { return fixedNow }, load: Demo})
	m.width, m.height = 160, 48
	for v := view(0); v < nViews; v++ {
		m.view = v
		out := m.render()
		if strings.Contains(out, "\x1b[38") || strings.Contains(out, "\x1b[48") {
			t.Fatalf("%s: NO_COLOR output carries colors", viewNames[v])
		}
		for _, r := range ansi.Strip(out) {
			if (r >= 0x2500 && r <= 0x28ff) || strings.ContainsRune("●◐○✓✗▸■□", r) {
				t.Fatalf("%s: ASCII mode drew %q", viewNames[v], r)
			}
		}
	}
}

func TestDecisionActions(t *testing.T) {
	m := demo(t, 160, 48)
	var got []string
	m.confirm = func(n int) error { got = append(got, fmt.Sprint("confirm ", n)); return nil }
	m.reject = func(n int, why string) error { got = append(got, fmt.Sprint("reject ", n, " ", why)); return nil }
	m, _ = press(m, "4")
	m, _ = press(m, "j")
	m, cmd := press(m, "a")
	nm, _ := m.Update(cmd())
	if m = nm.(Model); len(got) != 1 || got[0] != "confirm 2" || !strings.Contains(m.toast, "accepted decision 2") {
		t.Fatalf("accept: %v %q", got, m.toast)
	}
	m, _ = press(m, "x")
	if !m.prompting || !strings.Contains(ansi.Strip(m.render()), "why reject?") {
		t.Fatal("x did not ask for a reason")
	}
	if m, cmd = press(m, "enter"); cmd != nil || !m.prompting {
		t.Fatal("an empty reason was accepted")
	}
	for _, r := range "too risky" {
		m, _ = press(m, string(r))
	}
	m, cmd = press(m, "enter")
	nm, _ = m.Update(cmd())
	if m = nm.(Model); len(got) != 2 || got[1] != "reject 2 too risky" || !strings.Contains(m.toast, "decisions pending") {
		t.Fatalf("reject: %v %q", got, m.toast)
	}
	m.sel[vNeeds] = 3
	if m, _ = press(m, "a"); !strings.Contains(m.status, "not one") {
		t.Fatalf("a on a PR row: %q", m.status)
	}
	m, _ = press(m, "enter")
	out := ansi.Strip(m.render())
	for _, want := range []string{"never merges from here", "o open", "y copy command"} {
		if !strings.Contains(out, want) {
			t.Fatalf("PR modal lacks %q", want)
		}
	}
	demoM := New(true, true)
	demoM.width, demoM.height = 160, 48
	demoM, _ = press(demoM, "4")
	if demoM, _ = press(demoM, "a"); !strings.Contains(demoM.toast, "demo: would run smithy decisions confirm 1") {
		t.Fatalf("demo accept toast %q", demoM.toast)
	}
	events.Off = false
}

func TestDecisionModalExplains(t *testing.T) {
	m := demo(t, 160, 48)
	m, _ = press(m, "4")
	m, _ = press(m, "enter")
	out := ansi.Strip(m.render())
	for _, want := range []string{"Decision 1", "decide", "after nobody answered in time", "a accepts it", "x rejects it", "smithy decisions pending", "a accept", "x reject"} {
		if !strings.Contains(out, want) {
			t.Fatalf("decision modal lacks %q:\n%s", want, out)
		}
	}
	if rs := m.rows(vNeeds); rs[2].chip != "rejected" {
		t.Fatalf("a rejected decision shows %q", rs[2].chip)
	}
}

func TestDone(t *testing.T) {
	m := demo(t, 160, 48)
	rs := m.rows(vDone)
	results := map[string]int{}
	for _, r := range rs {
		results[r.cells[3].s]++
	}
	for _, want := range []string{"merged", "ready", "closed", "blocked"} {
		if results[want] == 0 {
			t.Errorf("Done lacks %s: %v", want, results)
		}
	}
	m.filter[vDone] = "closed"
	for _, r := range m.rows(vDone) {
		if r.cells[3].s != "closed" && !strings.Contains(strings.ToLower(r.cells[1].s), "closed") {
			t.Fatalf("filter let %v through", r.cells)
		}
	}
	fixture(t)
	f := model(160, 48)
	if rs := f.rows(vDone); len(rs) == 0 || rs[0].cells[3].s != "ready" || rs[0].cells[4].s != "1h10m" {
		t.Fatalf("fixture Done = %+v", rs)
	}
}

func TestSystemView(t *testing.T) {
	m := demo(t, 160, 48)
	m, cmd := press(m, "9")
	if cmd == nil || m.view != vSystem || !strings.Contains(ansi.Strip(m.render()), "Running smithy doctor") {
		t.Fatal("System did not start loading")
	}
	nm, _ := m.Update(cmd())
	m = nm.(Model)
	out := ansi.Strip(m.render())
	for _, want := range []string{"Versions", "Doctor", "auto.*", "linear.*", "warden.*", "****9f2c", "events.jsonl", "Read-only"} {
		if !strings.Contains(out, want) {
			t.Fatalf("System lacks %q", want)
		}
	}
	if _, cmd := press(m, "8"); cmd != nil {
		t.Fatal("leaving System reloads it")
	}
	if strings.Contains(out, "9f2c3") {
		t.Fatal("auth key not masked")
	}
	fixture(t)
	s := loadSystem()
	if len(s.Doctor) != 0 {
		t.Fatal("loadSystem ran a binary that is not smithy")
	}
	if len(s.Settings) == 0 || !strings.HasPrefix(s.Settings[0][0], "auto.") {
		t.Fatalf("settings = %v", s.Settings)
	}
}

func TestPillBadgeInside(t *testing.T) {
	m := demo(t, 160, 48)
	m.view = vNeeds
	n := len(m.rows(vNeeds))
	if l := m.tabLabel(vNeeds); !strings.HasSuffix(l, fmt.Sprintf("%d ", n)) || !strings.HasPrefix(l, " 4 Needs you") {
		t.Fatalf("pill = %q", l)
	}
	_, tabY, _, _ := m.frame()
	bar := strings.Split(ansi.Strip(m.render()), "\n")[tabY]
	if !strings.Contains(bar, fmt.Sprintf(" 4 Needs you %d   5 Done", n)) {
		t.Fatalf("tab bar = %q", bar)
	}
}

func TestWorkChart(t *testing.T) {
	if niceTop(13) != 20 || niceTop(5) != 5 || niceTop(51) != 100 || niceTop(0) != 1 {
		t.Fatal("niceTop")
	}
	m := demo(t, 160, 48)
	lines, _ := m.cardThroughput(49, 18)
	out := ansi.Strip(strings.Join(lines, "\n"))
	for _, want := range []string{"launched", "ready/merged", "20┤", "0┼", "-12h", "-6h", "now", "·"} {
		if !strings.Contains(out, want) {
			t.Fatalf("chart lacks %q:\n%s", want, out)
		}
	}
	small, _ := m.cardThroughput(40, 6)
	if !strings.Contains(ansi.Strip(strings.Join(small, "\n")), "up ") {
		t.Fatal("no compact fallback under 48 columns")
	}
}

func TestMarkdownReaderFromFile(t *testing.T) {
	fixture(t)
	dir := Load(fixedNow).Tasks[0].Dir
	write(t, filepath.Join(dir, "spec.md"), "# The spec\n\n## Why\nBecause.\n\u001b]52;c;aGk=\u0007")
	m := model(160, 48)
	m, _ = press(m, "8")
	m, _ = press(m, "enter")
	out := m.render()
	if !m.textOpen || !strings.Contains(ansi.Strip(out), "The spec") || strings.Contains(out, "]52;") {
		t.Fatal("spec.md not rendered, or its escapes reached the screen")
	}
}
