//go:build harness

package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/api"
	"github.com/Zovaris/smithy/internal/store"
	"github.com/charmbracelet/x/ansi"
)

func TestVersionNote(t *testing.T) {
	if n := versionNote("v0.23.0", "v0.23.0"); n != "" {
		t.Fatalf("same version warned: %q", n)
	}
	n := versionNote("v0.22.0", "v0.23.0")
	for _, want := range []string{"v0.22.0", "v0.23.0", "smithy stop"} {
		if !strings.Contains(n, want) {
			t.Fatalf("mismatch note %q lacks %q", n, want)
		}
	}
	if n := versionNote("", "v0.23.0"); !strings.Contains(n, "older") {
		t.Fatalf("a daemon with no version should read as older: %q", n)
	}
}

func TestParseChatArgs(t *testing.T) {
	a, err := parseChatArgs([]string{"--standalone"})
	if err != nil || !a.standalone || a.url != "" {
		t.Fatalf("--standalone: %+v %v", a, err)
	}
	a, err = parseChatArgs([]string{"http://box.ts.net"})
	if err != nil || a.standalone || a.url != "http://box.ts.net" {
		t.Fatalf("url: %+v %v", a, err)
	}
	if _, err := parseChatArgs([]string{"--standalone", "http://x"}); err == nil {
		t.Fatal("--standalone with a url accepted")
	}
	if _, err := parseChatArgs([]string{"--nope"}); err == nil {
		t.Fatal("unknown flag accepted")
	}
}

func TestStartedNoteExplainsBackground(t *testing.T) {
	n := startedNote(&api.Info{PID: 42, Addr: "127.0.0.1:7477"}, 30*time.Minute)
	for _, want := range []string{"pid 42", "background", "smithy stop", "30m"} {
		if !strings.Contains(n, want) {
			t.Fatalf("started note %q lacks %q", n, want)
		}
	}
}

func TestShortDur(t *testing.T) {
	for d, want := range map[time.Duration]string{30 * time.Minute: "30m", time.Hour: "1h", 90 * time.Minute: "1h30m", 90 * time.Second: "1m30s"} {
		if got := shortDur(d); got != want {
			t.Errorf("shortDur(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestNarrowStatusLineKeepsStopHint(t *testing.T) {
	m := newChatTest(0)
	m.where = whereBackground
	m.status("serve: poll: github: To get started with GitHub CLI, please run: gh auth login")
	f := ansi.Strip(m.statusLine(100))
	if !strings.Contains(f, "smithy stop") {
		t.Fatalf("at 100 columns the status line lost the stop hint: %q", f)
	}
}

func TestNotesShowAfterHistoryReplay(t *testing.T) {
	m := newChatTest(0)
	m.hold([]string{"the daemon runs v0.22.0"}, 2)
	for seq := int64(1); seq <= 2; seq++ {
		m = chatStep(m, api.ClientMsg{Event: &store.Event{Seq: seq, Kind: "log", Data: []byte(`{"line":"old"}`)}})
	}
	last := m.msgs[len(m.msgs)-1]
	if !strings.Contains(last.text, "v0.22.0") {
		t.Fatalf("note not after the replayed history; last message %q", last.text)
	}
	m2 := newChatTest(0)
	m2.hold([]string{"fresh"}, 0)
	if len(m2.msgs) != 1 || m2.msgs[0].text != "fresh" {
		t.Fatal("with no history the note does not show at once")
	}
}
