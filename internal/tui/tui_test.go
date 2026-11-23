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

// fixture builds a smithy home with an auto state, a batch of three workers (one PR ready, one
// stuck, one queued), a task list, a decision, two worktrees (one orphaned) and a worker log.
func fixture(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SMITHY_HOME", home)
	t.Setenv("WARDEN_HOME", "")
	old := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = old })
	write(t, filepath.Join(home, "auto", "state.json"), `{"version":1,
"queue":[{"source":"github","key":"github","id":"42","title":"Add dark mode to the report","url":"https://github.com/o/r/issues/42","model":"mid","speed":"normal","queued_at":"2026-11-23T13:00:00Z","body":"The report is too bright.\nAdd a dark theme."},
{"source":"linear","key":"linear","id":"TES-7","title":"Fix flaky login test","url":"https://linear.app/x/issue/TES-7","queued_at":"2026-11-23T14:10:00Z"}],
"launched":{"2026-11-23":[{"source":"github","id":"40","batch":"b1","item":"01-api","at":"2026-11-23T09:10:00Z"},{"source":"github","id":"41","batch":"b1","item":"02-ui","at":"2026-11-23T13:20:00Z"}]},
"last_poll":{"github":"2026-11-23T14:29:30Z"}}`)
	b := filepath.Join(home, "warden", "b1")
	write(t, filepath.Join(b, "batch.json"), `{"items":[
{"id":"01-api","title":"API","route":"strong","source":"github#40","branch":"feat/api","worktree":"/w/api"},
{"id":"02-ui","title":"UI","route":"cheap","source":"github#41","branch":"feat/ui","worktree":"/w/ui"},
{"id":"03-db","title":"DB","route":"mid"}]}`)
	write(t, filepath.Join(b, "01-api.json"), `{"state":"pr_open","phase":"ship","pr":"https://github.com/o/r/pull/9","ci":"green","updated":"2026-11-23T14:20:00Z","last_beat":"2026-11-23T14:28:00Z"}`)
	write(t, filepath.Join(b, "02-ui.json"), `{"state":"blocked","phase":"build","note":"tests need a DB","updated":"2026-11-23T14:00:00Z"}`)
	write(t, filepath.Join(b, "01-api.log"), "step 1\nstep 2\nopened PR #9\n")
	tdir := filepath.Join(home, "plans", "repo", "v1")
	write(t, filepath.Join(tdir, "01-a.md"), "# 01 · A\nStatus: done\n")
	write(t, filepath.Join(tdir, "02-b.md"), "# 02 · B\nStatus: doing\n")
	write(t, filepath.Join(tdir, "03-c.md"), "# 03 · C\nStatus: todo\n")
	oldDirs, oldDec, oldWT, oldRepo := taskDirs, pendingDecisions, scanWorktrees, repoName
	taskDirs = func() []string { return []string{tdir} }
	pendingDecisions = func() ([]string, error) {
		return []string{"2026-11-22 · timeout · Use Postgres? → yes · fits the stack · ref: https://github.com/o/r/pull/9"}, nil
	}
	scanWorktrees = func([]Batch, time.Time) []Worktree {
		return []Worktree{{Path: "/w/api", Branch: "feat/api", Item: "b1/01-api", Age: 25 * time.Minute},
			{Path: "/w/old", Branch: "feat/old", Age: 49 * time.Hour, Dirty: true, Orphan: true, Status: []string{" M main.go"}}}
	}
	repoName = func() string { return "smithy" }
	t.Cleanup(func() { taskDirs, pendingDecisions, scanWorktrees, repoName = oldDirs, oldDec, oldWT, oldRepo })
}

func model(w, h int) Model {
	m := newModel(options{profile: colorprofile.TrueColor, dark: true, unicode: true, seed: 7,
		now: func() time.Time { return fixedNow }, load: Load})
	m.width, m.height = w, h
	m.opener = func(string) error { return nil }
	m.pauser = func(bool) error { return nil }
	return m
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
	if it.Stage != "verify" || it.Model != "strong" || it.Branch != "feat/api" || it.LastBeat == "" || !strings.HasSuffix(it.Log, "01-api.log") {
		t.Fatalf("item = %+v", it)
	}
	if tl := s.Tasks[0]; tl.Total != 3 || tl.Counts["done"] != 1 {
		t.Fatalf("tasks = %+v", tl)
	}
	if len(s.Activity) < 5 || !s.Activity[0].At.After(s.Activity[len(s.Activity)-1].At) {
		t.Fatalf("activity = %+v", s.Activity)
	}
	if len(s.Worktrees) != 2 || s.Repo != "smithy" || len(s.Decisions) != 1 || len(s.Errors) != 0 {
		t.Fatalf("worktrees/repo/decisions/errors = %v %q %v %v", s.Worktrees, s.Repo, s.Decisions, s.Errors)
	}
	if Load(fixedNow.Add(time.Hour)).Watcher.Alive {
		t.Fatal("watcher should be stale an hour later")
	}
}

func TestPausedSetting(t *testing.T) {
	fixture(t)
	write(t, filepath.Join(os.Getenv("SMITHY_HOME"), "config.json"), `{"auto":{"paused":"true"}}`)
	if err := setPaused(true); err != nil {
		t.Fatal(err)
	}
	if !Load(fixedNow).Watcher.Paused {
		t.Fatal("auto.paused not read")
	}
	if err := setPaused(false); err != nil || Load(fixedNow).Watcher.Paused {
		t.Fatalf("unpause: %v", err)
	}
}

func TestLoadEmpty(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	t.Setenv("WARDEN_HOME", "")
	oldDirs, oldDec, oldWT := taskDirs, pendingDecisions, scanWorktrees
	taskDirs = func() []string { return nil }
	pendingDecisions = func() ([]string, error) { return nil, nil }
	scanWorktrees = func([]Batch, time.Time) []Worktree { return nil }
	t.Cleanup(func() { taskDirs, pendingDecisions, scanWorktrees = oldDirs, oldDec, oldWT })
	m := model(140, 45)
	out := ansi.Strip(m.render())
	for _, want := range []string{"not running", "start: tend auto", "smithy auto poll", "Nothing needs you", "smithy warden status"} {
		if !strings.Contains(out, want) {
			t.Errorf("empty view lacks %q:\n%s", want, out)
		}
	}
}

func TestGolden(t *testing.T) {
	fixture(t)
	for _, size := range [][2]int{{140, 45}, {100, 30}, {70, 24}} {
		m := model(size[0], size[1])
		m.focus = pAgents
		got := ansi.Strip(m.View().Content)
		lines := strings.Split(got, "\n")
		if len(lines) != size[1] {
			t.Errorf("%dx%d: %d lines", size[0], size[1], len(lines))
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w > size[0] {
				t.Errorf("%dx%d: line %d is %d wide", size[0], size[1], i, w)
			}
		}
		file := filepath.Join("testdata", fmt.Sprintf("%dx%d.golden", size[0], size[1]))
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
	case "backspace":
		msg = tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "ctrl+c":
		msg = tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	default:
		msg = tea.KeyPressMsg{Code: rune(k[0]), Text: k}
	}
	nm, cmd := m.Update(msg)
	return nm.(Model), cmd
}

func TestKeys(t *testing.T) {
	fixture(t)
	m := model(140, 45)
	if m, _ = press(m, "tab"); m.focus != pAgents {
		t.Fatalf("tab -> %v", m.focus)
	}
	if m, _ = press(m, "shift+tab"); m.focus != pIncoming {
		t.Fatalf("shift+tab -> %v", m.focus)
	}
	if m, _ = press(m, "left"); m.focus != pPlans {
		t.Fatalf("left wraps -> %v", m.focus)
	}
	if m, _ = press(m, "5"); m.focus != pWorktrees {
		t.Fatalf("5 -> %v", m.focus)
	}
	m, _ = press(m, "1")
	if m, _ = press(m, "j"); m.sel[pIncoming] != 1 {
		t.Fatalf("j -> %d", m.sel[pIncoming])
	}
	if m, _ = press(m, "down"); m.sel[pIncoming] != 1 {
		t.Fatal("down past the end moved")
	}
	if m, _ = press(m, "k"); m.sel[pIncoming] != 0 {
		t.Fatal("k did not move")
	}
	if m, _ = press(m, "enter"); !m.modal || !strings.Contains(ansi.Strip(m.render()), "Add a dark theme") {
		t.Fatal("enter did not open the issue detail with its text")
	}
	if m, _ = press(m, "tab"); m.focus != pIncoming {
		t.Fatal("tab moved focus under the modal")
	}
	if m, _ = press(m, "esc"); m.modal {
		t.Fatal("esc did not close the modal")
	}
	var opened string
	m.opener = func(u string) error { opened = u; return nil }
	m, cmd := press(m, "o")
	if cmd == nil {
		t.Fatal("o returned no command")
	}
	cmd()
	if opened != "https://github.com/o/r/issues/42" {
		t.Fatalf("opened %q", opened)
	}
	if m, cmd = press(m, "c"); cmd == nil || !strings.HasPrefix(m.status, "copied https://github.com/o/r/issues/42") {
		t.Fatalf("c: %q", m.status)
	}
	m.focus = pPlans
	if m, cmd = press(m, "o"); cmd != nil || m.status != "no URL for this row" {
		t.Fatalf("o on a row without URL: %q", m.status)
	}
	if m, _ = press(m, "?"); !m.helpOpen || !strings.Contains(ansi.Strip(m.render()), "Every smithy worktree") {
		t.Fatal("? did not open the help overlay")
	}
	m, _ = press(m, "esc")
	if _, cmd = press(m, "r"); cmd == nil {
		t.Fatal("r returned no command")
	} else if _, ok := cmd().(snapMsg); !ok {
		t.Fatal("r did not reload")
	}
	for _, k := range []string{"q", "ctrl+c"} {
		_, cmd := press(m, k)
		if cmd == nil {
			t.Fatalf("%s returned no command", k)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("%s did not quit", k)
		}
	}
}

func TestFilter(t *testing.T) {
	fixture(t)
	m := model(140, 45)
	m, _ = press(m, "1")
	m, _ = press(m, "/")
	if !m.filtering {
		t.Fatal("/ did not start filtering")
	}
	for _, r := range "flaky" {
		m, _ = press(m, string(r))
	}
	if es := m.visible(pIncoming); len(es) != 1 || !strings.Contains(es[0].text, "TES-7") {
		t.Fatalf("filtered = %+v", es)
	}
	if m, _ = press(m, "q"); m.focus != pIncoming || !m.filtering {
		t.Fatal("q typed into the filter should not quit")
	}
	m, _ = press(m, "enter")
	if m.filtering || len(m.visible(pIncoming)) != 0 {
		t.Fatalf("after enter: filtering=%v visible=%d (filter %q)", m.filtering, len(m.visible(pIncoming)), m.filter[pIncoming])
	}
	m, _ = press(m, "esc")
	if len(m.visible(pIncoming)) != 2 {
		t.Fatal("esc did not clear the filter")
	}
}

func TestLogView(t *testing.T) {
	fixture(t)
	m := model(140, 45)
	m, _ = press(m, "2")
	if m, _ = press(m, "l"); !m.logOpen || !strings.Contains(ansi.Strip(m.render()), "opened PR #9") {
		t.Fatal("l did not open the worker log")
	}
	if m, _ = press(m, "esc"); m.logOpen {
		t.Fatal("esc did not close the log")
	}
	m, _ = press(m, "j")
	if m, _ = press(m, "l"); m.logOpen || !strings.HasPrefix(m.status, "no log yet") {
		t.Fatalf("missing log: %q", m.status)
	}
}

func TestPauseKey(t *testing.T) {
	fixture(t)
	m := model(140, 45)
	var got []bool
	m.pauser = func(on bool) error { got = append(got, on); return nil }
	m, cmd := press(m, "p")
	nm, cmd2 := m.Update(cmd())
	if len(got) != 1 || !got[0] || cmd2 == nil || !strings.Contains(nm.(Model).status, "paused") {
		t.Fatalf("p: %v %q", got, nm.(Model).status)
	}
}

func TestMouse(t *testing.T) {
	fixture(t)
	m := model(140, 45)
	_, tabY, rects, _ := m.layout()
	r := rects[pNeeds]
	nm, _ := m.Update(tea.MouseClickMsg{X: r.x + 3, Y: r.y + 1})
	m = nm.(Model)
	if m.focus != pNeeds || m.sel[pNeeds] != 0 {
		t.Fatalf("click focus=%v sel=%d", m.focus, m.sel[pNeeds])
	}
	_, owner := m.panelBody(pNeeds, r.w-4, r.h-2)
	y := -1
	for i, o := range owner {
		if o == 1 {
			y = r.y + 1 + i
			break
		}
	}
	nm, _ = m.Update(tea.MouseClickMsg{X: r.x + 3, Y: y})
	m = nm.(Model)
	if m.sel[pNeeds] != 1 || m.modal {
		t.Fatalf("second row click sel=%d modal=%v", m.sel[pNeeds], m.modal)
	}
	nm, _ = m.Update(tea.MouseClickMsg{X: r.x + 3, Y: y})
	if !nm.(Model).modal {
		t.Fatal("double click did not open")
	}
	nm, _ = m.Update(tea.MouseClickMsg{X: 1 + ansi.StringWidth(m.tabLabel(pIncoming)) + 3, Y: tabY})
	if nm.(Model).focus != pAgents {
		t.Fatalf("tab click focus=%v", nm.(Model).focus)
	}
	a := rects[pAgents]
	nm, _ = m.Update(tea.MouseWheelMsg{X: a.x + 2, Y: a.y + 2, Button: tea.MouseWheelDown})
	if mm := nm.(Model); mm.focus != pAgents || mm.sel[pAgents] != 1 {
		t.Fatalf("wheel focus=%v sel=%d", mm.focus, mm.sel[pAgents])
	}
}

func TestToast(t *testing.T) {
	fixture(t)
	m := newModel(options{profile: colorprofile.TrueColor, dark: true, unicode: true, toast: true,
		now: func() time.Time { return fixedNow }, load: Load})
	m.width, m.height = 140, 45
	if !strings.Contains(ansi.Strip(m.render()), "tab or 1-6 to move") {
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
	for _, e := range s.Activity {
		all = append(all, e.Text)
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
	m := model(140, 45)
	m.focus = pIncoming
	var opened []string
	m.opener = func(u string) error { opened = append(opened, u); return nil }
	for i := 0; i < 3; i++ {
		m.sel[pIncoming] = i
		nm, cmd := press(m, "o")
		if cmd != nil || nm.status != "not a web URL" {
			t.Fatalf("row %d: cmd=%v status=%q", i, cmd != nil, nm.status)
		}
	}
	m.sel[pIncoming] = 3
	_, cmd := press(m, "o")
	cmd()
	if len(opened) != 1 || opened[0] != "http://ok.example/4" {
		t.Fatalf("opened %q", opened)
	}
}

func TestOverlappingLoads(t *testing.T) {
	fixture(t)
	m := model(140, 45)
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
	old := Snapshot{At: fixedNow.Add(-time.Minute)}
	nm, _ = m.Update(snapMsg(old))
	if m = nm.(Model); !m.snap.At.Equal(fixedNow) || m.loading {
		t.Fatalf("older snapshot applied (at %v) or loading kept", m.snap.At)
	}
	nm, _ = m.Update(snapMsg(Snapshot{At: fixedNow.Add(time.Second)}))
	if !nm.(Model).snap.At.Equal(fixedNow.Add(time.Second)) {
		t.Fatal("newer snapshot dropped")
	}
}

func TestModalFloats(t *testing.T) {
	fixture(t)
	m := model(140, 45)
	m, _ = press(m, "2")
	m, _ = press(m, "enter")
	out := ansi.Strip(m.render())
	for _, want := range []string{"b1/01-api", "last beat", "opened PR #9", "Incoming", "Worktrees"} {
		if !strings.Contains(out, want) {
			t.Fatalf("modal lacks %q or does not float over the dashboard:\n%s", want, out)
		}
	}
	if n := len(strings.Split(out, "\n")); n != 45 {
		t.Fatalf("modal view has %d lines", n)
	}
}

func TestResize(t *testing.T) {
	fixture(t)
	m := model(140, 45)
	for _, sz := range [][2]int{{60, 20}, {100, 30}, {200, 60}} {
		nm, _ := m.Update(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
		out := ansi.Strip(nm.(Model).render())
		if n := len(strings.Split(out, "\n")); n != sz[1] {
			t.Fatalf("%dx%d rendered %d lines", sz[0], sz[1], n)
		}
	}
}

func TestAnvilReadsAsAnvil(t *testing.T) {
	fixture(t)
	for _, s := range []sprite{bigAnvil, smallAnvil} {
		widths := make([]int, s.h())
		for y, row := range s.mask {
			widths[y] = strings.Count(row, "#")
		}
		face, waist, base := widths[s.faceY], widths[s.h()-1], widths[s.h()-1]
		for _, w := range widths[s.faceY:] {
			waist = min(waist, w)
		}
		if !(face > base && base > waist && waist > 0) {
			t.Fatalf("silhouette is not face > base > waist: %v", widths)
		}
		if s.mask[s.faceY][0:3] == "###" || s.mask[s.faceY][s.w()-1] == '#' {
			t.Fatal("no margin around the horn or heel")
		}
	}
	m := model(140, 45)
	if rows := m.anvil(bigAnvil); len(rows) != 7 || ansi.StringWidth(rows[3]) != 30 {
		t.Fatalf("big anvil is %d rows, %d wide", len(rows), ansi.StringWidth(rows[3]))
	}
}

func TestHeaderFrameDeterministic(t *testing.T) {
	fixture(t)
	frameAt := func(seed uint64, steps int) string {
		m := model(140, 45)
		m.motion = true
		m.fx = newFX(seed)
		m.strike(10)
		for i := 0; i < steps; i++ {
			m.fx.step()
		}
		return strings.Join(m.anvil(bigAnvil), "\n") + strings.Join(m.wordmark(), "\n")
	}
	a := frameAt(42, 6)
	if a != frameAt(42, 6) {
		t.Fatal("same seed, different frames")
	}
	if a == frameAt(43, 6) {
		t.Fatal("different seeds drew the same sparks")
	}
	still := model(140, 45)
	if a == strings.Join(still.anvil(bigAnvil), "\n")+strings.Join(still.wordmark(), "\n") {
		t.Fatal("a mid-spark frame equals the still one")
	}
	for _, w := range []int{140, 110, 109, 80, 79} {
		m := model(w, 40)
		head := m.header(w)
		if n := len(head); n > 7 {
			t.Fatalf("width %d: header is %d rows", w, n)
		}
		if !strings.Contains(ansi.Strip(strings.Join(head, "\n")), strings.TrimRight(wordB[0], " ")) {
			t.Fatalf("width %d: name cut", w)
		}
	}
}

func TestMotionStopsWhenIdle(t *testing.T) {
	fixture(t)
	m := model(140, 45)
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
	still := model(140, 45)
	if nm, cmd := still.Update(strikeMsg{}); cmd != nil || nm.(Model).framing {
		t.Fatal("reduced motion scheduled frames")
	}
}

func TestNoColor(t *testing.T) {
	fixture(t)
	m := newModel(options{profile: colorprofile.Ascii, dark: true, unicode: false,
		now: func() time.Time { return fixedNow }, load: Load})
	m.width, m.height = 140, 45
	out := m.render()
	if strings.Contains(out, "\x1b[38") || strings.Contains(out, "\x1b[48") {
		i := strings.Index(out, "\x1b[38")
		t.Fatalf("NO_COLOR output carries colors: %q", out[max(i-40, 0):min(i+40, len(out))])
	}
	for _, r := range ansi.Strip(out) {
		if (r >= 0x2500 && r <= 0x28ff) || strings.ContainsRune("●◐○✓✗▸", r) {
			t.Fatalf("ASCII mode drew %q", r)
		}
	}
}
