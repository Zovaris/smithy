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

// fixture builds a smithy home with an auto state, one batch, one task list and two decisions.
func fixture(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SMITHY_HOME", home)
	t.Setenv("WARDEN_HOME", "")
	old := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = old })
	write(t, filepath.Join(home, "auto", "state.json"), `{"version":1,
"queue":[{"source":"github","key":"github","id":"42","title":"Add dark mode to the report","url":"https://github.com/o/r/issues/42","model":"mid","speed":"normal","queued_at":"2026-11-23T13:00:00Z"},
{"source":"linear","key":"linear","id":"ENG-7","title":"Fix flaky login test","url":"https://linear.app/x/issue/ENG-7"}],
"launched":{"2026-11-23":[{"source":"github","id":"40","at":"2026-11-23T09:10:00Z"},{"source":"github","id":"41","at":"2026-11-23T13:20:00Z"},{"source":"github","id":"39","at":"2026-11-23T13:40:00Z"}]},
"last_poll":{"github":"2026-11-23T14:29:30Z"}}`)
	b := filepath.Join(home, "warden", "b1")
	write(t, filepath.Join(b, "batch.json"), `{"items":[{"id":"01-api","title":"API","route":"strong"},{"id":"02-ui","title":"UI","route":"cheap"}]}`)
	write(t, filepath.Join(b, "01-api.json"), `{"state":"pr_open","phase":"ship","pr":"https://github.com/o/r/pull/9","ci":"green"}`)
	tdir := filepath.Join(home, "plans", "repo", "v1")
	write(t, filepath.Join(tdir, "01-a.md"), "# 01 · A\nStatus: done\n")
	write(t, filepath.Join(tdir, "02-b.md"), "# 02 · B\nStatus: doing\n")
	write(t, filepath.Join(tdir, "03-c.md"), "# 03 · C\nStatus: todo\n")
	oldDirs, oldDec := taskDirs, pendingDecisions
	taskDirs = func() []string { return []string{tdir} }
	pendingDecisions = func() ([]string, error) {
		return []string{"2026-11-22 · timeout · Use Postgres? → yes · fits the stack · ref: https://github.com/o/r/pull/9"}, nil
	}
	t.Cleanup(func() { taskDirs, pendingDecisions = oldDirs, oldDec })
}

func model(w, h int) Model {
	m := newModel(options{profile: colorprofile.TrueColor, dark: true, unicode: true,
		now: func() time.Time { return fixedNow }, load: Load})
	m.width, m.height = w, h
	m.opener = func(string) error { return nil }
	return m
}

func TestLoad(t *testing.T) {
	fixture(t)
	s := Load(fixedNow)
	w := s.Watcher
	if !w.Alive || w.Today != 3 || w.Cap != 20 || len(w.Sources) != 1 {
		t.Fatalf("watcher = %+v", w)
	}
	if len(s.Queue) != 2 || len(s.Batches) != 1 || len(s.Batches[0].Items) != 2 {
		t.Fatalf("queue/batches = %d/%+v", len(s.Queue), s.Batches)
	}
	if it := s.Batches[0].Items[0]; it.Stage != "verify" || it.Model != "strong" || it.CI != "green" {
		t.Fatalf("item = %+v", it)
	}
	if tl := s.Tasks[0]; tl.Total != 3 || tl.Counts["done"] != 1 || tl.Counts["doing"] != 1 {
		t.Fatalf("tasks = %+v", tl)
	}
	if len(s.Decisions) != 1 || len(s.Errors) != 0 {
		t.Fatalf("decisions/errors = %v %v", s.Decisions, s.Errors)
	}
	if Load(fixedNow.Add(time.Hour)).Watcher.Alive {
		t.Fatal("watcher should be stale an hour later")
	}
}

func TestLoadEmpty(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	t.Setenv("WARDEN_HOME", "")
	oldDirs, oldDec := taskDirs, pendingDecisions
	taskDirs = func() []string { return nil }
	pendingDecisions = func() ([]string, error) { return nil, nil }
	t.Cleanup(func() { taskDirs, pendingDecisions = oldDirs, oldDec })
	m := model(80, 24)
	out := ansi.Strip(m.render())
	for _, want := range []string{"never polled", "0 queued", "smithy warden status"} {
		if !strings.Contains(out, want) {
			t.Errorf("empty view lacks %q:\n%s", want, out)
		}
	}
}

func TestGolden(t *testing.T) {
	fixture(t)
	for _, size := range [][2]int{{120, 40}, {80, 24}} {
		m := model(size[0], size[1])
		m.focus = pBatches
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
	m := model(120, 40)
	if m, _ = press(m, "tab"); m.focus != pQueue {
		t.Fatalf("tab -> %v", m.focus)
	}
	if m, _ = press(m, "shift+tab"); m.focus != pWatcher {
		t.Fatalf("shift+tab -> %v", m.focus)
	}
	if m, _ = press(m, "left"); m.focus != pDecisions {
		t.Fatalf("left wraps -> %v", m.focus)
	}
	m, _ = press(m, "right")
	m, _ = press(m, "right")
	if m, _ = press(m, "down"); m.sel[pQueue] != 1 {
		t.Fatalf("down -> %d", m.sel[pQueue])
	}
	if m, _ = press(m, "down"); m.sel[pQueue] != 1 {
		t.Fatal("down past the end moved")
	}
	if m, _ = press(m, "up"); m.sel[pQueue] != 0 {
		t.Fatal("up did not move")
	}
	if m, _ = press(m, "enter"); !m.modal || !strings.Contains(ansi.Strip(m.render()), "Add dark mode") {
		t.Fatal("enter did not open the detail modal")
	}
	if m, _ = press(m, "tab"); m.focus != pQueue {
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
	m.focus = pTasks
	if m, cmd = press(m, "o"); cmd != nil || m.status != "no URL for this row" {
		t.Fatalf("o on a row without URL: %q", m.status)
	}
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

func TestSanitize(t *testing.T) {
	fixture(t)
	home := os.Getenv("SMITHY_HOME")
	write(t, filepath.Join(home, "auto", "state.json"), `{"version":1,"queue":[
{"source":"github","id":"1","title":"evil\u001b]52;c;aGk=\u0007 clip","url":"https://x.io/1\u001b[2J"},
{"source":"github","id":"2","title":"\u001b[2Jwipe\u009b31m\u0001","url":"javascript:alert(1)"}]}`)
	write(t, filepath.Join(home, "warden", "b1", "02-ui.json"), `{"state":"working","note":"n\u001b[31mred\u001b]8;;http://e\u0007"}`)
	pendingDecisions = func() ([]string, error) { return []string{"d \u001b[2J· x"}, errors.New("bad\u001b[2J") }
	s := Load(fixedNow)
	var all []string
	for _, q := range s.Queue {
		all = append(all, q.Title, q.URL, q.ID, q.Source)
	}
	for _, b := range s.Batches {
		for _, it := range b.Items {
			all = append(all, it.Text, it.State, it.Title)
		}
	}
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
	m := model(120, 40)
	m.focus = pQueue
	var opened []string
	m.opener = func(u string) error { opened = append(opened, u); return nil }
	for i := 0; i < 3; i++ {
		m.sel[pQueue] = i
		nm, cmd := press(m, "o")
		if cmd != nil || nm.status != "not a web URL" {
			t.Fatalf("row %d: cmd=%v status=%q", i, cmd != nil, nm.status)
		}
	}
	m.sel[pQueue] = 3
	_, cmd := press(m, "o")
	cmd()
	if len(opened) != 1 || opened[0] != "http://ok.example/4" {
		t.Fatalf("opened %q", opened)
	}
}

func TestOverlappingLoads(t *testing.T) {
	fixture(t)
	m := model(120, 40)
	nm, cmd := m.Update(tickMsg(fixedNow))
	m = nm.(Model)
	if !m.loading || cmd == nil {
		t.Fatal("tick did not start a load")
	}
	nm, _ = m.Update(tickMsg(fixedNow))
	if m2 := nm.(Model); !m2.loading {
		t.Fatal("second tick lost the loading flag")
	}
	calls := 0
	m.load = func(t time.Time) Snapshot { calls++; return Snapshot{At: t} }
	_, cmd = m.Update(tickMsg(fixedNow))
	if _, ok := cmd().(tickMsg); !ok {
		t.Fatal("tick in flight should only schedule the next tick")
	}
	if calls != 0 {
		t.Fatalf("load called %d times while in flight", calls)
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
	m := model(120, 40)
	m.focus = pQueue
	m, _ = press(m, "enter")
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "Queue · details") || !strings.Contains(out, "Decisions") || !strings.Contains(out, "watcher") {
		t.Fatalf("modal does not float over the dashboard:\n%s", out)
	}
	if n := len(strings.Split(out, "\n")); n != 40 {
		t.Fatalf("modal view has %d lines", n)
	}
}

func TestResize(t *testing.T) {
	fixture(t)
	m := model(120, 40)
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	out := ansi.Strip(nm.(Model).render())
	if n := len(strings.Split(out, "\n")); n != 20 {
		t.Fatalf("60x20 rendered %d lines", n)
	}
}

func TestHeaderFrameDeterministic(t *testing.T) {
	fixture(t)
	frameAt := func(steps int) string {
		m := model(120, 40)
		m.motion = true
		m.fx.sweep(m.wordWidth())
		for i := 0; i < steps; i++ {
			m.fx.step()
		}
		return strings.Join(m.wordmark(wordA), "\n")
	}
	if frameAt(9) != frameAt(9) {
		t.Fatal("same step, different frames")
	}
	rest := model(120, 40)
	still := strings.Join(rest.wordmark(wordA), "\n")
	if frameAt(9) == still || frameAt(9) == frameAt(20) {
		t.Fatal("the shimmer band does not move")
	}
	if ansi.Strip(frameAt(9)) != strings.Join(wordA, "\n") {
		t.Fatal("shimmer changed the wordmark's glyphs")
	}
	for _, w := range []int{120, 100, 99, 80} {
		m := model(w, 30)
		head := ansi.Strip(m.header(w, 30))
		want := wordA[0]
		if w < 100 {
			want = wordB[0]
		}
		if !strings.Contains(head, strings.TrimRight(want, " ")) {
			t.Fatalf("width %d: wordmark cut or wrong variant:\n%s", w, head)
		}
		if n := len(strings.Split(head, "\n")); n > 6 {
			t.Fatalf("width %d: header is %d rows", w, n)
		}
	}
}

func TestMotionStopsWhenIdle(t *testing.T) {
	fixture(t)
	m := model(120, 40)
	m.motion = true
	nm, cmd := m.Update(strikeMsg{}) // sweeps the shimmer
	m = nm.(Model)
	if cmd == nil || !m.framing {
		t.Fatal("sweep did not start frames")
	}
	for i := 0; i < 200 && m.framing; i++ {
		nm, _ = m.Update(frameMsg{})
		m = nm.(Model)
	}
	if m.framing || m.fx.busy() {
		t.Fatal("frames keep running after the sweep ended")
	}
	still := model(120, 40)
	if nm, cmd := still.Update(strikeMsg{}); cmd != nil || nm.(Model).framing {
		t.Fatal("reduced motion scheduled frames")
	}
}

func TestNoColor(t *testing.T) {
	fixture(t)
	m := newModel(options{profile: colorprofile.Ascii, dark: true, unicode: false,
		now: func() time.Time { return fixedNow }, load: Load})
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
