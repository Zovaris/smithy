//go:build harness

package tui

import (
	"slices"
	"testing"
)

func TestChatViewCarriesProgramResume(t *testing.T) {
	m := newChatTest(0)
	m.resume = []string{"/bin/smithy", "chat", "--server", "home"}
	m.resumeDir = "/work"
	m.open("s-42")
	v := m.View()
	if v.ProgramResume == nil {
		t.Fatal("an open chat must tell the terminal how to resume it")
	}
	want := []string{"/bin/smithy", "chat", "--server", "home", "--session", "s-42"}
	if !slices.Equal(v.ProgramResume.Args, want) || v.ProgramResume.Dir != "/work" {
		t.Fatalf("resume = %v in %q, want %v in /work", v.ProgramResume.Args, v.ProgramResume.Dir, want)
	}
	m.open("")
	if got := m.View().ProgramResume.Args; !slices.Equal(got, want[:4]) {
		t.Fatalf("no session open: resume = %v, want %v", got, want[:4])
	}
	n, _ := m.Update(ckey("ctrl+c"))
	if n.(chatView).View().ProgramResume != nil {
		t.Fatal("a quitting chat must not ask to be resumed")
	}
}

func TestParseChatArgsSession(t *testing.T) {
	a, err := parseChatArgs([]string{"--session", "s-1"})
	if err != nil || a.open != "s-1" {
		t.Fatalf("--session: %+v %v", a, err)
	}
	if _, err := parseChatArgs([]string{"--session"}); err == nil {
		t.Fatal("--session without an id must fail")
	}
}
