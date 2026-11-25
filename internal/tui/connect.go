//go:build harness

package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/remote"
)

// connectPicker is /connect's state inside the palette box: every server with its agents and
// sessions, the Tailscale peers running smithy, and a typed host.
type connectPicker struct {
	loading  bool
	entering bool
	loaded   connectLoadedMsg
}

// connectLoadedMsg is what /connect found: each saved server's status (local first) and the
// Tailscale peers not saved yet.
type connectLoadedMsg struct {
	statuses []remote.Status
	found    []remote.Found
	ferr     error
	err      error
}

// chatSwitch is where the chat goes once this program ends: a server name, local or a URL, and
// a session to open there.
type chatSwitch struct{ server, open string }

// defaultConnectLoad reads the servers file, probes every server and runs discovery, all at once.
// local is this machine's daemon URL and token ("" when none runs).
func defaultConnectLoad(local func() (string, string)) func() tea.Msg {
	return func() tea.Msg {
		ctx := context.Background()
		localURL, localTok := local()
		l, err := remote.Load(remote.File())
		if err != nil {
			return connectLoadedMsg{err: err}
		}
		all := append([]remote.Server{{Name: remote.Local, URL: localURL, Token: localTok}}, l.Servers...)
		type disc struct {
			f   []remote.Found
			err error
		}
		dc := make(chan disc, 1)
		go func() {
			f, err := remote.Discoverer{Status: remote.TailscaleStatus}.Discover(ctx)
			dc <- disc{f, err}
		}()
		var sts []remote.Status
		if localURL == "" {
			sts = append([]remote.Status{{Server: all[0]}}, remote.ProbeAll(ctx, all[1:], 2*time.Second)...)
		} else {
			sts = remote.ProbeAll(ctx, all, 2*time.Second)
		}
		d := <-dc
		var fresh []remote.Found
		for _, f := range d.f {
			known := false
			for _, s := range l.Servers {
				known = known || s.URL == f.URL
			}
			if !known {
				fresh = append(fresh, f)
			}
		}
		return connectLoadedMsg{statuses: sts, found: fresh, ferr: d.err}
	}
}

// showConnect is /connect: opens the server picker and loads it.
func (m *chatView) showConnect() tea.Cmd {
	m.overlay, m.pal = overlayPalette, palette{}
	m.picker = &connectPicker{loading: true}
	if m.connectLoad == nil {
		return nil
	}
	return m.connectLoad
}

func (m *chatView) connectLoaded(msg connectLoadedMsg) {
	if m.picker == nil {
		return
	}
	m.picker.loading, m.picker.loaded = false, msg
	m.pal.sel = 0
}

func serverKey(s remote.Server) string {
	if s.Name != "" {
		return s.Name
	}
	return s.URL
}

// switchServer saves s as the default and ends this program so Chat reconnects there.
func (m *chatView) switchServer(key, open string) tea.Cmd {
	if key == m.server && open == "" {
		return nil
	}
	if key == m.server {
		m.open(open)
		return nil
	}
	if m.connectUse != nil && !strings.Contains(key, "://") {
		if err := m.connectUse(key); err != nil {
			m.system("could not save the default server: " + err.Error())
		}
	}
	m.switchTo = &chatSwitch{server: key, open: open}
	return tea.Quit
}

// connectItems are the picker's rows, filtered by the typed query.
func (m chatView) connectItems() []palItem {
	t := m.th
	p := m.picker
	if p.entering {
		host := strings.TrimSpace(m.pal.query)
		label, hint := "Connect to "+host, "http(s)://host:port; a tailscale serve URL needs no code"
		if host == "" {
			label = "Connect to …"
		}
		return []palItem{{group: "ENTER HOST", glyph: "→", color: t.accent, label: label, hint: hint, run: func(m *chatView) tea.Cmd {
			u, err := remote.CleanURL(host)
			if err != nil {
				m.system(err.Error())
				return nil
			}
			return m.switchServer(u, "")
		}}}
	}
	var all []palItem
	if p.loading {
		all = append(all, palItem{group: "SERVERS", glyph: t.g.dot, color: t.dim, label: "checking servers…", run: func(*chatView) tea.Cmd { return nil }})
	}
	if p.loaded.err != nil {
		all = append(all, palItem{group: "SERVERS", glyph: t.g.fail, color: t.err, label: p.loaded.err.Error(), run: func(*chatView) tea.Cmd { return nil }})
	}
	for _, st := range p.loaded.statuses {
		key := serverKey(st.Server)
		g, c := t.g.on, t.ok
		if !st.Online {
			g, c = t.g.fail, t.dim
		}
		label := key
		if key == m.server {
			label += "  (this chat)"
		}
		hint := remote.StatusLine(st)
		if key == remote.Local {
			hint = "this machine " + t.g.dot + " " + hint
		} else {
			hint = st.Server.URL + " " + t.g.dot + " " + hint
		}
		all = append(all, palItem{group: "SERVERS", glyph: g, color: c, label: label, hint: hint, run: func(m *chatView) tea.Cmd { return m.switchServer(key, "") }})
		for _, s := range st.Sessions {
			sg, sc := m.sessionGlyph(s.Status)
			title, id := short(s.Title, 44), s.ID
			if title == "" {
				title = s.ID
			}
			all = append(all, palItem{group: "SERVERS", glyph: "  " + sg, color: sc, label: "  " + title, hint: strings.Trim(s.Agent+" "+t.g.dot+" "+s.Status, " "+t.g.dot),
				run: func(m *chatView) tea.Cmd { return m.switchServer(key, id) }})
		}
	}
	for _, f := range p.loaded.found {
		f := f
		all = append(all, palItem{group: "TAILSCALE", glyph: "+", color: t.muted, label: f.Host, hint: f.URL + " " + t.g.dot + " smithy " + f.Version,
			run: func(m *chatView) tea.Cmd {
				name := strings.ToLower(f.Host)
				if m.connectAdd != nil {
					if err := m.connectAdd(remote.Server{Name: name, URL: f.URL}); err != nil {
						m.system("could not save " + name + ": " + err.Error())
						return nil
					}
				}
				return m.switchServer(name, "")
			}})
	}
	if p.loaded.ferr != nil && !p.loading {
		all = append(all, palItem{group: "TAILSCALE", glyph: t.g.dot, color: t.dim, label: "no peers", hint: short(p.loaded.ferr.Error(), 60), run: func(*chatView) tea.Cmd { return nil }})
	}
	all = append(all, palItem{group: "OTHER", glyph: "→", color: t.muted, label: "Enter host…", hint: "a daemon by URL; pair it first with smithy pair there",
		run: func(m *chatView) tea.Cmd {
			m.overlay, m.pal = overlayPalette, palette{}
			m.picker.entering = true
			return nil
		}})
	q := strings.ToLower(m.pal.query)
	var out []palItem
	for _, it := range all {
		if q == "" || strings.Contains(strings.ToLower(it.label+" "+it.hint), q) {
			out = append(out, it)
		}
	}
	return out
}

// connectTitle is the picker box's placeholder.
func (m chatView) connectTitle() string {
	if m.picker.entering {
		return "Type a daemon URL"
	}
	return fmt.Sprintf("Pick a server (now: %s)", m.server)
}
