//go:build harness

package tui

import (
	"fmt"
	"net/url"
	"strings"

	tea "charm.land/bubbletea/v2"
)

type webOpenedMsg struct{ err error }

// openWeb opens the web UI, signed in with the daemon token, in the default browser.
func (m chatView) openWeb() (chatView, tea.Cmd) {
	if m.webURL == "" {
		m.system("no web UI address for this daemon")
		return m, nil
	}
	u := m.webURL
	m.status("opening the web UI in your browser")
	return m, func() tea.Msg {
		if err := openURL(u); err != nil {
			return sessionErrMsg{what: "open the web UI", err: err}
		}
		return nil
	}
}

func hostOf(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return u
	}
	return p.Host
}

// showRemote is /remote: how to reach this daemon from a phone or another machine.
func (m *chatView) showRemote() {
	lines := []string{"#FROM ANOTHER MACHINE"}
	if strings.Contains(hostOf(m.url), ".ts.net") {
		lines = append(lines, "ok "+m.url+"\tserved on your tailnet; daemon.allow decides who gets in")
	} else {
		lines = append(lines,
			"-> tailscale serve --bg "+portOf(m.url)+"\tserves this daemon on your tailnet, nothing exposed to the internet",
			"-> smithy config set daemon.allow <login>\tthe tailnet logins let in, comma separated",
			"then open https://<this machine>.<tailnet>.ts.net\ton your phone, or smithy attach it from a laptop")
	}
	lines = append(lines, "#ON THIS MACHINE", "-> /web\tthe same sessions in the browser")
	m.info = infoPanel{title: "Remote", lines: lines}
	m.overlay, m.pal = overlayInfo, palette{}
}

func portOf(u string) string {
	p, err := url.Parse(u)
	if err != nil || p.Port() == "" {
		return "7477"
	}
	return p.Port()
}

// showLog is /log: every daemon event kept out of the transcript, newest last.
func (m *chatView) showLog() {
	lines := []string{fmt.Sprintf("#DAEMON LOG · %d EVENTS", len(m.noise))}
	for _, n := range m.noise {
		lines = append(lines, n.at.Format("15:04:05")+"  "+n.text)
	}
	if len(m.noise) == 0 {
		lines = append(lines, "nothing yet")
	}
	m.info = infoPanel{title: "Log", lines: lines, scroll: true}
	m.overlay, m.pal = overlayInfo, palette{sel: max(len(m.noise)-1, 0)}
}
