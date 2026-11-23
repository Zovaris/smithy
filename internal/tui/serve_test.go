package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/auto"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

func keyPress(k string) tea.KeyPressMsg {
	switch k {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: rune(k[0]), Text: k}
}

func TestServeViewStopConfirm(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	t.Setenv("SMITHY_CONFIG", t.TempDir()+"/config.json")
	done := make(chan struct{})
	n := time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)
	m := newServeView(done, nil, make(chan string), newTheme(true, colorprofile.Ascii, false), func() time.Time { return n },
		func(time.Time) Snapshot { return Snapshot{} })
	var tm tea.Model = m
	tm, _ = tm.Update(serveLog("orchestrator started: pid 5150"))
	out := ansi.Strip(strings.Join(tm.(serveView).lines(), "\n"))
	for _, want := range []string{"smithy serve", "not running", "sources", "today", "session", "workers", "orchestrator started", "esc/q stop"} {
		if !strings.Contains(out, want) {
			t.Errorf("view lacks %q:\n%s", want, out)
		}
	}
	for _, k := range []string{"esc", "q", "ctrl+c"} {
		var cmd tea.Cmd
		tm, _ = tm.Update(keyPress(k))
		if !strings.Contains(strings.Join(tm.(serveView).lines(), "\n"), "stop serve? running agents keep going (y/n)") {
			t.Errorf("%s did not ask", k)
		}
		tm, cmd = tm.Update(keyPress("n"))
		if tm.(serveView).confirm || cmd != nil {
			t.Errorf("n after %s did not cancel", k)
		}
	}
	tm, _ = tm.Update(keyPress("q"))
	if _, cmd := tm.Update(keyPress("y")); cmd == nil {
		t.Error("y did not quit")
	}
	if _, cmd := tm.Update(keyPress("ctrl+c")); cmd == nil {
		t.Error("ctrl+c at the prompt did not confirm")
	}
	once := m
	once.sv = &auto.Server{Opt: auto.ServeOptions{Once: true}}
	var om tea.Model = once
	om, cmd := om.Update(serveDone{})
	if !om.(serveView).ended || cmd == nil || !strings.Contains(strings.Join(om.(serveView).lines(), "\n"), "any key closes") {
		t.Error("--once did not hold the view")
	}
	if _, cmd := om.Update(keyPress("x")); cmd == nil {
		t.Error("a key did not close the held view")
	}
	if _, cmd := tm.Update(serveDone{}); cmd == nil {
		t.Error("serve end did not quit")
	}
}
