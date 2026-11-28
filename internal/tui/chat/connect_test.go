//go:build harness

package chat

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/remote"
)

func TestParseChatArgsServer(t *testing.T) {
	for _, in := range [][]string{{"--server", "home"}, {"--server=home"}} {
		a, err := parseChatArgs(in)
		if err != nil || a.server != "home" {
			t.Fatalf("%v: %+v %v", in, a, err)
		}
	}
	if _, err := parseChatArgs([]string{"--server"}); err == nil {
		t.Fatal("--server needs a value")
	}
	if _, err := parseChatArgs([]string{"--server", "home", "--standalone"}); err == nil {
		t.Fatal("--standalone runs its own daemon")
	}
}

func connectScene(t *testing.T) chatView {
	t.Helper()
	m := newChatScene()
	m.server = remote.Local
	m.connectLoad = func() tea.Msg {
		return connectLoadedMsg{
			statuses: []remote.Status{
				{Server: remote.Server{Name: remote.Local}, Online: true, Version: "0.23.0", Agents: []string{"claude"},
					Sessions: []remote.SessionRow{{ID: "s1", Title: "fix login", Status: "running", Agent: "claude"}}},
				{Server: remote.Server{Name: "home", URL: "http://100.64.0.2:7478"}, Online: true, Version: "0.23.0", Agents: []string{"opencode"},
					Sessions: []remote.SessionRow{{ID: "h1", Title: "nightly docs", Status: "done", Agent: "opencode"}}},
				{Server: remote.Server{Name: "cloud", URL: "https://c.example.com"}},
			},
			found: []remote.Found{{Peer: remote.Peer{Host: "mac", DNS: "mac.tail1.ts.net"}, URL: "https://mac.tail1.ts.net", Version: "0.23.0"}},
		}
	}
	return m
}

func runCmd(m chatView, c tea.Cmd) chatView {
	for c != nil {
		msg := c()
		if msg == nil {
			break
		}
		if _, ok := msg.(tea.QuitMsg); ok {
			break
		}
		var mm tea.Model
		mm, c = m.Update(msg)
		m = mm.(chatView)
	}
	return m
}

func TestConnectPickerGroupsServers(t *testing.T) {
	m := connectScene(t)
	m = runCmd(m, m.showConnect())
	out := screen(m)
	for _, want := range []string{"Servers", "local", "this machine", "claude", "home", "opencode", "fix login", "nightly docs", "cloud", "offline", "Tailscale", "mac", "Enter host"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in\n%s", want, out)
		}
	}
	labels := ""
	for _, it := range m.palItems() {
		labels += it.group + ":" + it.label + "\n"
	}
	if strings.Index(labels, "fix login") > strings.Index(labels, "home") {
		t.Fatalf("each server's sessions sit under it:\n%s", labels)
	}
}

func pick(t *testing.T, m chatView, label string) (chatView, tea.Cmd) {
	t.Helper()
	for i, it := range m.palItems() {
		if strings.Contains(it.label, label) {
			m.pal.sel = i
			mm, c := m.overlayKey(tea.KeyPressMsg{Code: tea.KeyEnter})
			return mm.(chatView), c
		}
	}
	t.Fatalf("no item %q", label)
	return m, nil
}

func TestConnectPickSwitchesAndSaves(t *testing.T) {
	m := connectScene(t)
	var saved []string
	m.connectUse = func(name string) error { saved = append(saved, name); return nil }
	m = runCmd(m, m.showConnect())
	m, c := pick(t, m, "nightly docs")
	if c == nil || m.switchTo == nil || m.switchTo.server != "home" || m.switchTo.open != "h1" {
		t.Fatalf("picking a session on another server switches there and opens it: %+v", m.switchTo)
	}
	if len(saved) != 1 || saved[0] != "home" {
		t.Fatalf("the choice is saved as default: %v", saved)
	}

	m = connectScene(t)
	m.connectUse = func(string) error { return nil }
	m = runCmd(m, m.showConnect())
	m, c = pick(t, m, "local")
	if c != nil || m.switchTo != nil || m.overlay != overlayNone {
		t.Fatal("picking the current server just closes the picker")
	}
}

func TestConnectPickTailscalePeer(t *testing.T) {
	m := connectScene(t)
	var added []remote.Server
	m.connectAdd = func(s remote.Server) error { added = append(added, s); return nil }
	m.connectUse = func(string) error { return nil }
	m = runCmd(m, m.showConnect())
	m, _ = pick(t, m, "mac")
	if len(added) != 1 || added[0].Name != "mac" || added[0].URL != "https://mac.tail1.ts.net" || m.switchTo == nil || m.switchTo.server != "mac" {
		t.Fatalf("a tailscale peer is saved and connected: %+v %+v", added, m.switchTo)
	}
}

func TestConnectEnterHost(t *testing.T) {
	m := connectScene(t)
	m.connectUse = func(string) error { return nil }
	m = runCmd(m, m.showConnect())
	m, _ = pick(t, m, "Enter host")
	if m.overlay != overlayPalette || m.picker == nil || !m.picker.entering {
		t.Fatal("enter host keeps the box open for typing")
	}
	for _, r := range "http://10.0.0.9:7478" {
		mm, _ := m.overlayKey(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = mm.(chatView)
	}
	if !strings.Contains(screen(m), "10.0.0.9") {
		t.Fatal("the typed host shows")
	}
	m, _ = pick(t, m, "Connect to")
	if m.switchTo == nil || m.switchTo.server != "http://10.0.0.9:7478" {
		t.Fatalf("%+v", m.switchTo)
	}
}

func TestConnectEscClosesPicker(t *testing.T) {
	m := connectScene(t)
	m = runCmd(m, m.showConnect())
	mm, _ := m.overlayKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = mm.(chatView)
	if m.overlay != overlayNone || m.picker != nil {
		t.Fatal("esc closes it")
	}
	m.overlay = overlayPalette
	if strings.Contains(screen(m), "Tailscale") {
		t.Fatal("the palette is back to actions")
	}
}
