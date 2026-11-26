//go:build harness

package tui

import (
	"context"
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
}

// settingsDoneMsg is a settings command's result: lines for the transcript, or a panel to open.
type settingsDoneMsg struct {
	lines []string
	panel *infoPanel
}

// detach quits and leaves the daemon running; a standalone daemon lives in this process, so it stays.
func (m chatView) detach() (tea.Model, tea.Cmd) {
	if m.where == whereStandalone {
		m.system("standalone: the daemon runs in this process and ends with it; /quit to leave, or start it with smithy daemon to detach")
		return m, nil
	}
	return m, tea.Quit
}

// settingsCommand runs /serve, /set, /unset, /key, /keys and /doctor through the daemon.
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
		run = func(ctx context.Context) ([]string, error) {
			s, err := api.Settings(ctx)
			return []string{map[bool]string{true: "serve on", false: "serve off (auto.paused)"}[s.Serve]}, err
		}
	case name == "doctor":
		m.system("running smithy doctor…")
		run = api.Doctor
	default:
		m.system("usage: /serve on|off · /set <key> <value> · /unset <key> · /keys · /key rm <service> · /doctor")
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
