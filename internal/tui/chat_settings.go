//go:build harness

package tui

import (
	"context"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/daemon"
)

// settingsAPI is the daemon's settings endpoints; *daemon.Client is one.
type settingsAPI interface {
	Settings(ctx context.Context) (daemon.Settings, error)
	SetSetting(ctx context.Context, key, value string) error
	UnsetSetting(ctx context.Context, key string) error
	RemoveKey(ctx context.Context, service string) error
	SetServe(ctx context.Context, on bool) error
	Doctor(ctx context.Context) ([]string, error)
	Tend(ctx context.Context) (daemon.TendList, error)
	ConnectTest(ctx context.Context) (daemon.ConnectTestResult, error)
	SessionLinks(ctx context.Context, id string) (daemon.SessionLinks, error)
	AddLink(ctx context.Context, id, ref string) (daemon.SessionLinks, error)
}

// settingsDoneMsg is a settings command's result: lines for the transcript, or a panel to open.
type settingsDoneMsg struct {
	lines []string
	panel *infoPanel
	tend  *daemon.TendList // a fresh tend state for the header
}

// tendStateMsg is the tend mode and PR count the header shows.
type tendStateMsg struct{ l daemon.TendList }

// loadTend reads the tend state for the header; nil without a daemon.
func (m chatView) loadTend() tea.Cmd {
	if m.cfg == nil {
		return nil
	}
	api := m.cfg
	return func() tea.Msg {
		l, err := api.Tend(context.Background())
		if err != nil {
			return nil
		}
		return tendStateMsg{l: l}
	}
}

// tendTag is the header's tend state: "tend fix · 2 PRs"; "" before the first read.
func (m chatView) tendTag() string {
	if m.tendMode == "" {
		return ""
	}
	s := "tend " + m.tendMode
	switch {
	case m.tendPRs == 1:
		s += " · 1 PR"
	case m.tendPRs > 1:
		s += " · " + strconv.Itoa(m.tendPRs) + " PRs"
	}
	return s
}

// settingsUsage is one settings command's usage line.
func settingsUsage(name string) string {
	for _, c := range slashCommands {
		if c.name == name {
			return strings.TrimSpace("/" + name + " " + c.args)
		}
	}
	return "/" + name
}

// panelSetting is a modal row holding one setting: left/right cycle its value, enter saves it.
type panelSetting struct {
	key  string // a config key, or "serve"
	opts []string
	sel  int
}

// saveSetting writes the panel's setting through the daemon.
func (m chatView) saveSetting(p panelSetting) tea.Cmd {
	api, v := m.cfg, p.opts[p.sel]
	return func() tea.Msg {
		var err error
		if p.key == "serve" {
			err = api.SetServe(context.Background(), v == "on")
		} else {
			err = api.SetSetting(context.Background(), p.key, v)
		}
		if err != nil {
			return sessionErrMsg{what: "saving " + p.key, err: err}
		}
		out := settingsDoneMsg{lines: []string{"saved " + p.key + " = " + v}}
		if p.key == "tend.daemon" {
			if l, err := api.Tend(context.Background()); err == nil {
				out.tend = &l
			}
		}
		return out
	}
}

// settingRow is how infoBox draws a setting: its value between arrows.
func settingRow(p panelSetting) string {
	return p.key + "  ‹ " + p.opts[p.sel] + " ›\t←→ change · enter saves"
}

// servePanel is /serve: serve on or off, cycled with left/right.
func servePanel(on bool) *infoPanel {
	sel := 1
	if on {
		sel = 0
	}
	return &infoPanel{title: "Serve", set: &panelSetting{key: "serve", opts: []string{"on", "off"}, sel: sel},
		lines: []string{"#SERVE", "off launches no new work (auto.paused); running work goes on"}}
}

// doctorPanel is /doctor: the checks grouped by what they need, each problem with its fix.
func doctorPanel(lines []string) *infoPanel {
	type check struct{ label, fix string }
	var fail, warn, ok []check
	var last *[]check
	for _, l := range lines {
		l = strings.TrimSpace(l)
		word, rest, _ := strings.Cut(l, " ")
		rest = strings.TrimSpace(rest)
		switch {
		case l == "" || l == "warden doctor":
		case word == "fix:" && last != nil && len(*last) > 0:
			(*last)[len(*last)-1].fix = rest
		case word == "ok":
			ok, last = append(ok, check{label: rest}), &ok
		case word == "warn":
			warn, last = append(warn, check{label: rest}), &warn
		case word == "FAIL" || word == "bad" || word == "fail" || word == "doctor:":
			fail, last = append(fail, check{label: rest}), &fail
		default:
			ok, last = append(ok, check{label: l}), &ok
		}
	}
	out := []string{}
	add := func(head, mark string, cs []check) {
		if len(cs) == 0 {
			return
		}
		out = append(out, "#"+head)
		for _, c := range cs {
			out = append(out, mark+c.label)
			if c.fix != "" {
				out = append(out, "-> fix: "+c.fix)
			}
		}
	}
	add("NEEDS A FIX", "!! ", fail)
	add("WORTH A LOOK", "?? ", warn)
	add("OK", "ok ", ok)
	if len(out) == 0 {
		out = []string{"doctor printed nothing"}
	}
	return &infoPanel{title: "Doctor", lines: out, scroll: true}
}

// detach quits and leaves the daemon running; a standalone daemon lives in this process, so it stays.
func (m chatView) detach() (tea.Model, tea.Cmd) {
	if m.where == whereStandalone {
		m.system("standalone: the daemon runs in this process and ends with it; /quit to leave, or start it with smithy daemon to detach")
		return m, nil
	}
	return m, tea.Quit
}

// settingsCommand runs /serve, /set, /unset, /key, /keys, /doctor, /tend and /channels through the daemon.
func (m chatView) settingsCommand(name, arg string) (tea.Model, tea.Cmd) {
	if m.cfg == nil {
		m.system("settings need a daemon this chat can reach")
		return m, nil
	}
	api := m.cfg
	f := strings.Fields(arg)
	var run func(context.Context) ([]string, error)
	switch {
	case name == "serve" && len(f) == 1 && (f[0] == "on" || f[0] == "off"):
		on := f[0] == "on"
		run = func(ctx context.Context) ([]string, error) {
			return []string{"serve " + f[0] + map[bool]string{true: ": serve launches new work again", false: ": serve launches nothing new (auto.paused)"}[on]}, api.SetServe(ctx, on)
		}
	case name == "set" && len(f) >= 2:
		k, v := f[0], strings.TrimSpace(strings.TrimPrefix(arg, f[0]))
		run = func(ctx context.Context) ([]string, error) {
			return []string{"saved " + k + " = " + v}, api.SetSetting(ctx, k, v)
		}
	case name == "unset" && len(f) == 1:
		run = func(ctx context.Context) ([]string, error) {
			return []string{"removed " + f[0] + ": back to its default"}, api.UnsetSetting(ctx, f[0])
		}
	case name == "key" && len(f) == 2 && f[0] == "rm":
		run = func(ctx context.Context) ([]string, error) {
			return []string{"removed the " + f[1] + " key"}, api.RemoveKey(ctx, f[1])
		}
	case name == "keys":
		return m, func() tea.Msg {
			s, err := api.Settings(context.Background())
			if err != nil {
				return sessionErrMsg{what: "/keys", err: err}
			}
			return settingsDoneMsg{panel: keysPanel(s)}
		}
	case name == "serve" && arg == "":
		return m, func() tea.Msg {
			st, err := api.Settings(context.Background())
			if err != nil {
				return sessionErrMsg{what: "/serve", err: err}
			}
			return settingsDoneMsg{panel: servePanel(st.Serve)}
		}
	case name == "prompt":
		run = func(ctx context.Context) ([]string, error) {
			s, err := api.Settings(ctx)
			state := "override active: it replaces smithy's default system prompt"
			if !s.Prompt.Active {
				state = "no override: the default system prompt is in use; write this file to replace it"
			}
			return []string{"prompt file " + s.Prompt.Path, state}, err
		}
	case name == "tend":
		return m, func() tea.Msg {
			l, err := api.Tend(context.Background())
			if err != nil {
				return sessionErrMsg{what: "/tend", err: err}
			}
			return settingsDoneMsg{panel: tendPanel(l), tend: &l}
		}
	case name == "channels" && arg == "test":
		m.system("sending a test message to every chat channel…")
		run = func(ctx context.Context) ([]string, error) {
			r, err := api.ConnectTest(ctx)
			if r.Error != "" {
				r.Results = append(r.Results, r.Error)
			}
			return r.Results, err
		}
	case name == "channels":
		return m, func() tea.Msg {
			s, err := api.Settings(context.Background())
			if err != nil {
				return sessionErrMsg{what: "/channels", err: err}
			}
			return settingsDoneMsg{panel: channelsPanel(s)}
		}
	case name == "doctor":
		m.info, m.overlay, m.pal = infoPanel{title: "Doctor", lines: []string{"running smithy doctor…"}}, overlayInfo, palette{}
		return m, func() tea.Msg {
			lines, err := api.Doctor(context.Background())
			if err != nil {
				return sessionErrMsg{what: "/doctor", err: err}
			}
			return settingsDoneMsg{panel: doctorPanel(lines)}
		}
	default:
		m.system("usage: " + settingsUsage(name))
		return m, nil
	}
	return m, func() tea.Msg {
		lines, err := run(context.Background())
		if err != nil {
			return sessionErrMsg{what: "/" + name, err: err}
		}
		return settingsDoneMsg{lines: lines}
	}
}

// keysPanel is /keys: the stored keys masked, each with the command that removes it, and how
// to add one (a secret is typed in a terminal, never into the chat).
func keysPanel(s daemon.Settings) *infoPanel {
	lines := []string{"#STORED API KEYS"}
	for _, k := range s.Keys {
		lines = append(lines, "ok "+k.Service+" "+k.Masked+"\t/key rm "+k.Service+" removes it")
	}
	if len(s.Keys) == 0 {
		lines = append(lines, "none yet")
	}
	lines = append(lines, "#ADD OR REPLACE ONE", "-> smithy auth set <service>\tin a terminal; the key is read without echo",
		"#SERVE", map[bool]string{true: "ok serve on\t/serve off pauses new work", false: "serve off (auto.paused)\t/serve on resumes it"}[s.Serve])
	return &infoPanel{title: "API keys", lines: lines}
}

// channelsPanel is /channels: each chat connector's notifications and chat channel, how to set
// them, and the test-send.
func channelsPanel(s daemon.Settings) *infoPanel {
	val := map[string]string{}
	for _, r := range s.Settings {
		val[r.Key] = r.Value
	}
	lines := []string{}
	for _, svc := range []string{"telegram", "slack", "discord"} {
		lines = append(lines, "#"+strings.ToUpper(svc))
		for _, p := range []string{"notify", "chat"} {
			k := "connect." + svc + "." + p
			v := val[k]
			if v == "" {
				v = "unset"
			}
			lines = append(lines, k+" "+v+"\t/set "+k+" <id>")
		}
	}
	lines = append(lines, "#TEST", "-> /channels test\tsends a test message to every channel set, from the current settings")
	return &infoPanel{title: "Chat channels", lines: lines}
}

// tendPanel is /tend: the warden batch and chat session PRs the daemon tends, each with its status, tend's next
// action and the CI state; a PR being fixed names its tend session.
func tendPanel(l daemon.TendList) *infoPanel {
	modes := []string{"off", "watch", "fix"}
	sel := max(slices.Index(modes, l.Mode), 0)
	lines := []string{"#PULL REQUESTS"}
	for _, p := range l.PRs {
		mark := map[string]string{"green": "ok ", "closed": "ok ", "error": "!! ", "needs-you": "!! "}[p.Status]
		hint := p.Action + " · checks " + p.Checks
		switch {
		case p.Error != "":
			hint = p.Error
		case p.PID != 0:
			hint += " · tend session pid " + strconv.Itoa(p.PID)
		}
		from := p.Batch + "/" + p.Item
		if p.Batch == "" {
			from = "session " + p.Session
		}
		lines = append(lines, mark+from+" "+p.Status+" "+p.URL+"\t"+hint)
	}
	if len(l.PRs) == 0 {
		lines = append(lines, "none: no warden batch or chat session has an open PR to tend")
	}
	return &infoPanel{title: "Tend", lines: lines, scroll: true, set: &panelSetting{key: "tend.daemon", opts: modes, sel: sel}}
}
